package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	pbjira "github.com/conductorone/baton-jira/pb/c1/connector/v2"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	sdkTicket "github.com/conductorone/baton-sdk/pkg/types/ticket"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	jira "github.com/conductorone/go-jira/v2/cloud"
)

type TicketManager interface {
	GetTicket(ctx context.Context, ticketId string) (*v2.Ticket, annotations.Annotations, error)
	CreateTicket(ctx context.Context, ticket *v2.Ticket, schema *v2.TicketSchema) (*v2.Ticket, annotations.Annotations, error)
	GetTicketSchema(ctx context.Context, schemaID string) (*v2.TicketSchema, annotations.Annotations, error)
	ListTicketSchemas(ctx context.Context, pToken *pagination.Token) ([]*v2.TicketSchema, string, annotations.Annotations, error)
}

// Format is projectKey:issueID.
type ProjectKeyIssueTypeIDSchemaID struct {
	ProjectKey  string
	IssueTypeID string
}

func (p ProjectKeyIssueTypeIDSchemaID) String() string {
	return fmt.Sprintf("%s:%s", p.ProjectKey, p.IssueTypeID)
}

func (p *ProjectKeyIssueTypeIDSchemaID) Parse(schemaID string) error {
	schemaIDParts := strings.Split(schemaID, ":")
	if len(schemaIDParts) != 2 {
		return errors.New("invalid schemaID format, expected 'projectKey:issueTypeID'")
	}
	p.ProjectKey = schemaIDParts[0]
	p.IssueTypeID = schemaIDParts[1]
	return nil
}

type JiraName struct {
	Name string `json:"name,omitempty"`
}

// example https://developer.atlassian.com/server/jira/platform/jira-rest-api-example-create-issue-7897248/
func (j *Jira) customFieldSchemaToMetaField(field *v2.TicketCustomField) (interface{}, error) {
	if field == nil {
		return nil, nil
	}

	type JiraPickerStruct struct {
		Id string `json:"id"`
	}
	switch v := field.GetValue().(type) {
	case *v2.TicketCustomField_StringValue:
		strValue := v.StringValue.GetValue()
		if len(strValue) == 0 {
			return nil, nil
		}

		typ := GeCustomFieldTypeAnnotation(field.Annotations)

		switch typ {
		case jira.TypeUser:
			return jira.User{
				AccountID: strValue,
			}, nil
		case jira.TypeGroup:
			return JiraName{
				Name: strValue,
			}, nil
		case jira.TypeNumber:
			v, err := strconv.Atoi(strValue)
			if err != nil {
				return nil, err
			}
			return v, nil
		}
		return strValue, nil

	case *v2.TicketCustomField_StringValues:
		return v.StringValues.GetValues(), nil
	case *v2.TicketCustomField_BoolValue:
		return v.BoolValue.GetValue(), nil
	case *v2.TicketCustomField_TimestampValue:
		// must be in ISO 8601 date time format (RFC3339)
		// https://support.atlassian.com/cloud-automation/docs/advanced-field-editing-using-json/
		// -> Date time picker custom field
		return v.TimestampValue.GetValue().AsTime().Format(time.RFC3339), nil
	case *v2.TicketCustomField_PickStringValue:
		return v.PickStringValue.GetValue(), nil
	case *v2.TicketCustomField_PickMultipleStringValues:
		return v.PickMultipleStringValues.GetValues(), nil
	case *v2.TicketCustomField_PickObjectValue:
		if v.PickObjectValue.GetValue() != nil {
			return &JiraPickerStruct{
				Id: v.PickObjectValue.GetValue().GetId(),
			}, nil
		}
	case *v2.TicketCustomField_PickMultipleObjectValues:
		pickObjects := []*JiraPickerStruct{}
		for _, value := range v.PickMultipleObjectValues.GetValues() {
			pickObjects = append(pickObjects, &JiraPickerStruct{Id: value.GetId()})
		}
		return pickObjects, nil
	default:
		return false, errors.New("error: unknown custom field type")
	}

	return nil, nil
}

func (j *Jira) getJiraStatusesForProject(ctx context.Context, projectId string) ([]jira.JiraStatus, error) {
	var jiraStatuses []jira.JiraStatus
	statusOffset := 0
	statusMaxResults := 100

	for {
		// Fetch statuses here and pass in to schemaForProject
		statuses, resp, err := j.client.Status.SearchStatusesPaginated(ctx,
			jira.WithStartAt(statusOffset),
			jira.WithMaxResults(statusMaxResults),
			jira.WithStatusCategory("DONE"),
			jira.WithProjectId(projectId))
		if err != nil {
			return nil, err
		}

		jiraStatuses = append(jiraStatuses, statuses...)

		statusOffset += resp.MaxResults
		if statusOffset >= resp.Total {
			break
		}
	}

	return jiraStatuses, nil
}

