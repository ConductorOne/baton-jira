package connector

import (
	"context"
	"fmt"

	jira "github.com/andygrunwald/go-jira/v2/cloud"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	grant "github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
)

var resourceTypeProject = &v2.ResourceType{
	Id:          "project",
	DisplayName: "Project",
	Traits: []v2.ResourceType_Trait{
		v2.ResourceType_TRAIT_GROUP,
	},
}

type projectResourceType struct {
	resourceType *v2.ResourceType
	client       *jira.Client
}

func projectResource(ctx context.Context, project *jira.Project) (*v2.Resource, error) {
	profile := map[string]interface{}{
		"name":       project.Name,
		"project_id": project.ID,
		"category":   project.ProjectCategory.Name,
	}

	projectTraitOptions := []rs.GroupTraitOption{
		rs.WithGroupProfile(profile),
	}

	resource, err := rs.NewGroupResource(project.Name, resourceTypeProject, project.ID, projectTraitOptions)
	if err != nil {
		return nil, err
	}

	return resource, nil
}

func (g *projectResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return g.resourceType
}

func projectBuilder(client *jira.Client) *projectResourceType {
	return &projectResourceType{
		resourceType: resourceTypeProject,
		client:       client,
	}
}

func (u *projectResourceType) Entitlements(ctx context.Context, resource *v2.Resource, token *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	var rv []*v2.Entitlement

	assigmentOptions := []ent.EntitlementOption{
		ent.WithGrantableTo(resourceTypeUser),
		ent.WithDescription(fmt.Sprintf("Participating on %s project", resource.DisplayName)),
		ent.WithDisplayName(fmt.Sprintf("%s project %s", resource.DisplayName, participateEntitlement)),
	}
	rv = append(rv, ent.NewAssignmentEntitlement(resource, participateEntitlement, assigmentOptions...))

	assigmentOptions = []ent.EntitlementOption{
		ent.WithGrantableTo(resourceTypeUser),
		ent.WithDescription(fmt.Sprintf("Leading %s project", resource.DisplayName)),
		ent.WithDisplayName(fmt.Sprintf("%s project %s", resource.DisplayName, leadEntitlement)),
	}
	rv = append(rv, ent.NewAssignmentEntitlement(resource, leadEntitlement, assigmentOptions...))

	return rv, "", nil, nil
}

func (p *projectResourceType) Grants(ctx context.Context, resource *v2.Resource, token *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	project, _, err := p.client.Project.Get(ctx, resource.Id.Resource)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get project")
	}

	var rv []*v2.Grant

	leadGrants, err := getLeadGrants(ctx, resource, project)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get lead grants")
	}
	rv = append(rv, leadGrants...)

	participateGrants, err := getGrantsForAllUsersIfProjectIsPublic(ctx, p, resource, project)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get participate grants")
	}
	rv = append(rv, participateGrants...)

	return rv, "", nil, nil
}

func getLeadGrants(ctx context.Context, resource *v2.Resource, project *jira.Project) ([]*v2.Grant, error) {
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

func getGrantsForAllUsersIfProjectIsPublic(ctx context.Context, p *projectResourceType, resource *v2.Resource, project *jira.Project) ([]*v2.Grant, error) {
	var rv []*v2.Grant

	if !project.IsPrivate {
		users, _, err := p.client.User.Find(ctx, "")
		if err != nil {
			return nil, wrapError(err, "failed to list users")
		}

		for _, user := range users {
			userResource, err := userResource(ctx, &user)
			if err != nil {
				return nil, err
			}

			grant := grant.NewGrant(resource, participateEntitlement, userResource.Id)
			rv = append(rv, grant)
		}
	}

	return rv, nil
}

func (u *projectResourceType) List(ctx context.Context, _ *v2.ResourceId, _ *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	projects, _, err := u.client.Project.GetAll(ctx, nil)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get projects")
	}

	var resources []*v2.Resource
	for _, project := range *projects {

		resource, err := projectResource(ctx, &jira.Project{
			Name:            project.Name,
			ID:              project.ID,
			ProjectCategory: project.ProjectCategory,
		})

		if err != nil {
			return nil, "", nil, err
		}

		resources = append(resources, resource)
	}

	return resources, "", nil, nil
}
