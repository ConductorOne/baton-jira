package connector

import (
	"context"

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
	transport := jira.BasicAuthTransport{
		Username: b.Username,
		APIToken: b.ApiToken,
	}

	httpClient := transport.Client()

	resolvedURL, err := client.ResolveURL(ctx, b.Username, b.Base.Url, httpClient)
	if err != nil {
		return nil, client.WrapError(err, "failed to resolve URL", nil)
	}

	c, err := client.New(resolvedURL, httpClient)
	if err != nil {
		return nil, client.WrapError(err, "error creating jira client", nil)
	}

	jc := &Jira{
		client:                  c,
		projectKeys:             b.Base.ProjectKeys,
		skipProjectParticipants: skipProjectParticipants,
		skipCustomerUser:        skipCustomerUser,
	}

	if b.Base.AtlassianBuilder == nil {
		return jc, nil
	}

	ac, siteIDs, err := atlassianclient.New(ctx,
		resolvedURL,
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

	_, resp, err = j.client.Jira().Project.GetAll(ctx, nil)
	if err != nil {
		var statusCode *int
		if resp != nil {
			statusCode = &resp.StatusCode
		}
		return nil, wrapError(err, "failed to get projects", statusCode)
	}

	return nil, nil
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
