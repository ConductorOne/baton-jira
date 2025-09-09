package client

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/conductorone/baton-sdk/pkg/session"
	"github.com/conductorone/baton-sdk/pkg/types/sessions"
	jira "github.com/conductorone/go-jira/v2/cloud"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

type AuditRecord = jira.AuditRecord
type AuditOptions = jira.AuditOptions

type Client struct {
	jira *jira.Client
}

func (c *Client) Jira() *jira.Client {
	return c.jira
}

func (c *Client) GetProject(ctx context.Context, ss sessions.SessionStore, projectID string) (*jira.Project, error) {
	l := ctxzap.Extract(ctx)

	project, ok, err := session.GetJSON[*jira.Project](ctx, ss, projectID)
	if err != nil {
		l.Error("Error getting project from cache", zap.Error(err))
		return nil, fmt.Errorf("failed to get project from cache: %w", err)
	}

	if ok {
		return project, nil
	}

	prj, _, err := c.jira.Project.Get(ctx, projectID)
	if err != nil {
		return nil, err
	}

	err = session.SetJSON(ctx, ss, projectID, prj)
	if err != nil {
		return nil, err
	}
	return prj, nil
}

func (c *Client) GetProjects(ctx context.Context, ss sessions.SessionStore, projectIDs ...string) ([]*jira.Project, error) {
	// Try to get projects from cache first
	projectMap, err := session.GetManyJSON[*jira.Project](ctx, ss, projectIDs)
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
		err = session.SetManyJSON(ctx, ss, projectMap)
		if err != nil {
			return nil, err
		}
	}

	return projects, nil
}

func (c *Client) SetProjects(ctx context.Context, ss sessions.SessionStore, projects []jira.Project) error {
	projectMap := make(map[string]*jira.Project)
	for _, project := range projects {
		projectMap[project.ID] = &project
	}

	return session.SetManyJSON(ctx, ss, projectMap)
}

func (c *Client) GetRole(ctx context.Context, ss sessions.SessionStore, roleID int) (*jira.Role, error) {
	l := ctxzap.Extract(ctx)

	role, ok, err := session.GetJSON[jira.Role](ctx, ss, "role:"+strconv.Itoa(roleID))
	if err != nil {
		l.Error("Error getting role from cache", zap.Error(err))
		return nil, fmt.Errorf("failed to get role from cache: %w", err)
	}

	if ok {
		return &role, nil
	}
	r, _, err := c.jira.Role.Get(ctx, roleID)
	if err != nil {
		return nil, err
	}

	err = session.SetJSON(ctx, ss, "role:"+strconv.Itoa(roleID), r)
	if err != nil {
		return nil, fmt.Errorf("failed to set role in cache: %w", err)
	}

	return r, nil
}

func (c *Client) GetRoles(ctx context.Context, ss sessions.SessionStore, roleIDs []int) ([]*jira.Role, error) {
	roleIDsStr := make([]string, len(roleIDs))
	for i, roleID := range roleIDs {
		roleIDsStr[i] = strconv.Itoa(roleID)
	}
	res, err := session.GetManyJSON[*jira.Role](ctx, ss, roleIDsStr, sessions.WithPrefix("role"))
	if err != nil {
		return nil, fmt.Errorf("failed to get roles from cache: %w", err)
	}

	missingRoleIDs := make([]int, 0, len(roleIDs))
	if len(res) != len(roleIDs) {
		for _, roleID := range roleIDs {
			if _, exists := res[strconv.Itoa(roleID)]; !exists {
				missingRoleIDs = append(missingRoleIDs, roleID)
			}
		}
	}

	newRoles := make(map[string]*jira.Role)
	for _, roleID := range missingRoleIDs {
		r, _, err := c.jira.Role.Get(ctx, roleID)
		if err != nil {
			return nil, err
		}
		newRoles[strconv.Itoa(roleID)] = r
	}

	if len(newRoles) > 0 {
		err = session.SetManyJSON(ctx, ss, newRoles, sessions.WithPrefix("role"))
		if err != nil {
			return nil, fmt.Errorf("failed to set roles in cache: %w", err)
		}
	}

	ret := make([]*jira.Role, 0, len(roleIDs))
	for _, role := range res {
		ret = append(ret, role)
	}
	for _, role := range newRoles {
		ret = append(ret, role)
	}

	return ret, nil
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
