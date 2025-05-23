package cloud

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type AuditService service

type AuditTime struct {
	time.Time
}

func (auditTime *AuditTime) UnmarshalJSON(jsonBytes []byte) error {
	jsonStr := string(jsonBytes)
	if jsonStr == "null" {
		return nil
	}

	timestampStr := jsonStr[1 : len(jsonStr)-1]
	const jiraTimestampLayout = "2006-01-02T15:04:05.000-0700"
	parsedTime, err := time.Parse(jiraTimestampLayout, timestampStr)
	if err != nil {
		return fmt.Errorf("AuditTime unmarshal error: %w", err)
	}

	auditTime.Time = parsedTime
	return nil
}

type AuditRecord struct {
	ID              int64               `json:"id"`
	Summary         string              `json:"summary"`
	Created         AuditTime           `json:"created"`
	Category        string              `json:"category"`
	EventSource     string              `json:"eventSource"`
	ObjectItem      AuditObjectItem     `json:"objectItem"`
	ChangedValues   []AuditChangedValue `json:"changedValues"`
	AssociatedItems []AuditObjectItem   `json:"associatedItems"`
	RemoteAddress   string              `json:"remoteAddress"`
	AuthorKey       string              `json:"authorKey"`
	AuthorAccountId string              `json:"authorAccountId"`
	Description     string              `json:"description"`
}

type AuditObjectItem struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	TypeName   string `json:"typeName"`
	ParentId   string `json:"parentId"`
	ParentName string `json:"parentName"`
}

type AuditChangedValue struct {
	FieldName   string `json:"fieldName"`
	ChangedFrom string `json:"changedFrom"`
	ChangedTo   string `json:"changedTo"`
}

type AuditResponse struct {
	Offset  int64         `json:"offset"`
	Limit   int64         `json:"limit"`
	Total   int64         `json:"total"`
	Records []AuditRecord `json:"records"`
}

type AuditOptions struct {
	From   string `url:"from,omitempty"`
	Offset int    `url:"offset,omitempty"`
	Limit  int    `url:"limit,omitempty"`
}

func (s *AuditService) Get(ctx context.Context, from time.Time, offset int, limit int) (*AuditResponse, *Response, error) {
	apiEndpoint := "/rest/api/3/auditing/record"

	options := &AuditOptions{}
	if !from.IsZero() {
		options.From = from.Format(time.RFC3339)
	}
	if offset > 0 {
		options.Offset = offset
	}
	if limit > 0 {
		options.Limit = limit
	}

	urlWithParams, err := addOptions(apiEndpoint, options)
	if err != nil {
		return nil, nil, err
	}

	request, err := s.client.NewRequest(ctx, http.MethodGet, urlWithParams, nil)
	if err != nil {
		return nil, nil, err
	}

	audit := new(AuditResponse)
	response, err := s.client.Do(request, audit)
	if err != nil {
		return nil, response, NewJiraError(response, err)
	}

	return audit, response, nil
}
