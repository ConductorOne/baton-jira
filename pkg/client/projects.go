package client

import (
	"context"

	jira "github.com/conductorone/go-jira/v2/cloud"
)

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

// https://docs.atlassian.com/software/jira/docs/api/REST/9.14.0/#api/2/project-createProject
func (c *Client) CreateProject(ctx context.Context) error {
	return nil
}

// https://docs.atlassian.com/software/jira/docs/api/REST/9.14.0/#api/2/project-deleteProject
func (c *Client) DeleteProject(ctx context.Context) error {
	return nil
}
