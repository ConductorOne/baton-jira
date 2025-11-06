package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"

	jira "github.com/conductorone/go-jira/v2/cloud"
)

type AuditRecord = jira.AuditRecord
type AuditOptions = jira.AuditOptions

const GetUsers = "/rest/api/3/users/search?%s"

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

// FindUsersV3 calls the Jira API v3 /rest/api/3/users/search endpoint directly
// This endpoint returns all users including inactive ones, unlike the Find method in the go-jira library.
func (c *Client) FindUsersV3(ctx context.Context, startAt, maxResults int) ([]jira.User, *jira.Response, error) {
	query := url.Values{}
	query.Set("startAt", fmt.Sprintf("%d", startAt))
	query.Set("maxResults", fmt.Sprintf("%d", maxResults))

	apiURL := fmt.Sprintf(GetUsers, query.Encode())

	req, err := c.Jira().NewRequest(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, nil, err
	}

	var users []jira.User
	resp, err := c.Jira().Do(req, &users)
	if err != nil {
		return nil, resp, err
	}

	return users, resp, nil
}
