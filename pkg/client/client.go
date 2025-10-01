package client

import (
	"context"
	"net/http"
	"sync"

	jira "github.com/conductorone/go-jira/v2/cloud"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
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
	l := ctxzap.Extract(ctx)

	project, ok := c.projectCache.Load(projectID)
	if ok {
		l.Info("Got project from cache", zap.String("projectID", projectID))
		return project.(*jira.Project), nil
	}

	prj, _, err := c.jira.Project.Get(ctx, projectID)
	if err != nil {
		return nil, err
	}

	c.projectCache.Store(projectID, prj)
	l.Info("Stored project in cache", zap.String("projectID", projectID))

	return prj, nil
}

func (c *Client) GetRole(ctx context.Context, roleID int) (*jira.Role, error) {
	l := ctxzap.Extract(ctx)

	role, ok := c.roleCache.Load(roleID)
	if ok {
		l.Info("Got role from cache", zap.Int("roleID", roleID))
		return role.(*jira.Role), nil
	}

	r, _, err := c.jira.Role.Get(ctx, roleID)
	if err != nil {
		return nil, err
	}

	c.roleCache.Store(roleID, r)
	l.Info("Stored role in cache", zap.Int("roleID", roleID))

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
