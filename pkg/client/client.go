package client

import (
	"context"
	"net/http"
	"strconv"

	"github.com/conductorone/baton-sdk/pkg/session"
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

func (c *Client) GetProject(ctx context.Context, projectID string) (*jira.Project, error) {
	l := ctxzap.Extract(ctx)

	project, ok, err := session.GetJSON[*jira.Project](ctx, projectID)
	if err != nil {
		l.Error("Error getting project from cache", zap.Error(err))
	}
	// project, ok := c.projectCache.Load(projectID)
	if ok {
		l.Info("Got project from cache", zap.String("projectID", projectID))
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
	l.Info("Stored project in cache", zap.String("projectID", projectID))

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
	l := ctxzap.Extract(ctx)

	role, ok, err := session.GetJSON[jira.Role](ctx, "role:"+strconv.Itoa(roleID))
	if err != nil {
		l.Error("Error getting role from cache", zap.Error(err))
		// return nil, err
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
	l.Info("Stored role in cache", zap.Int("roleID", roleID))

	return r, nil
}

func (c *Client) GetRoles(ctx context.Context, roleIDs []int) ([]*jira.Role, error) {
	l := ctxzap.Extract(ctx)
	roleIDsStr := make([]string, len(roleIDs))
	for i, roleID := range roleIDs {
		roleIDsStr[i] = strconv.Itoa(roleID)
	}
	res, err := session.GetManyJSON[*jira.Role](ctx, roleIDsStr, session.WithPrefix("role"))
	if err != nil {
		return nil, err
	}
	l.Info("🌮 Got roles from cache", zap.Ints("roleIDs", roleIDs), zap.Int("count", len(res)))

	missingRoleIDs := make([]int, 0, len(roleIDs))
	if len(res) != len(roleIDs) {
		for _, roleID := range roleIDs {
			if _, exists := res[strconv.Itoa(roleID)]; !exists {
				missingRoleIDs = append(missingRoleIDs, roleID)
			}
		}
	}

	l.Info("Missing roles", zap.Ints("roleIDs", missingRoleIDs))
	newRoles := make(map[string]*jira.Role)
	for _, roleID := range missingRoleIDs {
		r, _, err := c.jira.Role.Get(ctx, roleID)
		if err != nil {
			return nil, err
		}
		newRoles[strconv.Itoa(roleID)] = r
	}

	if len(newRoles) > 0 {
		l.Info("🌮 Setting roles in cache", zap.Ints("roleIDs", missingRoleIDs), zap.Int("count", len(newRoles)))
		err = session.SetManyJSON(ctx, newRoles, session.WithPrefix("role"))
		if err != nil {
			return nil, err
		}
	}

	ret := make([]*jira.Role, 0, len(roleIDs))
	for _, role := range res {
		l.Info("🌮 Adding role to result", zap.Int("roleID", role.ID))
		ret = append(ret, role)
	}
	for _, role := range newRoles {
		l.Info("🌮 Adding role to result", zap.Int("roleID", role.ID))
		ret = append(ret, role)
	}

	l.Info("🌮 Returning roles", zap.Ints("roleIDs", roleIDs), zap.Int("count", len(ret)))
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
