package connector

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"time"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	sdkTicket "github.com/conductorone/baton-sdk/pkg/types/ticket"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/timestamppb"

	jira "github.com/conductorone/go-jira/v2/cloud"
)

type TicketManager interface {
	GetTicket(ctx context.Context, ticketId string) (*v2.Ticket, annotations.Annotations, error)
	CreateTicket(ctx context.Context, ticket *v2.Ticket, schema *v2.TicketSchema) (*v2.Ticket, annotations.Annotations, error)
	GetTicketSchema(ctx context.Context, schemaID string) (*v2.TicketSchema, annotations.Annotations, error)
	ListTicketSchemas(ctx context.Context, pToken *pagination.Token) ([]*v2.TicketSchema, string, annotations.Annotations, error)
}

func getJiraStatusesForProject(ctx context.Context, client *jira.Client, projectId string) ([]jira.JiraStatus, error) {
	var jiraStatuses []jira.JiraStatus
	statusOffset := 0
	statusMaxResults := 100

	for {
		// Fetch statuses here and pass in to schemaForProject
		statuses, resp, err := client.Status.SearchStatusesPaginated(ctx,
			jira.WithStartAt(statusOffset),
			jira.WithMaxResults(statusOffset+statusMaxResults),
			jira.WithStatusCategory("DONE"),
			jira.WithProjectId(projectId))
		if err != nil {
			return nil, err
		}
		jiraStatuses = append(jiraStatuses, statuses...)
		if resp.Total == 0 {
			break
		}
		statusOffset += resp.Total
	}

	return jiraStatuses, nil
}

func (j *Jira) ListTicketSchemas(ctx context.Context, p *pagination.Token) ([]*v2.TicketSchema, string, annotations.Annotations, error) {
	var ret []*v2.TicketSchema

	offset := 0
	// get offset from page token if its not empty
	if p != nil && p.Token != "" {
		var err error
		offset, err = strconv.Atoi(p.Token)
		if err != nil {
			return nil, "", nil, err
		}
	}

	projects, _, err := j.client.Project.Find(ctx, jira.WithStartAt(offset), jira.WithMaxResults(p.Size), jira.WithExpand("issueTypes"))
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get projects")
	}

	for _, project := range projects {
		schema, err := j.schemaForProject(ctx, project)
		if err != nil {
			return nil, "", nil, err
		}
		ret = append(ret, schema)
	}

	nextPageToken := ""
	if len(ret) > 1 {
		nextPageToken = fmt.Sprintf("%d", offset+len(ret))
	}

	return ret, nextPageToken, nil, nil
}

func (j *Jira) getTicketStatuses(ctx context.Context, projectId string, statuses []jira.JiraStatus) ([]*v2.TicketStatus, error) {
	// filter statuses by project id
	var filteredStatuses []jira.JiraStatus
	for _, status := range statuses {
		if status.Scope != nil && status.Scope.Project == nil && status.Scope.Project.Id == projectId {
			filteredStatuses = append(filteredStatuses, status)
		}
	}

	ret := make([]*v2.TicketStatus, 0, len(filteredStatuses))
	for _, status := range filteredStatuses {
		ret = append(ret, &v2.TicketStatus{
			Id:          status.Id,
			DisplayName: status.Name,
		})
	}

	return ret, nil
}

func (j *Jira) schemaForProject(ctx context.Context, project jira.Project) (*v2.TicketSchema, error) {
	var ticketTypes []*v2.TicketType
	customFields := make(map[string]*v2.TicketCustomField)

	var components []*v2.TicketCustomFieldObjectValue

	for _, issueType := range project.IssueTypes {
		// TODO: Maybe we care about subtasks?
		if !issueType.Subtask {
			ticketTypes = append(ticketTypes, &v2.TicketType{
				Id:          issueType.ID,
				DisplayName: issueType.Name,
			})
		}
	}
	for _, component := range project.Components {
		components = append(components, &v2.TicketCustomFieldObjectValue{
			Id:          component.ID,
			DisplayName: component.Name,
		})
	}

	// Add a required field for the project
	customFields["project"] = sdkTicket.PickObjectValueFieldSchema(
		"project",
		"Project",
		true,
		[]*v2.TicketCustomFieldObjectValue{
			{
				Id:          project.ID,
				DisplayName: project.Name,
			},
		},
	)

	if len(components) > 0 {
		customFields["components"] = sdkTicket.PickMultipleObjectValuesFieldSchema(
			"components",
			"Components",
			false,
			components,
		)
	}

	ret := &v2.TicketSchema{
		Id:           project.Key,
		DisplayName:  project.Name,
		Types:        ticketTypes,
		CustomFields: customFields,
	}

	jiraStatuses, err := getJiraStatusesForProject(ctx, j.client, project.ID)
	if err != nil {
		return nil, err
	}

	// iterate through statues, if global or done or projectId
	statuses, err := j.getTicketStatuses(ctx, project.ID, jiraStatuses)
	if err != nil {
		return nil, err
	}
	ret.Statuses = statuses

	j.ticketSchemas[project.Key] = ret

	return ret, nil
}

