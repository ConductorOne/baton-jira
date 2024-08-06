package connector

import (
	"context"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	jira "github.com/conductorone/go-jira/v2/cloud"
)

type (
	Jira struct {
		client      *jira.Client
		metaProject *jira.MetaProject
	}

	JiraBuilder interface {
		New() (*Jira, error)
	}

	JiraOptions struct {
		Url string
	}

	JiraBasicAuthBuilder struct {
		Base *JiraOptions

		Username string
		ApiToken string
	}
)

func (b *JiraBasicAuthBuilder) New() (*Jira, error) {
	transport := jira.BasicAuthTransport{
		Username: b.Username,
		APIToken: b.ApiToken,
	}

	client, err := jira.NewClient(b.Base.Url, transport.Client())
	if err != nil {
		return nil, wrapError(err, "error creating jira client")
	}

	return &Jira{
		client: client,
	}, nil
}

func (j *Jira) Validate(ctx context.Context) (annotations.Annotations, error) {
	_, _, err := j.client.User.Find(ctx, "")
	if err != nil {
		return nil, wrapError(err, "failed to get users")
	}

	_, _, err = j.client.Project.GetAll(ctx, nil)
	if err != nil {
		return nil, wrapError(err, "failed to get projects")
	}

	return nil, nil
}

func (o *Jira) ResourceSyncers(ctx context.Context) []connectorbuilder.ResourceSyncer {
	return []connectorbuilder.ResourceSyncer{
		userBuilder(o.client),
		groupBuilder(o.client),
		projectBuilder(o.client),
		roleBuilder(o.client),
	}
}

func (o *Jira) Metadata(ctx context.Context) (*v2.ConnectorMetadata, error) {
	return &v2.ConnectorMetadata{
		DisplayName: "Jira",
		Description: "Connector syncing Jira users and their groups and projects to Baton.",
	}, nil
}
