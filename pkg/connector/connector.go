package connector

import (
	jira "github.com/andygrunwald/go-jira/v2/cloud"
)

type (
	Jira struct {
		client *jira.Client
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
		return nil, err
	}

	return &Jira{
		client: client,
	}, nil
}
