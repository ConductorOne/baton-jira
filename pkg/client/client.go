package client

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/conductorone/baton-sdk/pkg/cli"
	"github.com/conductorone/baton-sdk/pkg/types"
	jira "github.com/conductorone/go-jira/v2/cloud"
)

type AuditRecord = jira.AuditRecord
type AuditOptions = jira.AuditOptions

type Client struct {
	jira         *jira.Client
	projectCache types.SessionCache
	roleCache    sync.Map
}

func (c *Client) Jira() *jira.Client {
	return c.jira
}

// func (c *Client) GetProjects(ctx context.Context, projectIDs ...string) ([]*jira.Project, error) {
// 	projects := make([]*jira.Project, 0, len(projectIDs))

// 	projectMap, err := c.projectCache.GetMany(ctx, projectIDs)
// 	if err != nil {
// 		return nil, err
// 	}

// 	for _, project := range projectMap {
// 		var prj jira.Project
// 		err = json.Unmarshal(project, &prj)
// 		if err != nil {
// 			return nil, err
// 		}
// 		projects = append(projects, &prj)
// 	}
// 	return projects, nil
// }

func (c *Client) GetProject(ctx context.Context, projectID string) (*jira.Project, error) {
	project, _, err := c.projectCache.Get(ctx, projectID)
	if err == nil {
		var prj jira.Project
		err = json.Unmarshal(project, &prj)
		if err != nil {
			return nil, err
		}
		return &prj, nil
	}

	prj, _, err := c.jira.Project.Get(ctx, projectID)
	if err != nil {
		return nil, err
	}

	bytes, err := json.Marshal(prj)
	if err != nil {
		return nil, err
	}
	err = c.projectCache.Set(ctx, projectID, bytes)
	if err != nil {
		return nil, err
	}

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

func New(ctx context.Context, url string, httpClient *http.Client) (*Client, error) {
	jira, err := jira.NewClient(url, httpClient)
	if err != nil {
		return nil, err
	}
	cache, err := cli.GetSessionCache(ctx)
	if err != nil {
		return nil, err
	}

	return &Client{
		jira:         jira,
		projectCache: cache,
	}, nil
}