func (j *Jira) constructMetaDataFields(issues []*jira.MetaIssueType) (map[string]jira.MetaDataFields, error) {
	fieldsMap := make(map[string]jira.MetaDataFields)

	for _, issueType := range issues {
		for key, field := range issueType.Fields {
			var metaDataField jira.MetaDataFields

			jsonData, err := json.Marshal(field)
			if err != nil {
				return nil, err
			}

			err = json.Unmarshal(jsonData, &metaDataField)
			if err != nil {
				return nil, err
			}

			fieldsMap[key] = metaDataField
		}
	}

	return fieldsMap, nil
}

func (j *Jira) schemaForProjectIssueType(ctx context.Context, project *jira.Project, issueType *jira.IssueType, statuses []*v2.TicketStatus, includeProjectInName bool) (*v2.TicketSchema, error) {
	customFieldsMap := make(map[string]*v2.TicketCustomField)

	issueTypeCustomFields, err := j.getCustomFieldsForIssueType(ctx, project.ID, issueType)
	if err != nil {
		return nil, err
	}

	for _, cf := range issueTypeCustomFields {
		customFieldsMap[cf.GetId()] = cf
	}

	projectKeySchemaID := &ProjectKeyIssueTypeIDSchemaID{
		ProjectKey:  project.Key,
		IssueTypeID: issueType.ID,
	}
	schemaId := projectKeySchemaID.String()

	displayName := issueType.Name

	if includeProjectInName {
		displayName = fmt.Sprintf("%s (%s)", displayName, project.Key)
	}

	projectAnno := &pbjira.IssueTypeProject{
		ProjectId:   project.ID,
		ProjectName: project.Name,
		ProjectKey:  project.Key,
	}

	ret := &v2.TicketSchema{
		Id:           schemaId,
		DisplayName:  displayName,
		CustomFields: customFieldsMap,
		Annotations:  annotations.New(projectAnno),
		Statuses:     statuses,
	}

	return ret, nil
}

func (j *Jira) getCustomFieldsForIssueType(ctx context.Context, projectId string, issueType *jira.IssueType) ([]*v2.TicketCustomField, error) {
	customFields := make([]*v2.TicketCustomField, 0)

	issueFields, err := j.GetIssueTypeFields(ctx, projectId, issueType.ID, &jira.GetQueryIssueTypeOptions{
		MaxResults: 100,
		StartAt:    0,
	})
	if err != nil {
		return nil, err
	}

	for _, field := range issueFields {
		// TODO(lauren) remove custom?
		if field.Schema.Custom == "" {
			continue
		}
		customField := convertMetadataFieldToCustomField(field)
		customFields = append(customFields, customField)
	}

	return customFields, nil
}

func (j *Jira) GetIssueTypeFields(ctx context.Context, projectKey, issueTypeId string, opts *jira.GetQueryIssueTypeOptions) ([]*jira.MetaDataFields, error) {
	l := ctxzap.Extract(ctx)

	allMetaFields := make([]*jira.MetaDataFields, 0)

	for {
		issueFields, resp, err := j.client.Issue.GetCreateMetaIssueType(ctx, projectKey, issueTypeId, opts)
		if err != nil {
			l.Error("error getting issue type fields", zap.Error(err))
			return nil, err
		}

		allMetaFields = append(allMetaFields, issueFields...)

		if len(allMetaFields) >= resp.Total || opts == nil {
			break
		}

		opts.StartAt += len(allMetaFields)
	}

	return allMetaFields, nil
}

