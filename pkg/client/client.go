package client

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/conductorone/baton-sdk/pkg/session"
	"github.com/conductorone/baton-sdk/pkg/types/sessions"
	jira "github.com/conductorone/go-jira/v2/cloud"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AuditRecord = jira.AuditRecord
type AuditOptions = jira.AuditOptions

var rolesNamespace = sessions.WithPrefix("role")
var projectsNamespace = sessions.WithPrefix("project")

func wrapJiraErrorResponse(err error, resp *jira.Response, message string) error {
	var statusCode *int
	if resp != nil {
		statusCode = &resp.StatusCode
	}
	return WrapError(err, message, statusCode)
}

func WrapError(err error, message string, statusCode *int) error {
	if statusCode == nil {
		return fmt.Errorf("jira-connector: %s: %w", message, err)
	}

	switch *statusCode {
	case http.StatusRequestTimeout:
		return status.Error(codes.DeadlineExceeded, fmt.Sprintf("%s: %v", message, err))
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return status.Error(codes.Unavailable, fmt.Sprintf("%s: %v", message, err))
	case http.StatusUnauthorized:
		return status.Error(codes.Unauthenticated, fmt.Sprintf("%s: %v", message, err))
	case http.StatusNotFound:
		return status.Error(codes.NotFound, fmt.Sprintf("%s: %v", message, err))
	case http.StatusForbidden:
		return status.Error(codes.PermissionDenied, fmt.Sprintf("%s: %v", message, err))
	case http.StatusNotImplemented:
		return status.Error(codes.Unimplemented, fmt.Sprintf("%s: %v", message, err))
	default:
		return fmt.Errorf("jira-connector: %s: %w", message, err)
	}
}

type Client struct {
	jira         *jira.Client
	projectCache sync.Map
}

func (c *Client) Jira() *jira.Client {
	return c.jira
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

func (c *Client) GetProjectFromSessionStore(ctx context.Context, ss sessions.SessionStore, projectID string) (*jira.Project, error) {
	project, found, err := session.GetJSON[*jira.Project](ctx, ss, projectID, projectsNamespace)
	if err != nil {
		return nil, err
	}

	if found {
		return project, nil
	}

	prj, resp, err := c.jira.Project.Get(ctx, projectID)
	if err != nil {
		return nil, wrapJiraErrorResponse(err, resp, "failed to get project")
	}

	err = session.SetJSON(ctx, ss, projectID, prj, projectsNamespace)
	if err != nil {
		return nil, err
	}

	return prj, nil
}

func (c *Client) GetRole(ctx context.Context, ss sessions.SessionStore, roleID int) (*jira.Role, error) {
	sRoleID := strconv.Itoa(roleID)
	role, found, err := session.GetJSON[*jira.Role](ctx, ss, sRoleID, rolesNamespace)
	if err != nil {
		return nil, err
	}

	if found {
		return role, nil
	}

	r, resp, err := c.jira.Role.Get(ctx, roleID)
	if err != nil {
		return nil, wrapJiraErrorResponse(err, resp, "failed to get role")
	}

	err = session.SetJSON(ctx, ss, sRoleID, r, rolesNamespace)
	if err != nil {
		return nil, err
	}

	return r, nil
}

func (c *Client) GetProjects(ctx context.Context, ss sessions.SessionStore, projectIDs []string) (map[string]*jira.Project, error) {
	cachedProjects, err := session.GetManyJSON[*jira.Project](ctx, ss, projectIDs, projectsNamespace)
	if err != nil {
		return nil, err
	}
	newProjectIDs := []string{}
	for _, sRoleID := range projectIDs {
		_, ok := cachedProjects[sRoleID]
		if !ok {
			newProjectIDs = append(newProjectIDs, sRoleID)
		}
	}
	newProjects := make(map[string]*jira.Project)
	for _, pid := range newProjectIDs {
		project, resp, err := c.jira.Project.Get(ctx, pid)
		if err != nil {
			return nil, wrapJiraErrorResponse(err, resp, "failed to get project")
		}
		newProjects[pid] = project
		cachedProjects[pid] = project
	}
	err = session.SetManyJSON(ctx, ss, newProjects, projectsNamespace)
	if err != nil {
		return nil, err
	}
	return cachedProjects, nil
}

func (c *Client) GetRoles(ctx context.Context, ss sessions.SessionStore, roleIDs []int) (map[string]*jira.Role, error) {
	sRoleIDs := make([]string, 0, len(roleIDs))
	for _, roleID := range roleIDs {
		sRoleIDs = append(sRoleIDs, strconv.Itoa(roleID))
	}
	cachedRoles, err := session.GetManyJSON[*jira.Role](ctx, ss, sRoleIDs, rolesNamespace)
	if err != nil {
		return nil, err
	}
	newRoleIDs := make([]string, 0, len(roleIDs))
	for _, sRoleID := range sRoleIDs {
		_, ok := cachedRoles[sRoleID]
		if !ok {
			newRoleIDs = append(newRoleIDs, sRoleID)
		}
	}
	newRoles := make(map[string]*jira.Role)
	for _, sRoleID := range newRoleIDs {
		roleID, err := strconv.Atoi(sRoleID)
		if err != nil {
			return nil, err
		}
		role, resp, err := c.jira.Role.Get(ctx, roleID)
		if err != nil {
			return nil, wrapJiraErrorResponse(err, resp, "failed to get role")
		}
		newRoles[sRoleID] = role
		cachedRoles[sRoleID] = role
	}
	err = session.SetManyJSON(ctx, ss, newRoles, rolesNamespace)
	if err != nil {
		return nil, err
	}
	return cachedRoles, nil
}

func (c *Client) GetProject(ctx context.Context, projectID string) (*jira.Project, error) {
	project, ok := c.projectCache.Load(projectID)
	if ok {
		return project.(*jira.Project), nil
	}

	prj, resp, err := c.jira.Project.Get(ctx, projectID)
	if err != nil {
		return nil, wrapJiraErrorResponse(err, resp, "failed to get project")
	}

	c.projectCache.Store(projectID, prj)

	return prj, nil
}
