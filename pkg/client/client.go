package client

import (
	"context"
	"encoding/json"
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
	cache, err := session.GetSession(ctx)
	if err != nil {
		panic(err)
	}

	project, ok, err := cache.Get(ctx, projectID)
	if err == nil && ok {
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

	err = cache.Set(ctx, projectID, bytes)
	if err != nil {
		return nil, err
	}

	return prj, nil
}

func (c *Client) GetProjects(ctx context.Context, projectIDs ...string) ([]*jira.Project, error) {
	// Try to get projects from cache first
	cache, err := session.GetSession(ctx)
	if err != nil {
		panic(err)
	}

	projectMap, err := cache.GetMany(ctx, projectIDs)
	if err != nil {
		return nil, err
	}

	// Track which projects we found in cache and which we need to fetch
	projects := make([]*jira.Project, 0, len(projectIDs))
	missingProjectIDs := make([]string, 0, len(projectIDs))

	for _, projectID := range projectIDs {
		if projectBytes, exists := projectMap[projectID]; !exists {
			missingProjectIDs = append(missingProjectIDs, projectID)
		} else {
			var prj jira.Project
			err = json.Unmarshal(projectBytes, &prj)
			if err != nil {
				return nil, err
			}
			projects = append(projects, &prj)
		}
	}

	// If we have missing projects, fetch them from the API
	if len(missingProjectIDs) > 0 {
		projectBytesMap := make(map[string][]byte)

		for _, projectID := range missingProjectIDs {
			prj, _, err := c.jira.Project.Get(ctx, projectID)
			if err != nil {
				return nil, err
			}

			bytes, err := json.Marshal(prj)
			if err != nil {
				return nil, err
			}

			projects = append(projects, prj)
			projectBytesMap[projectID] = bytes
		}

		// Set the fetched projects in cache
		err = cache.SetMany(ctx, projectBytesMap)
		if err != nil {
			return nil, err
		}
	}

	return projects, nil
}

func (c *Client) SetProjects(ctx context.Context, projects []jira.Project) error {
	cache, err := session.GetSession(ctx)
	if err != nil {
		panic(err)
	}

	projectBytesMap := make(map[string][]byte)

	for _, project := range projects {
		bytes, err := json.Marshal(project)
		if err != nil {
			return err
		}
		projectBytesMap[project.ID] = bytes
	}

	return cache.SetMany(ctx, projectBytesMap)
}

func (c *Client) GetRole(ctx context.Context, roleID int) (*jira.Role, error) {
	cache, err := session.GetSession(ctx)
	if err != nil {
		panic(err)
	}

	role, ok, err := cache.Get(ctx, "role:"+strconv.Itoa(roleID))
	if err != nil {
		return nil, err
	}

	if ok {
		var r jira.Role
		err = json.Unmarshal(role, &r)
		if err != nil {
			return nil, err
		}
		return &r, nil
	}
	r, _, err := c.jira.Role.Get(ctx, roleID)
	if err != nil {
		return nil, err
	}

	bytes, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}

	err = cache.Set(ctx, "role:"+strconv.Itoa(roleID), bytes)
	if err != nil {
		return nil, err
	}

	return r, nil
}

func New(_ context.Context, url string, httpClient *http.Client) (*Client, error) {
	jira, err := jira.NewClient(url, httpClient)
	if err != nil {
		return nil, err
	}

	return &Client{jira: jira}, nil
}
