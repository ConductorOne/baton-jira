package client

import (
	"context"
	"net/http"
	"strconv"

	"github.com/conductorone/baton-sdk/pkg/session"
	jira "github.com/conductorone/go-jira/v2/cloud"
)

type AuditRecord = jira.AuditRecord
type AuditOptions = jira.AuditOptions

type Client struct {
	jira *jira.Client
}

func (c *Client) Jira() *jira.Client {
	return c.jira
}

func (c *Client) GetProject(ctx context.Context, projectID string) (*jira.Project, error) {
	project, ok, err := session.GetJSON[*jira.Project](ctx, projectID)
	if err == nil && ok {
		return project, nil
	}

	prj, _, err := c.jira.Project.Get(ctx, projectID)
	if err != nil {
		return nil, err
	}

	err = session.SetJSON(ctx, projectID, prj)
	if err != nil {
		return nil, err
	}

	return prj, nil
}

func (c *Client) GetProjects(ctx context.Context, projectIDs ...string) ([]*jira.Project, error) {
	// Try to get projects from cache first
	projectMap, err := session.GetManyJSON[*jira.Project](ctx, projectIDs)
	if err != nil {
		return nil, err
	}

	// Track which projects we found in cache and which we need to fetch
	projects := make([]*jira.Project, 0, len(projectIDs))
	missingProjectIDs := make([]string, 0, len(projectIDs))

	for _, projectID := range projectIDs {
		if project, exists := projectMap[projectID]; !exists {
			missingProjectIDs = append(missingProjectIDs, projectID)
		} else {
			projects = append(projects, project)
		}
	}

	// If we have missing projects, fetch them from the API
	if len(missingProjectIDs) > 0 {
		projectMap := make(map[string]*jira.Project)

		for _, projectID := range missingProjectIDs {
			prj, _, err := c.jira.Project.Get(ctx, projectID)
			if err != nil {
				return nil, err
			}

			projects = append(projects, prj)
			projectMap[projectID] = prj
		}

		// Set the fetched projects in cache
		err = session.SetManyJSON(ctx, projectMap)
		if err != nil {
			return nil, err
		}
	}

	return projects, nil
}

func (c *Client) SetProjects(ctx context.Context, projects []jira.Project) error {
	projectMap := make(map[string]*jira.Project)
	for _, project := range projects {
		projectMap[project.ID] = &project
	}

	return session.SetManyJSON(ctx, projectMap)
}

func (c *Client) GetRole(ctx context.Context, roleID int) (*jira.Role, error) {
	role, ok, err := session.GetJSON[jira.Role](ctx, "role:"+strconv.Itoa(roleID))
	if err != nil {
		return nil, err
	}

	if ok {
		return &role, nil
	}
	r, _, err := c.jira.Role.Get(ctx, roleID)
	if err != nil {
		return nil, err
	}

	err = session.SetJSON(ctx, "role:"+strconv.Itoa(roleID), r)
	if err != nil {
		return nil, err
	}

	return r, nil
}

func New(url string, httpClient *http.Client) (*Client, error) {
	jira, err := jira.NewClient(url, httpClient)
	if err != nil {
		return nil, err
	}

	return &Client{jira: jira}, nil
}