func convertMetadataFieldToCustomField(metaDataField *jira.MetaDataFields) *v2.TicketCustomField {
	var customField *v2.TicketCustomField
	var allowedValues []*v2.TicketCustomFieldObjectValue

	hasAllowedValues := len(metaDataField.AllowedValues) > 0
	isMultiSelect := metaDataField.Schema.Items != ""

	if hasAllowedValues {
		for _, choice := range metaDataField.AllowedValues {
			displayName := choice.Name
			if displayName == "" {
				displayName = choice.Value
			}
			allowedValues = append(allowedValues, &v2.TicketCustomFieldObjectValue{
				Id:          choice.Id,
				DisplayName: displayName,
			})
		}
	}

	id := metaDataField.Key

	switch metaDataField.Schema.Type {
	case jira.TypeString:
		customField = sdkTicket.StringFieldSchema(id, metaDataField.Name, metaDataField.Required)
	case jira.TypeArray:
		switch {
		case isMultiSelect && hasAllowedValues:
			customField = sdkTicket.PickMultipleObjectValuesFieldSchema(id, metaDataField.Name, metaDataField.Required, allowedValues)
		case isMultiSelect && !hasAllowedValues:
			customField = sdkTicket.StringsFieldSchema(id, metaDataField.Name, metaDataField.Required)
		case !isMultiSelect && hasAllowedValues:
			customField = sdkTicket.PickObjectValueFieldSchema(id, metaDataField.Name, metaDataField.Required, allowedValues)
		default:
			customField = sdkTicket.StringFieldSchema(id, metaDataField.Name, metaDataField.Required)
		}
	case jira.TypeDate, jira.TypeDateTime:
		customField = sdkTicket.TimestampFieldSchema(id, metaDataField.Name, metaDataField.Required)
	case jira.TypeNumber:
		// TODO(lauren) use number field type
		customField = sdkTicket.StringFieldSchema(id, metaDataField.Name, metaDataField.Required)
	case jira.TypeObject, jira.TypeGroup, jira.TypeUser, jira.TypeOption:
		if hasAllowedValues {
			customField = sdkTicket.PickObjectValueFieldSchema(id, metaDataField.Name, metaDataField.Required, allowedValues)
		} else {
			customField = sdkTicket.StringFieldSchema(id, metaDataField.Name, metaDataField.Required)
		}
	default:
		// Default to string, even if its not we this field would still be required to create a ticket
		customField = sdkTicket.StringFieldSchema(id, metaDataField.Name, metaDataField.Required)
	}
	customFieldAnno := &pbjira.CustomField{Type: metaDataField.Schema.Type}
	customField.Annotations = annotations.New(customFieldAnno)
	return customField
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

	projects, resp, err := j.client.Project.Find(ctx, jira.WithStartAt(offset), jira.WithMaxResults(p.Size), jira.WithExpand("issueTypes"))
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get projects")
	}

	multipleProjects := false
	if len(projects) > 1 {
		multipleProjects = true
	}

	for _, project := range projects {
		statuses, err := j.getTicketStatuses(ctx, project.ID)
		if err != nil {
			return nil, "", nil, err
		}
		for _, issueType := range project.IssueTypes {
			if issueType.Name == "Epic" || issueType.Name == "Bug" {
				continue
			}

			if issueType.Subtask {
				continue
			}

			schema, err := j.schemaForProjectIssueType(ctx, &project, &issueType, statuses, multipleProjects)
			if err != nil {
				return nil, "", nil, err
			}
			ret = append(ret, schema)
		}
	}

	nextPageToken := ""
	if offset < resp.Total {
		nextPageToken = fmt.Sprintf("%d", offset+len(ret))
	}

	return ret, nextPageToken, nil, nil
}

func (j *Jira) getTicketStatuses(ctx context.Context, projectID string) ([]*v2.TicketStatus, error) {
	statuses, err := j.getJiraStatusesForProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	ret := make([]*v2.TicketStatus, 0, len(statuses))
	for _, status := range statuses {
		ret = append(ret, &v2.TicketStatus{
			Id:          status.Id,
			DisplayName: status.Name,
		})
	}

	return ret, nil
}

func (j *Jira) GetTicketSchema(ctx context.Context, schemaID string) (*v2.TicketSchema, annotations.Annotations, error) {
	projectKeyIssueTypeID := &ProjectKeyIssueTypeIDSchemaID{}
	err := projectKeyIssueTypeID.Parse(schemaID)
	if err != nil {
		return nil, nil, err
	}

	project, _, err := j.client.Project.Get(ctx, projectKeyIssueTypeID.ProjectKey)
	if err != nil {
		return nil, nil, err
	}

	issueType := findIssueTypeFromProject(project, projectKeyIssueTypeID.IssueTypeID)
	if issueType == nil {
		return nil, nil, errors.New("issueType not found")
	}

	statuses, err := j.getTicketStatuses(ctx, project.ID)
	if err != nil {
		return nil, nil, err
	}

	ret, err := j.schemaForProjectIssueType(ctx, project, issueType, statuses, false)
	if err != nil {
		return nil, nil, err
	}

	return ret, nil, nil
}

