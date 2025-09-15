package connector

import (
	"context"
	"fmt"

	"github.com/conductorone/baton-jira/pkg/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	grant "github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	jira "github.com/conductorone/go-jira/v2/cloud"
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

func projectResource(ctx context.Context, project *jira.Project) (*v2.Resource, error) {
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

func (u *projectResourceType) Entitlements(ctx context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
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

	return rv, "", nil, nil
}

func (p *projectResourceType) Grants(ctx context.Context, resource *v2.Resource, pt *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	project, resp, err := p.client.GetProject(ctx, resource.Id.Resource)
	if err != nil {
		var statusCode *int
		if resp != nil {
			statusCode = &resp.StatusCode
		}
		return nil, "", nil, wrapError(err, "failed to get project", statusCode)
	}

	var rv []*v2.Grant

	bag, offset, err := parsePageToken(pt.Token, &v2.ResourceId{ResourceType: resourceTypeProject.Id})
	if err != nil {
		return nil, "", nil, err
	}

	if offset == 0 {
		// handle grants without pagination
		leadGrants, err := p.getLeadGrants(ctx, resource, project)
		if err != nil {
			return nil, "", nil, wrapError(err, "failed to get lead grants", nil)
		}
		rv = append(rv, leadGrants...)
	}

	if p.skipProjectParticipants {
		return rv, "", nil, nil
	}

	participateGrants, isLastPage, err := p.getGrantsForProjectUsers(ctx, resource, project, int(offset), resourcePageSize)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get participate grants", nil)
	}
	rv = append(rv, participateGrants...)

	if isLastPage {
		return rv, "", nil, nil
	}

	nextPage, err := getPageTokenFromOffset(bag, offset+int64(resourcePageSize))
	if err != nil {
		return nil, "", nil, err
	}

	return rv, nextPage, nil, nil
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

func (p *projectResourceType) getGrantsForProjectUsers(ctx context.Context, resource *v2.Resource, project *jira.Project, offset int, count int) ([]*v2.Grant, bool, error) {
	var rv []*v2.Grant

	lastPage := true
	users, _, err := p.client.Jira().User.FindUsersWithBrowsePermission(ctx, ".", jira.WithStartAt(offset), jira.WithMaxResults(count), jira.WithProjectKey(project.Key))
	if err != nil {
		return nil, lastPage, err
	}

	for i := range users {
		userResource, err := userResource(ctx, &users[i])
		if err != nil {
			return nil, lastPage, err
		}

		grant := grant.NewGrant(resource, participateEntitlement, userResource.Id)
		rv = append(rv, grant)
	}

	lastPage = isLastPage(len(users), resourcePageSize)

	return rv, lastPage, nil
}

func (u *projectResourceType) List(ctx context.Context, _ *v2.ResourceId, p *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	bag, offset, err := parsePageToken(p.Token, &v2.ResourceId{ResourceType: resourceTypeGroup.Id})
	if err != nil {
		return nil, "", nil, err
	}

	projects, resp, err := u.client.Jira().Project.Find(ctx, jira.WithStartAt(int(offset)), jira.WithMaxResults(resourcePageSize))
	if err != nil {
		var statusCode *int
		if resp != nil {
			statusCode = &resp.StatusCode
		}
		return nil, "", nil, wrapError(err, "failed to get projects", statusCode)
	}

	var resources []*v2.Resource
	for _, project := range projects {
		resource, err := projectResource(ctx, &jira.Project{
			Name: project.Name,
			ID:   project.ID,
		})

		if err != nil {
			return nil, "", nil, err
		}

		resources = append(resources, resource)
	}

	if isLastPage(len(projects), resourcePageSize) {
		return resources, "", nil, nil
	}

	nextPage, err := getPageTokenFromOffset(bag, offset+int64(resourcePageSize))
	if err != nil {
		return nil, "", nil, err
	}

	return resources, nextPage, nil, nil
}
