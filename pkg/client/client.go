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

func (c *Client) GetProject(ctx context.Context, projectID string) (*jira.Project, *jira.Response, error) {
	project, ok := c.projectCache.Load(projectID)
	if ok {
		return project.(*jira.Project), nil, nil
	}

	prj, resp, err := c.jira.Project.Get(ctx, projectID)
	if err != nil {
		return nil, resp, err
	}

	c.projectCache.Store(projectID, prj)

	return prj, resp, nil
}

func (c *Client) GetRole(ctx context.Context, roleID int) (*jira.Role, *jira.Response, error) {
	role, ok := c.roleCache.Load(roleID)
	if ok {
		return role.(*jira.Role), nil, nil
	}

	r, resp, err := c.jira.Role.Get(ctx, roleID)
	if err != nil {
		return nil, resp, err
	}

	c.roleCache.Store(roleID, r)

	return r, resp, nil
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
