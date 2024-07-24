package cloud

import (
	"context"
	"fmt"
	"net/http"
)

// StatusService handles staties for the Jira instance / API.
//
// Jira API docs: https://developer.atlassian.com/cloud/jira/platform/rest/v2/#api-group-Workflow-statuses
type StatusService service

// Status represents the current status of a Jira issue.
// Typical status are "Open", "In Progress", "Closed", ...
// Status can be user defined in every Jira instance.
type Status struct {
	Self           string         `json:"self" structs:"self"`
	Description    string         `json:"description" structs:"description"`
	IconURL        string         `json:"iconUrl" structs:"iconUrl"`
	Name           string         `json:"name" structs:"name"`
	ID             string         `json:"id" structs:"id"`
	StatusCategory StatusCategory `json:"statusCategory" structs:"statusCategory"`
	Scope          Scope          `json:"scope" structs:"scope"`
}

type JiraStatus struct {
	Description    string           `json:"description"`
	Id             string           `json:"id"`
	Name           string           `json:"name"`
	StatusCategory string           `json:"statusCategory"`
	Scope          *Scope           `json:"scope"`
	Usages         []*Usage         `json:"usages"`
	WorkflowUsages []*WorkflowUsage `json:"workflowUsages"`
}

type Scope struct {
	Project *ProjectId `json:"project"`
	Type    string     `json:"type"`
}

type Usage struct {
	IssueTypes []string   `json:"issueTypes"`
	Project    *ProjectId `json:"project"`
}

type ProjectId struct {
	Id string `json:"id"`
}

type WorkflowUsage struct {
	WorkflowId   string `json:"workflowId"`
	WorkflowName string `json:"workflowName"`
}

type searchStatusResponse struct {
	MaxResults int          `json:"maxResults,omitempty" structs:"maxResults,omitempty"`
	Self       string       `json:"self,omitempty" structs:"self,omitempty"`
	NextPage   string       `json:"nextPage,omitempty" structs:"nextPage,omitempty"`
	StartAt    int          `json:"startAt,omitempty" structs:"startAt,omitempty"`
	Total      int          `json:"total,omitempty" structs:"total,omitempty"`
	IsLast     bool         `json:"isLast,omitempty" structs:"isLast,omitempty"`
	Values     []JiraStatus `json:"values,omitempty" structs:"values,omitempty"`
}

// GetAllStatuses returns a list of all statuses associated with workflows.
//
// Jira API docs: https://developer.atlassian.com/cloud/jira/platform/rest/v2/#api-rest-api-2-status-get
//
// TODO Double check this method if this works as expected, is using the latest API and the response is complete
// This double check effort is done for v2 - Remove this two lines if this is completed.
func (s *StatusService) GetAllStatuses(ctx context.Context) ([]Status, *Response, error) {
	apiEndpoint := "rest/api/2/status"
	req, err := s.client.NewRequest(ctx, http.MethodGet, apiEndpoint, nil)

	if err != nil {
		return nil, nil, err
	}

	statusList := []Status{}
	resp, err := s.client.Do(req, &statusList)
	if err != nil {
		return nil, resp, NewJiraError(resp, err)
	}

	return statusList, resp, nil
}

func (s *StatusService) SearchStatusesPaginated(ctx context.Context, tweaks ...searchF) ([]JiraStatus, *Response, error) {
	apiEndpoint := "rest/api/3/statuses/search"

	search := []searchParam{}
	for _, f := range tweaks {
		search = f(search)
	}

	queryString := ""
	for _, param := range search {
		queryString += fmt.Sprintf("%s=%s&", param.name, param.value)
	}

	if queryString != "" {
		apiEndpoint += "?" + queryString
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, apiEndpoint, nil)
	if err != nil {
		return nil, nil, err
	}

	response := new(searchStatusResponse)
	resp, err := s.client.Do(req, response)
	if err != nil {
		return nil, resp, NewJiraError(resp, err)
	}

	return response.Values, resp, nil
}
