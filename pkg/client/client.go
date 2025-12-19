package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/conductorone/baton-sdk/pkg/session"
	"github.com/conductorone/baton-sdk/pkg/types/sessions"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	jira "github.com/conductorone/go-jira/v2/cloud"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AuditRecord = jira.AuditRecord
type AuditOptions = jira.AuditOptions

const GetUsers = "/rest/api/3/users/search?%s"
const ScopedTokenBaseURL = "https://api.atlassian.com/ex/jira/%s"
const TenantInfoEndpoint = "/_edge/tenant_info"

var rolesNamespace = sessions.WithPrefix("role")
var projectsNamespace = sessions.WithPrefix("project")

type tenantInfo struct {
	CloudID string `json:"cloudId"`
}

func IsServiceAccount(email string) bool {
	return strings.HasSuffix(email, "@serviceaccount.atlassian.com")
}

func IsScopedTokenURL(url string) bool {
	return strings.Contains(url, "api.atlassian.com/ex/jira/")
}

// NewHTTPClient creates a new uhttp client with logging enabled.
func NewHTTPClient(ctx context.Context) (*uhttp.BaseHttpClient, error) {
	httpClient, err := uhttp.NewClient(ctx, uhttp.WithLogger(true, ctxzap.Extract(ctx)))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	wrapper, err := uhttp.NewBaseHttpClientWithContext(ctx, httpClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP wrapper: %w", err)
	}

	return wrapper, nil
}

// Will get the Cloud ID from the tenant info endpoint, only for service accounts.
// the cloud id is required to build the correct base URL for service accounts requests.
func ResolveCloudID(ctx context.Context, jiraURL string) (string, error) {
	if jiraURL == "" {
		return "", status.Error(codes.InvalidArgument, "jira URL cannot be empty")
	}

	parsedURL, err := url.Parse(jiraURL)
	if err != nil {
		return "", status.Error(codes.InvalidArgument, fmt.Sprintf("invalid jira URL: %v", err))
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return "", status.Error(codes.InvalidArgument, "jira URL must include scheme and host")
	}

	wrapper, err := NewHTTPClient(ctx)
	if err != nil {
		return "", err
	}

	tenantInfoURLStr := strings.TrimSuffix(jiraURL, "/") + TenantInfoEndpoint
	tenantInfoURL, err := url.Parse(tenantInfoURLStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse tenant info URL %s: %w", tenantInfoURLStr, err)
	}

	req, err := wrapper.NewRequest(ctx, http.MethodGet, tenantInfoURL)
	if err != nil {
		return "", fmt.Errorf("failed to create request for tenant info endpoint %s: %w", tenantInfoURLStr, err)
	}

	var info tenantInfo
	resp, err := wrapper.Do(req, uhttp.WithJSONResponse(&info))
	if err != nil {
		return "", fmt.Errorf("failed to call tenant info endpoint %s (check if URL is accessible): %w", tenantInfoURLStr, err)
	}
	if resp != nil {
		defer resp.Body.Close()
	}

	if info.CloudID == "" {
		return "", fmt.Errorf("cloudId field not found or empty in tenant info response from %s", tenantInfoURLStr)
	}

	return info.CloudID, nil
}

// GetScopedTokenUrl determines the base URL to use for API calls.
// Service accounts (example@serviceaccount.atlassian.com) and scoped tokens use:
//
//	https://api.atlassian.com/ex/jira/<cloud-id>
//
// while regular accounts use:
//
//	https://<cloud-name>.atlassian.net.
func GetScopedTokenUrl(ctx context.Context, jiraURL string) (string, error) {
	if jiraURL == "" {
		return "", status.Error(codes.InvalidArgument, "jira URL cannot be empty")
	}

	parsedURL, err := url.Parse(jiraURL)
	if err != nil {
		return "", status.Error(codes.InvalidArgument, fmt.Sprintf("invalid jira URL: %v", err))
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return "", status.Error(codes.InvalidArgument, "jira URL must include scheme and host")
	}

	cloudID, err := ResolveCloudID(ctx, jiraURL)
	if err != nil {
		return "", fmt.Errorf("failed to resolve scoped token url: %w", err)
	}

	serviceURL := fmt.Sprintf(ScopedTokenBaseURL, cloudID)
	return serviceURL, nil
}

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

func (c *Client) UpdateJiraClient(newJiraClient *jira.Client) {
	c.jira = newJiraClient
}

// creates a new client with service account support. It resolves the appropriate
// base URL based on the email (service accounts use a different API endpoint).
func NewWithScopedToken(ctx context.Context, username, apiToken, jiraURL string) (*Client, error) {
	resolvedURL, err := GetScopedTokenUrl(ctx, jiraURL)
	if err != nil {
		return nil, WrapError(err, "failed to resolve base URL", nil)
	}

	return New(username, apiToken, resolvedURL)
}

func NewHttpClient(username, apiToken string) *http.Client {
	transport := jira.BasicAuthTransport{
		Username: username,
		APIToken: apiToken,
	}

	return transport.Client()
}

func New(username, apiToken, url string) (*Client, error) {
	httpClient := NewHttpClient(username, apiToken)
	jira, err := jira.NewClient(url, httpClient)
	if err != nil {
		return nil, err
	}

	return &Client{
		jira: jira,
	}, nil
}

func (c *Client) GetProject(ctx context.Context, ss sessions.SessionStore, projectID string) (*jira.Project, error) {
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
	if len(newProjects) > 0 {
		err = session.SetManyJSON(ctx, ss, newProjects, projectsNamespace)
		if err != nil {
			return nil, err
		}
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
	if len(newRoles) > 0 {
		err = session.SetManyJSON(ctx, ss, newRoles, rolesNamespace)
		if err != nil {
			return nil, err
		}
	}
	return cachedRoles, nil
}

func (c *Client) GetProjectForTicket(ctx context.Context, projectID string) (*jira.Project, error) {
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
