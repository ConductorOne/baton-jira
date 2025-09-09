package connector

import (
	"context"

	"github.com/conductorone/baton-jira/pkg/client"
	cfg "github.com/conductorone/baton-jira/pkg/config"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/cli"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	jira "github.com/conductorone/go-jira/v2/cloud"
)

type (
	Jira struct {
		client                  *client.Client
		projectKeys             []string
		skipProjectParticipants bool
		skipCustomerUser        bool
	}
)

var _ connectorbuilder.ConnectorBuilderV2 = (*Jira)(nil)

func New(ctx context.Context, jc *cfg.Jira, connectorOpts *cli.ConnectorOpts) (connectorbuilder.ConnectorBuilderV2, []connectorbuilder.Opt, error) {
	transport := jira.BasicAuthTransport{
		Username: jc.JiraEmail,
		APIToken: jc.JiraApiToken,
	}

	c, err := client.New(jc.JiraUrl, transport.Client())
	if err != nil {
		return nil, nil, wrapError(err, "error creating jira client", nil)
	}
	opts := []connectorbuilder.Opt{}
	if jc.Ticketing {
		opts = append(opts, connectorbuilder.WithTicketingEnabled())
	}

	return &Jira{
		client:                  c,
		projectKeys:             jc.JiraProjectKeys,
		skipProjectParticipants: jc.SkipProjectParticipants,
		skipCustomerUser:        jc.SkipCustomerUser,
	}, opts, nil
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
		userBuilder(o.client, o.skipCustomerUser),
		groupBuilder(o.client),
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
