package connector

import (
	"context"

	"github.com/conductorone/baton-jira/pkg/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
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

	JiraBuilder interface {
		New() (*Jira, error)
	}

	JiraOptions struct {
		Url         string
		ProjectKeys []string
	}

	JiraBasicAuthBuilder struct {
		Base *JiraOptions

		Username string
		ApiToken string
	}
)

func (b *JiraBasicAuthBuilder) New(skipProjectParticipants bool, skipCustomerUser bool) (*Jira, error) {
	transport := jira.BasicAuthTransport{
		Username: b.Username,
		APIToken: b.ApiToken,
	}

	c, err := client.New(b.Base.Url, transport.Client())
	if err != nil {
		return nil, wrapError(err, "error creating jira client")
	}

	return &Jira{
		client:                  c,
		projectKeys:             b.Base.ProjectKeys,
		skipProjectParticipants: skipProjectParticipants,
		skipCustomerUser:        skipCustomerUser,
	}, nil
}

func (j *Jira) Validate(ctx context.Context) (annotations.Annotations, error) {
	_, _, err := j.client.Jira().User.Find(ctx, "")
	if err != nil {
		return nil, wrapError(err, "failed to get users")
	}

	_, _, err = j.client.Jira().Project.GetAll(ctx, nil)
	if err != nil {
		return nil, wrapError(err, "failed to get projects")
	}

	return nil, nil
}

func (o *Jira) ResourceSyncers(ctx context.Context) []connectorbuilder.ResourceSyncer {
	return []connectorbuilder.ResourceSyncer{
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