func (j *Jira) issueToTicket(ctx context.Context, issue *jira.Issue) (*v2.Ticket, error) {
	if issue.Fields == nil {
		return nil, errors.New("issue has no fields")
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
		WithStatus(ticket.GetStatus().GetId()),
		WithDescription(ticket.GetDescription()),
		WithLabels(ticket.GetLabels()...),
	}

	ticketFields := ticket.GetCustomFields()

	var projectKey string
	var issueTypeID string

	projectAnno := GetProjectAnnotation(schema.Annotations)
	if projectAnno == nil {
		// If no projectAnnotation assume schema id is project
		// Because the config schema may have not been updated
		projectKey = schema.Id
	} else {
		projectKeyIssueTypeID := &ProjectKeyIssueTypeIDSchemaID{}
		err := projectKeyIssueTypeID.Parse(schema.Id)
		if err != nil {
			return nil, nil, err
		}
		projectKey = projectKeyIssueTypeID.ProjectKey
		issueTypeID = projectKeyIssueTypeID.IssueTypeID
		// This could use projectAnno.ProjectKey but the former schemaID is the projectKey so using
		// this for consistency
	}

	for id, cf := range schema.GetCustomFields() {
		switch id {
		case "project":
			continue
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
		case "issue_type":
			// If issueTypeID is empty, the config has not been updated to use issue type as schema
			// So issue type is still stored in the custom fields
			if issueTypeID == "" {
				issueType, err := sdkTicket.GetPickObjectValue(ticketFields[id])
				if err != nil {
					return nil, nil, err
				}
				issueTypeID = issueType.GetId()
			}
		default:
			metaFieldValue, err := j.customFieldSchemaToMetaField(ticketFields[id])
			if err != nil {
				return nil, nil, err
			}

			// The ticket doesn't have this key set, so we skip it
			if metaFieldValue == nil {
				continue
			}

			ticketOptions = append(ticketOptions, WithCustomField(cf.GetId(), metaFieldValue))
		}
	}

	if issueTypeID == "" {
		return nil, nil, errors.New("error: unable to create ticket, issue type is required")
	}

	ticketOptions = append(ticketOptions, WithType(issueTypeID))

	valid, err := sdkTicket.ValidateTicket(ctx, schema, ticket)
	if err != nil {
		return nil, nil, err
	}
	if !valid {
		return nil, nil, errors.Join(errors.New("error: unable to create ticket, ticket is invalid"), sdkTicket.ErrTicketValidationError)
	}

	iss, err := j.createIssue(ctx, projectKey, ticket.GetDisplayName(), ticketOptions...)
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
	for i, label := range labels {
		labels[i] = strings.ReplaceAll(label, " ", "_")
	}
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

func (j *Jira) createIssue(ctx context.Context, projectKey string, summary string, opts ...FieldOption) (*jira.Issue, error) {
	l := ctxzap.Extract(ctx)

	i := &jira.Issue{
		Fields: &jira.IssueFields{
			Summary: summary,
			Project: jira.Project{
				Key: projectKey,
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

	l.Info("creating issue", zap.Any("issue", i))

	issue, resp, err := j.client.Issue.Create(ctx, i)
	if err != nil {
		jerr := jira.NewJiraError(resp, err)
		l.Error("error creating issue", zap.Error(jerr))
		return nil, jerr
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

// We don't error if the annotation is not found
// Because it may not be present for existing configs.
func GeCustomFieldTypeAnnotation(annotations []*anypb.Any) string {
	cf := &pbjira.CustomField{}
	for _, v := range annotations {
		if v.MessageIs(cf) {
			err := v.UnmarshalTo(cf)
			if err != nil {
				return ""
			}
			return cf.GetType()
		}
	}
	return ""
}

func GetProjectAnnotation(annotations []*anypb.Any) *pbjira.IssueTypeProject {
	pta := &pbjira.IssueTypeProject{}
	for _, a := range annotations {
		if a.MessageIs(pta) {
			err := a.UnmarshalTo(pta)
			if err != nil {
				return nil
			}
			return pta
		}
	}
	return nil
}

func findIssueTypeFromProject(project *jira.Project, issueTypeId string) *jira.IssueType {
	for _, issueType := range project.IssueTypes {
		if issueType.ID == issueTypeId {
			return &issueType
		}
	}
	return nil
}
