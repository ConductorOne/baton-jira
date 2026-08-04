package connector

import (
	"context"
	"fmt"

	"github.com/conductorone/baton-jira/pkg/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	grant "github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	jira "github.com/conductorone/go-jira/v2/cloud"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

const (
	atlassianUserRoleActor  = "atlassian-user-role-actor"
	atlassianGroupRoleActor = "atlassian-group-role-actor"
)

var resourceTypeProject = &v2.ResourceType{
	Id:          "project",
	DisplayName: "Project",
}

type projectResourceType struct {
	resourceType            *v2.ResourceType
	client                  *client.Client
	skipProjectParticipants bool
}

func projectResource(_ context.Context, project *jira.Project) (*v2.Resource, error) {
	resource, err := rs.NewResource(project.Name, resourceTypeProject, project.ID)
	if err != nil {
		return nil, err
	}

	return resource, nil
}

func (g *projectResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return g.resourceType
}

func projectBuilder(c *client.Client, skipProjectParticipants bool) *projectResourceType {
	return &projectResourceType{
		resourceType:            resourceTypeProject,
		client:                  c,
		skipProjectParticipants: skipProjectParticipants,
	}
}

func (u *projectResourceType) Entitlements(ctx context.Context, resource *v2.Resource, attrs rs.SyncOpAttrs) ([]*v2.Entitlement, *rs.SyncOpResults, error) {
	var rv []*v2.Entitlement

	var assigmentOptions []ent.EntitlementOption

	if !u.skipProjectParticipants {
		assigmentOptions = []ent.EntitlementOption{
			ent.WithGrantableTo(resourceTypeUser),
			ent.WithDescription(fmt.Sprintf("Participating on %s project", resource.DisplayName)),
			ent.WithDisplayName(fmt.Sprintf("%s project %s", resource.DisplayName, participateEntitlement)),
		}
		rv = append(rv, ent.NewAssignmentEntitlement(resource, participateEntitlement, assigmentOptions...))
	}

	assigmentOptions = []ent.EntitlementOption{
		ent.WithGrantableTo(resourceTypeUser),
		ent.WithDescription(fmt.Sprintf("Leading %s project", resource.DisplayName)),
		ent.WithDisplayName(fmt.Sprintf("%s project %s", resource.DisplayName, leadEntitlement)),
	}
	rv = append(rv, ent.NewAssignmentEntitlement(resource, leadEntitlement, assigmentOptions...))

	return rv, nil, nil
}

func (p *projectResourceType) Grants(ctx context.Context, resource *v2.Resource, attrs rs.SyncOpAttrs) ([]*v2.Grant, *rs.SyncOpResults, error) {
	project, err := p.client.GetProject(ctx, attrs.Session, resource.Id.Resource)
	if err != nil {
		return nil, nil, err
	}

	var rv []*v2.Grant

	bag, offset, err := parsePageToken(attrs.PageToken.Token, &v2.ResourceId{ResourceType: resourceTypeProject.Id})
	if err != nil {
		return nil, nil, err
	}

	if offset == 0 {
		// handle grants without pagination
		leadGrants, err := p.getLeadGrants(ctx, resource, project)
		if err != nil {
			return nil, nil, wrapError(err, "failed to get lead grants", nil)
		}
		rv = append(rv, leadGrants...)
	}

	if p.skipProjectParticipants {
		return rv, nil, nil
	}

	participateGrants, usersFetched, err := p.getGrantsForProjectUsers(ctx, resource, project, int(offset), participantPageSize)
	if err != nil {
		return nil, nil, err
	}
	rv = append(rv, participateGrants...)

	ctxzap.Extract(ctx).Debug("fetched project participants page",
		zap.String("project_key", project.Key),
		zap.Int64("start_at", offset),
		zap.Int("max_results_requested", participantPageSize),
		zap.Int("users_returned", usersFetched),
	)

	// Advance by the requested window size and terminate only on an empty
	// page, so a short-but-nonempty page doesn't end pagination prematurely;
	// the trailing empty page confirms the end of the list at the cost of one
	// extra request per project.
	if usersFetched == 0 {
		return rv, nil, nil
	}

	nextPage, err := getPageTokenFromOffset(bag, offset+int64(participantPageSize))
	if err != nil {
		return nil, nil, err
	}

	return rv, &rs.SyncOpResults{NextPageToken: nextPage}, nil
}

func (p *projectResourceType) getLeadGrants(ctx context.Context, resource *v2.Resource, project *jira.Project) ([]*v2.Grant, error) {
	var rv []*v2.Grant
	if project.Lead.AccountID != "" {
		lead := project.Lead
		leadResource, err := userResource(ctx, &jira.User{
			Name:         lead.Name,
			Key:          lead.Key,
			AccountID:    lead.AccountID,
			EmailAddress: lead.EmailAddress,
			DisplayName:  lead.DisplayName,
			Active:       lead.Active,
			TimeZone:     lead.TimeZone,
			AccountType:  lead.AccountType,
		})
		if err != nil {
			return nil, err
		}

		grant := grant.NewGrant(resource, leadEntitlement, leadResource.Id)
		rv = append(rv, grant)
	}

	return rv, nil
}

func (p *projectResourceType) getGrantsForProjectUsers(ctx context.Context, resource *v2.Resource, project *jira.Project, offset int, count int) ([]*v2.Grant, int, error) {
	var rv []*v2.Grant

	users, resp, err := p.client.Jira().User.FindUsersWithBrowsePermission(ctx, ".", jira.WithStartAt(offset), jira.WithMaxResults(count), jira.WithProjectKey(project.Key))
	if err != nil {
		var statusCode *int
		if resp != nil {
			statusCode = &resp.StatusCode
		}
		return nil, 0, wrapError(err, "failed to get participate grants", statusCode)
	}

	for i := range users {
		userResource, err := userResource(ctx, &users[i])
		if err != nil {
			return nil, 0, err
		}

		grant := grant.NewGrant(resource, participateEntitlement, userResource.Id)
		rv = append(rv, grant)
	}

	return rv, len(users), nil
}

func (u *projectResourceType) List(ctx context.Context, _ *v2.ResourceId, attrs rs.SyncOpAttrs) ([]*v2.Resource, *rs.SyncOpResults, error) {
	bag, offset, err := parsePageToken(attrs.PageToken.Token, &v2.ResourceId{ResourceType: resourceTypeProject.Id})
	if err != nil {
		return nil, nil, err
	}

	projects, resp, err := u.client.Jira().Project.Find(ctx, jira.WithStartAt(int(offset)), jira.WithMaxResults(resourcePageSize))
	if err != nil {
		var statusCode *int
		if resp != nil {
			statusCode = &resp.StatusCode
		}
		return nil, nil, wrapError(err, "failed to get projects", statusCode)
	}

	var resources []*v2.Resource
	for _, project := range projects {
		resource, err := projectResource(ctx, &jira.Project{
			Name: project.Name,
			ID:   project.ID,
		})

		if err != nil {
			return nil, nil, err
		}

		resources = append(resources, resource)
	}

	if isLastPage(len(projects), resourcePageSize) {
		return resources, nil, nil
	}

	nextPage, err := getPageTokenFromOffset(bag, offset+int64(resourcePageSize))
	if err != nil {
		return nil, nil, err
	}

	return resources, &rs.SyncOpResults{NextPageToken: nextPage}, nil
}