func (j *Jira) GetTicketSchema(ctx context.Context, schemaID string) (*v2.TicketSchema, annotations.Annotations, error) {
	if schema, ok := j.ticketSchemas[schemaID]; ok {
		return schema, nil, nil
	}

	project, _, err := j.client.Project.Get(ctx, schemaID)
	if err != nil {
		return nil, nil, err
	}

	ret, err := j.schemaForProject(ctx, *project)
	if err != nil {
		return nil, nil, err
	}

	return ret, nil, nil
}

func (j *Jira) issueToTicket(ctx context.Context, issue *jira.Issue) (*v2.Ticket, error) {
	if issue.Fields == nil {
		return nil, errors.New("issue has no fields")
	}
	schema, _, err := j.GetTicketSchema(ctx, issue.Fields.Project.ID)
	if err != nil {
		return nil, err
	}

	issueURL, err := j.generateIssueURL(issue.Key)
	if err != nil {
		return nil, err
	}

	ret := &v2.Ticket{
		Id:          issue.ID,
		DisplayName: issue.Fields.Summary,
		Description: issue.Fields.Description,
		Type: &v2.TicketType{
			Id:          issue.Fields.Type.ID,
			DisplayName: issue.Fields.Type.Name,
		},
		Status: &v2.TicketStatus{
			Id:          issue.Fields.Status.ID,
			DisplayName: issue.Fields.Status.Name,
		},
		Labels:    issue.Fields.Labels,
		CreatedAt: timestamppb.New(time.Time(issue.Fields.Created)),
		UpdatedAt: timestamppb.New(time.Time(issue.Fields.Updated)),
		Url:       issueURL,
	}

	if issue.Fields.Assignee != nil {
		if assignee, err := userResource(ctx, issue.Fields.Assignee); err == nil && assignee != nil {
			ret.Assignees = []*v2.Resource{assignee}
		}
	}

	if issue.Fields.Reporter != nil {
		if reporter, err := userResource(ctx, issue.Fields.Reporter); err == nil && reporter != nil {
			ret.Reporter = reporter
		}
	}

	retCustomFields := make(map[string]*v2.TicketCustomField)
	for id, cf := range schema.GetCustomFields() {
		switch id {
		case "project":
			retCustomFields[id] = sdkTicket.PickObjectValueField(cf.GetId(), &v2.TicketCustomFieldObjectValue{
				Id:          issue.Fields.Project.ID,
				DisplayName: issue.Fields.Project.Name,
			})
		case "components":
			var components []*v2.TicketCustomFieldObjectValue
			for _, component := range issue.Fields.Components {
				components = append(components, &v2.TicketCustomFieldObjectValue{
					Id:          component.ID,
					DisplayName: component.Name,
				})
			}
			retCustomFields[id] = sdkTicket.PickMultipleObjectValuesField(cf.GetId(), components)
		}
	}
	ret.CustomFields = retCustomFields

	return ret, nil
}

func (j *Jira) GetTicket(ctx context.Context, ticketId string) (*v2.Ticket, annotations.Annotations, error) {
	issue, _, err := j.client.Issue.Get(ctx, ticketId, nil)
	if err != nil {
		return nil, nil, err
	}

	if issue == nil {
		return nil, nil, errors.New("issue not found")
	}

	ret, err := j.issueToTicket(ctx, issue)
	if err != nil {
		return nil, nil, err
	}

	return ret, nil, nil
}

