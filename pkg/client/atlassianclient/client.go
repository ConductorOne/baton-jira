package atlassianclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/conductorone/baton-jira/pkg/client"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
)

const (
	baseURL = "https://api.atlassian.com/admin"

	usersEP      = "admin/v2/orgs/%s/directories/-/users"
	workspacesEP = "v2/orgs/%s/workspaces"
	groupsEP     = "admin/v2/orgs/%s/directories/-/groups"
)

type AtlassianClient struct {
	wrapper *uhttp.BaseHttpClient
	config  Config
}

type Config struct {
	accessToken    string
	organizationID string
}

type Option func(*AtlassianClient)

func WithAccessToken(accessToken string) Option {
	return func(c *AtlassianClient) {
		c.config.accessToken = accessToken
	}
}

func WithOrganizationID(orgID string) Option {
	return func(c *AtlassianClient) {
		c.config.organizationID = orgID
	}
}

func (c *AtlassianClient) ListUsers(ctx context.Context, siteId, pageToken string) ([]User, string, error) {
	var usersResponse UserResponse
	u := &url.URL{
		Scheme:   "https",
		Host:     "api.atlassian.com",
		Path:     fmt.Sprintf(usersEP, c.config.organizationID),
		RawQuery: fmt.Sprintf("resourceIds=%s", siteId),
	}

	reqOpts := []ReqOpt{WithPageSize(maxItemsPerPage)}
	if pageToken != "" {
		reqOpts = append(reqOpts, WithPageToken(pageToken))
	}
	_, err := c.doRequest(ctx,
		http.MethodGet,
		u.String(),
		&usersResponse,
		nil,
		reqOpts...,
	)
	if err != nil {
		return nil, "", err
	}

	nextPageToken := usersResponse.Links.Next

	return usersResponse.Data, nextPageToken, nil
}

func (c *AtlassianClient) ListWorkspaces(ctx context.Context, pageToken string) ([]Workspace, string, error) {
	var workspacesResponse WorkspaceResponse
	requestURL, err := url.JoinPath(baseURL, fmt.Sprintf(workspacesEP, c.config.organizationID))
	if err != nil {
		return nil, "", err
	}

	// Pagination for this endpoint must be handled by sending the data in a json body instead of a query param.
	// If you sent the 'cursor' field, no others field can be provided, so the limit cannot be specified and should leave the API use the default value.
	body := struct {
		Cursor string `json:"cursor,omitempty"`
	}{
		Cursor: pageToken,
	}
	_, err = c.doRequest(ctx,
		http.MethodPost,
		requestURL,
		&workspacesResponse,
		body,
	)
	if err != nil {
		return nil, "", err
	}

	nextPageToken := workspacesResponse.Links.Next
	return workspacesResponse.Data, nextPageToken, nil
}

func (c *AtlassianClient) ListGroups(ctx context.Context, siteID string, pageToken string) ([]Group, string, error) {
	var groupsResponse GroupResponse
	u := &url.URL{
		Scheme:   "https",
		Host:     "api.atlassian.com",
		Path:     fmt.Sprintf(groupsEP, c.config.organizationID),
		RawQuery: fmt.Sprintf("resourceIds=%s", siteID),
	}

	reqOpts := []ReqOpt{WithPageSize(maxItemsPerPage)}
	if pageToken != "" {
		reqOpts = append(reqOpts, WithPageToken(pageToken))
	}
	_, err := c.doRequest(ctx,
		http.MethodGet,
		u.String(),
		&groupsResponse,
		nil,
		reqOpts...,
	)
	if err != nil {
		return nil, "", err
	}

	nextPageToken := groupsResponse.Links.Next

	return groupsResponse.Data, nextPageToken, nil
}

func (c *AtlassianClient) doRequest(
	ctx context.Context,
	method string,
	endpointUrl string,
	res interface{},
	body interface{},
	reqOpts ...ReqOpt,
) (http.Header, error) {
	var (
		resp   *http.Response
		apiErr APIError
		err    error
	)

	urlAddress, err := url.Parse(endpointUrl)
	if err != nil {
		return nil, err
	}

	for _, o := range reqOpts {
		o(urlAddress)
	}

	reqOptions := []uhttp.RequestOption{
		uhttp.WithBearerToken(c.config.accessToken),
	}
	if body != nil {
		reqOptions = append(reqOptions, uhttp.WithJSONBody(body))
	}

	req, err := c.wrapper.NewRequest(
		ctx,
		method,
		urlAddress,
		reqOptions...,
	)
	if err != nil {
		return nil, err
	}

	switch method {
	case http.MethodGet, http.MethodPut, http.MethodPost:
		doOptions := []uhttp.DoOption{
			uhttp.WithErrorResponse(&apiErr),
		}

		if res != nil {
			doOptions = append(doOptions, uhttp.WithResponse(&res))
		}
		resp, err = c.wrapper.Do(req, doOptions...)
		if resp != nil {
			defer resp.Body.Close()
		}

	case http.MethodDelete:
		resp, err = c.wrapper.Do(req)
		if resp != nil {
			defer resp.Body.Close()
		}
	}
	if err != nil {
		return nil, err
	}

	return resp.Header, nil
}

func New(ctx context.Context, siteurl string, clientOptions ...Option) (*AtlassianClient, []string, error) {
	wrapper, err := client.NewHTTPClient(ctx)
	if err != nil {
		return nil, nil, err
	}

	atlassianClient := AtlassianClient{
		wrapper: wrapper,
	}

	for _, opt := range clientOptions {
		opt(&atlassianClient)
	}

	siteIDs, err := atlassianClient.getSiteID(ctx, siteurl)
	if err != nil {
		return nil, nil, err
	}

	return &atlassianClient, siteIDs, nil
}

func (c *AtlassianClient) getSiteID(ctx context.Context, siteUrl string) ([]string, error) {
	var (
		workspaces  []Workspace
		pageToken   string
		wantSiteIDs []string
	)

	for {
		w, nextPage, err := c.ListWorkspaces(ctx, pageToken)
		if err != nil {
			return nil, err
		}

		workspaces = append(workspaces, w...)
		if nextPage == "" {
			break
		}
		pageToken = nextPage
	}

	for _, workspace := range workspaces {
		if workspace.Attributes.HostUrl == siteUrl {
			wantSiteIDs = append(wantSiteIDs, workspace.Id)
		}
	}

	if len(wantSiteIDs) == 0 {
		return nil, fmt.Errorf("site id not found")
	}
	return wantSiteIDs, nil
}
