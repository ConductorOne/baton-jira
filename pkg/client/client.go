package client

import (
	"context"
	"net/http"
	"sync"

	jira "github.com/conductorone/go-jira/v2/cloud"
)

type AuditRecord = jira.AuditRecord
type AuditOptions = jira.AuditOptions

type Client struct {
	jira         *jira.Client
	projectCache sync.Map
	roleCache    sync.Map
}

func (c *Client) Jira() *jira.Client {
	return c.jira
}

func (c *Client) GetProject(ctx context.Context, projectID string) (*jira.Project, error) {
	project, ok := c.projectCache.Load(projectID)
	if ok {
		return project.(*jira.Project), nil
	}

	prj, _, err := c.jira.Project.Get(ctx, projectID)
	if err != nil {
		return nil, err
	}

	c.projectCache.Store(projectID, prj)

	return prj, nil
}

func (c *Client) GetRole(ctx context.Context, roleID int) (*jira.Role, error) {
	role, ok := c.roleCache.Load(roleID)
	if ok {
		return role.(*jira.Role), nil
	}

	r, _, err := c.jira.Role.Get(ctx, roleID)
	if err != nil {
		return nil, err
	}

	c.roleCache.Store(roleID, r)

	return r, nil
}

func New(url string, httpClient *http.Client) (*Client, error) {
	jira, err := jira.NewClient(url, httpClient)
	if err != nil {
		return nil, err
	}

	return &Client{
		jira: jira,
	}, nil
}
