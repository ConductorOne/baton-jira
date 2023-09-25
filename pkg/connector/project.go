package connector

import (
	"context"
	"fmt"

	jira "github.com/andygrunwald/go-jira/v2/cloud"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
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

	return rv, "", nil, nil
}

func (u *projectResourceType) Grants(ctx context.Context, resource *v2.Resource, token *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	roles, _, err := u.client.Role.GetList(ctx)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get roles")
	}

	var rv []*v2.Grant
	for _, role := range *roles {
		if role.Actors == nil {
			continue
		}

		for _, actor := range role.Actors {
			if actor.ActorGroup != nil {
				groupResource, err := groupResource(ctx, &jira.Group{
					Name: actor.ActorGroup.Name,
				})
				if err != nil {
					return nil, "", nil, err
				}

				grant := grant.NewGrant(resource, participateEntitlement, groupResource.Id)
				rv = append(rv, grant)
			}

			if actor.ActorUser != nil {
				user, _, err := u.client.User.Get(ctx, actor.ActorUser.AccountID)
				if err != nil {
					return nil, "", nil, err
				}
				userResource, err := userResource(ctx, user)
				if err != nil {
					return nil, "", nil, err
				}

				grant := grant.NewGrant(resource, participateEntitlement, userResource.Id)
				rv = append(rv, grant)
			}
		}
	}

	return rv, "", nil, nil
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
