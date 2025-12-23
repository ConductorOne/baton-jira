package connector

import (
	"context"
	"fmt"
	"net/http"

	"github.com/conductorone/baton-jira/pkg/client"
	"github.com/conductorone/baton-jira/pkg/client/atlassianclient"
	cfg "github.com/conductorone/baton-jira/pkg/config"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/cli"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	jira "github.com/conductorone/go-jira/v2/cloud"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

var wrapError = client.WrapError

type (
	Jira struct {
		client                  *client.Client
		atlassianClient         *atlassianclient.AtlassianClient
		projectKeys             []string
		skipProjectParticipants bool
		skipCustomerUser        bool
		siteIDs                 []string

		username    string
		apiToken    string
		originalURL string
	}

	JiraBuilder interface {
		New() (*Jira, error)
	}

	JiraOptions struct {
		Url         string
		ProjectKeys []string

		AtlassianBuilder *AtlassianAuthBuilder
	}

	JiraBasicAuthBuilder struct {
		Base *JiraOptions

		Username string
		ApiToken string
	}

	AtlassianAuthBuilder struct {
		OrganizationId string
		AccessToken    string
	}
)

func New(ctx context.Context, jc *cfg.Jira, opts *cli.ConnectorOpts) (connectorbuilder.ConnectorBuilderV2, []connectorbuilder.Opt, error) {
	l := ctxzap.Extract(ctx)

	builder := JiraBasicAuthBuilder{
		Base: &JiraOptions{
			Url:         jc.JiraUrl,
			ProjectKeys: jc.JiraProjectKeys,
		},
		Username: jc.JiraEmail,
		ApiToken: jc.JiraApiToken,
	}

	if jc.AtlassianOrgid != "" && jc.AtlassianApiToken != "" {
		builder.Base.AtlassianBuilder = &AtlassianAuthBuilder{
			OrganizationId: jc.AtlassianOrgid,
			AccessToken:    jc.AtlassianApiToken,
		}
	}

	jiraConnector, err := builder.New(ctx, jc.SkipProjectParticipants, jc.SkipCustomerUser)
	if err != nil {
		l.Error("error creating connector", zap.Error(err))
		return nil, nil, err
	}

	builderOpts := make([]connectorbuilder.Opt, 0)
	if jc.Ticketing {
		builderOpts = append(builderOpts, connectorbuilder.WithTicketingEnabled())
	}

	return jiraConnector, builderOpts, nil
}

func (b *JiraBasicAuthBuilder) New(ctx context.Context, skipProjectParticipants bool, skipCustomerUser bool) (*Jira, error) {
	var err error
	var c *client.Client

	if client.IsServiceAccount(b.Username) {
		c, err = client.NewWithScopedToken(ctx, b.Username, b.ApiToken, b.Base.Url)
	} else {
		c, err = client.New(b.Username, b.ApiToken, b.Base.Url)
	}

	if err != nil {
		return nil, client.WrapError(err, "error creating jira client", nil)
	}

	jc := &Jira{
		client:                  c,
		projectKeys:             b.Base.ProjectKeys,
		skipProjectParticipants: skipProjectParticipants,
		skipCustomerUser:        skipCustomerUser,

		username:    b.Username,
		apiToken:    b.ApiToken,
		originalURL: b.Base.Url,
	}

	if b.Base.AtlassianBuilder == nil {
		return jc, nil
	}

	// Note: AtlassianClient uses the original URL (not the resolved service account URL)
	// because it authenticates via Bearer token and uses the Atlassian Admin API endpoints,
	// not the Jira API endpoints. The original URL is needed to match workspace hostUrl.
	ac, siteIDs, err := atlassianclient.New(ctx,
		b.Base.Url,
		atlassianclient.WithAccessToken(b.Base.AtlassianBuilder.AccessToken),
		atlassianclient.WithOrganizationID(b.Base.AtlassianBuilder.OrganizationId),
	)
	if err != nil {
		return nil, client.WrapError(err, "error creating atlassian client", nil)
	}

	jc.atlassianClient = ac
	jc.siteIDs = siteIDs
	return jc, nil
}

func (j *Jira) Validate(ctx context.Context) (annotations.Annotations, error) {
	_, resp, err := j.client.Jira().User.Find(ctx, "")
	if err != nil {
		var statusCode *int
		if resp != nil {
			statusCode = &resp.StatusCode
		}
		return nil, wrapError(err, "failed to get users", statusCode)
	}

	/* Try to list groups to validate permissions/correctness of URL.
	- If unauthorized, try switching to scoped token URL (using scoped tokens with wrong URL gives 401).
	- if is authorized after switching, return error indicating the need to update the jira-url flag. to the new scoped token URL.
	- If still unauthorized, return error indicating a lack of permissions.

		legacy url (for unscoped tokens): https://your-domain.atlassian.net
		scoped token url: https://api.atlassian.com/ex/jira/<cloud-id>
	*/
	_, resp, err = j.client.Jira().Group.Bulk(ctx, jira.WithMaxResults(1))
	if err == nil {
		return nil, nil
	}

	if resp == nil {
		return nil, wrapError(err, "failed to list groups", nil)
	}

	if resp.StatusCode != http.StatusUnauthorized {
		return nil, wrapError(err, "failed to list groups", &resp.StatusCode)
	}

	// if we get unauthorized, try again with the scoped token URL
	if client.IsScopedTokenURL(j.originalURL) {
		return nil, wrapError(err, "unauthorized access to groups - check that the API token has the necessary permissions", &resp.StatusCode)
	}

	err = j.SwitchToScopedTokenUrl(ctx)
	if err != nil {
		return nil, wrapError(err, "failed to switch to scoped token URL", nil)
	}

	// try the endpoint again but with the scoped token URL
	_, resp, err = j.client.Jira().Group.Bulk(ctx, jira.WithMaxResults(1))
	if err != nil {
		var statusCode *int
		if resp != nil {
			statusCode = &resp.StatusCode
		}
		return nil, wrapError(err, "failed to list groups after scoped token fallback", statusCode)
	}

	// error with message indicating the need to switch to scoped token URL
	newUrl := j.client.Jira().BaseURL.String()
	return nil, fmt.Errorf("jira-url flag needs to be updated to use this scoped token URL: %s", newUrl)
}

func (o *Jira) SwitchToScopedTokenUrl(ctx context.Context) error {
	l := ctxzap.Extract(ctx)
	l.Info("attempting  scoped token URL", zap.String("original_url", o.originalURL))

	newClient, err := client.NewWithScopedToken(ctx, o.username, o.apiToken, o.originalURL)
	if err != nil {
		return fmt.Errorf("error creating jira client with scoped token URL: %w", err)
	}

	o.client.UpdateJiraClient(newClient.Jira())
	return nil
}

func (o *Jira) ResourceSyncers(ctx context.Context) []connectorbuilder.ResourceSyncerV2 {
	return []connectorbuilder.ResourceSyncerV2{
		userBuilder(o.client, o.atlassianClient, o.skipCustomerUser, o.siteIDs),
		groupBuilder(o.client, o.atlassianClient, o.siteIDs),
		projectRoleBuilder(o.client),
		projectBuilder(o.client, o.skipProjectParticipants),
	}
}

func (o *Jira) Metadata(ctx context.Context) (*v2.ConnectorMetadata, error) {
	return &v2.ConnectorMetadata{
		DisplayName: "Jira",
		Description: "Connector syncing Jira users and their groups and projects to Baton.",
		AccountCreationSchema: &v2.ConnectorAccountCreationSchema{
			FieldMap: map[string]*v2.ConnectorAccountCreationSchema_Field{
				"email": {
					DisplayName: "Email",
					Required:    true,
					Description: "The email address of the user.",
					Field: &v2.ConnectorAccountCreationSchema_Field_StringField{
						StringField: &v2.ConnectorAccountCreationSchema_StringField{},
					},
					Placeholder: "Email",
					Order:       1,
				},
				"products": {
					DisplayName: "Products",
					Required:    true,
					Description: "Products the new user has access to",
					Field: &v2.ConnectorAccountCreationSchema_Field_StringListField{
						StringListField: &v2.ConnectorAccountCreationSchema_StringListField{},
					},
					Placeholder: "Products",
					Order:       2,
				},
			},
		},
	}, nil
}
