package connector

import (
	"context"
	"errors"

	jira "github.com/andygrunwald/go-jira/v2/cloud"
)

// TODO: implement your connector here
type Jira struct {
	client *jira.Client
}

type JiraOAuthOptions struct {
	PrivateKeyPath  string
	ConsumerKeyPath string
}

type JiraBasicAuthOptions struct {
	Username string
	ApiKey   string
}

type JiraOptions struct {
	Url       string
	BasicAuth *JiraBasicAuthOptions
	OAuth     *JiraOAuthOptions
}

func New(ctx context.Context, options JiraOptions) (*Jira, error) {
	if (options.BasicAuth == nil && options.OAuth == nil) || (options.BasicAuth != nil && options.OAuth != nil) {
		return nil, errors.New("jira-connector: either BasicAuth or OAuth must be provided")
	}

	if options.BasicAuth != nil {
		return newBasicAuthJira(ctx, options)
	}

	return newOAuthJira(ctx, options)
}

func newOAuthJira(ctx context.Context, options JiraOptions) (*Jira, error) {
	return &Jira{}, nil
}

func newBasicAuthJira(ctx context.Context, options JiraOptions) (*Jira, error) {
	return &Jira{}, nil
}