// This is returning nil for annotations.
func (j *Jira) CreateTicket(ctx context.Context, ticket *v2.Ticket, schema *v2.TicketSchema) (*v2.Ticket, annotations.Annotations, error) {
	ticketOptions := []FieldOption{
		// WithStatus(ticket.GetStatus().GetId()),
		WithType(ticket.GetType().GetId()),
		WithDescription(ticket.GetDescription()),
		WithLabels(ticket.GetLabels()...),
	}

	ticketFields := ticket.GetCustomFields()

	var projectID string

	for id, cf := range schema.GetCustomFields() {
		switch id {
		case "project":
			project, err := sdkTicket.GetPickObjectValue(ticketFields[id])
			if err != nil {
				return nil, nil, err
			}

			if project.GetId() == "" {
				return nil, nil, errors.New("error: unable to create ticket, project is required")
			}

			projectID = project.GetId()

		case "components":
			comps, err := sdkTicket.GetPickMultipleObjectValues(ticketFields[id])
			if err != nil {
				if errors.Is(err, sdkTicket.ErrFieldNil) {
					continue
				}
				return nil, nil, err
			}

			componentIDs := make([]string, 0, len(comps))
			for _, component := range comps {
				componentIDs = append(componentIDs, component.GetId())
			}
			ticketOptions = append(ticketOptions, WithComponents(componentIDs...))

		default:
			val, err := sdkTicket.GetCustomFieldValue(ticketFields[id])
			if err != nil {
				return nil, nil, err
			}

			// The ticket doesn't have this key set, so we skip it
			if val == nil {
				continue
			}

			ticketOptions = append(ticketOptions, WithCustomField(cf.GetId(), val))
		}
	}

	valid, err := sdkTicket.ValidateTicket(ctx, schema, ticket)
	if err != nil {
		return nil, nil, err
	}
	if !valid {
		return nil, nil, errors.New("error: unable to create ticket, ticket is invalid")
	}

	iss, err := j.createIssue(ctx, projectID, ticket.GetDisplayName(), ticketOptions...)
	if err != nil {
		return nil, nil, err
	}

	fullIss, _, err := j.client.Issue.Get(ctx, iss.ID, nil)
	if err != nil {
		return nil, nil, err
	}

	ret, err := j.issueToTicket(ctx, fullIss)
	if err != nil {
		return nil, nil, err
	}

	return ret, nil, nil
}

type FieldOption func(issue *jira.Issue)

func WithStatus(statusId string) FieldOption {
	return func(issue *jira.Issue) {
		if statusId != "" {
			issue.Fields.Status = &jira.Status{
				ID: statusId,
			}
		}
	}
}

func WithDescription(description string) FieldOption {
	return func(issue *jira.Issue) {
		issue.Fields.Description = description
	}
}

func WithType(id string) FieldOption {
	return func(issue *jira.Issue) {
		issue.Fields.Type = jira.IssueType{
			ID: id,
		}
	}
}

func WithLabels(labels ...string) FieldOption {
	return func(issue *jira.Issue) {
		issue.Fields.Labels = labels
	}
}

func WithCustomField(id string, value interface{}) FieldOption {
	return func(issue *jira.Issue) {
		if issue.Fields.Unknowns == nil {
			issue.Fields.Unknowns = make(map[string]interface{})
		}
		issue.Fields.Unknowns[id] = value
	}
}

func WithComponents(componentIDs ...string) FieldOption {
	return func(issue *jira.Issue) {
		if len(issue.Fields.Components) == 0 {
			issue.Fields.Components = make([]*jira.Component, 0)
		}
		for _, componentID := range componentIDs {
			issue.Fields.Components = append(issue.Fields.Components, &jira.Component{ID: componentID})
		}
	}
}

func (j *Jira) createIssue(ctx context.Context, projectID string, summary string, opts ...FieldOption) (*jira.Issue, error) {
	l := ctxzap.Extract(ctx)

	i := &jira.Issue{
		Fields: &jira.IssueFields{
			Summary: summary,
			Project: jira.Project{
				ID: projectID,
			},
		},
	}

	for _, opt := range opts {
		opt(i)
	}

	// Default to 'Task' if no type is provided
	if i.Fields.Type.ID == "" {
		i.Fields.Type = jira.IssueType{
			Name: "Task",
		}
	}
	issue, _, err := j.client.Issue.Create(ctx, i)
	if err != nil {
		l.Error("error creating issue", zap.Error(err))
		return nil, err
	}

	return issue, nil
}

func (j *Jira) generateIssueURL(issueKey string) (string, error) {
	baseURL, err := url.Parse(j.client.BaseURL.String())
	if err != nil {
		return "", err
	}
	baseURL.Path = path.Join("browse", issueKey)
	return baseURL.String(), nil
}
