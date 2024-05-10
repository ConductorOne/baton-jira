package connector

import (
	"context"
	"fmt"
	"strconv"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	grant "github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	jira "github.com/conductorone/go-jira/v2/cloud"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

var resourceTypeRole = &v2.ResourceType{
	Id:          "role",
	DisplayName: "Role",
	Traits: []v2.ResourceType_Trait{
		v2.ResourceType_TRAIT_ROLE,
	},
}

type roleResourceType struct {
	resourceType *v2.ResourceType
	client       *jira.Client
}

func roleResource(role *jira.Role) (*v2.Resource, error) {
	profile := map[string]interface{}{
		"name":        role.Name,
		"role_id":     role.ID,
		"description": role.Description,
	}

	roleTraitOptions := []rs.RoleTraitOption{
		rs.WithRoleProfile(profile),
	}

	resource, err := rs.NewRoleResource(role.Name, resourceTypeRole, role.ID, roleTraitOptions)
	if err != nil {
		return nil, err
	}

	return resource, nil
}

func (g *roleResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return g.resourceType
}

func roleBuilder(client *jira.Client) *roleResourceType {
	return &roleResourceType{
		resourceType: resourceTypeRole,
		client:       client,
	}
}

func (u *roleResourceType) Entitlements(ctx context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	var rv []*v2.Entitlement

	assigmentOptions := []ent.EntitlementOption{
		ent.WithGrantableTo(resourceTypeUser),
		ent.WithDescription(fmt.Sprintf("Appointed to %s role", resource.DisplayName)),
		ent.WithDisplayName(fmt.Sprintf("%s role %s", resource.DisplayName, appointedEntitlement)),
	}
	rv = append(rv, ent.NewAssignmentEntitlement(resource, appointedEntitlement, assigmentOptions...))

	assigmentOptions = []ent.EntitlementOption{
		ent.WithGrantableTo(resourceTypeGroup),
		ent.WithDescription(fmt.Sprintf("Members appointed to %s role", resource.DisplayName)),
		ent.WithDisplayName(fmt.Sprintf("%s role %s", resource.DisplayName, appointedEntitlement)),
	}
	rv = append(rv, ent.NewAssignmentEntitlement(resource, appointedEntitlement, assigmentOptions...))

	return rv, "", nil, nil
}

func (u *roleResourceType) Grants(ctx context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	roleId, err := strconv.Atoi(resource.Id.Resource)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to convert role ID to integer")
	}

	role, _, err := u.client.Role.Get(ctx, roleId)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get roles")
	}

	var rv []*v2.Grant
	userGrants, err := getUserGrants(ctx, resource, role)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get user grants")
	}
	rv = append(rv, userGrants...)

	groupGrants, err := getGroupGrants(ctx, resource, role)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get group grants")
	}
	rv = append(rv, groupGrants...)

	return rv, "", nil, nil
}

func getUserGrants(ctx context.Context, resource *v2.Resource, role *jira.Role) ([]*v2.Grant, error) {
	var rv []*v2.Grant

	for _, actor := range role.Actors {
		if actor.ActorUser == nil {
			continue
		}

		user, err := userResource(ctx, &jira.User{
			AccountID: actor.ActorUser.AccountID,
		})
		if err != nil {
			return nil, err
		}

		grant := grant.NewGrant(resource, appointedEntitlement, user.Id)
		rv = append(rv, grant)
	}

	return rv, nil
}

func getGroupGrants(ctx context.Context, resource *v2.Resource, role *jira.Role) ([]*v2.Grant, error) {
	var rv []*v2.Grant

	for _, actor := range role.Actors {
		if actor.ActorGroup == nil {
			continue
		}

		group, err := groupResource(ctx, &jira.Group{
			Name: actor.ActorGroup.Name,
		})
		if err != nil {
			return nil, err
		}

		grant := grant.NewGrant(resource, appointedEntitlement, group.Id)
		rv = append(rv, grant)
	}

	return rv, nil
}

func (u *roleResourceType) mapRoleIDsToProjectNames(ctx context.Context) (map[int]string, error) {
	nextPage := ""
	roleIDToProjectNameMap := make(map[int]string)
	for {
		bag, offset, err := parsePageToken(nextPage, &v2.ResourceId{ResourceType: resourceTypeProject.Id})
		if err != nil {
			return nil, err
		}

		projects, _, err := u.client.Project.Find(ctx, jira.WithStartAt(int(offset)), jira.WithMaxResults(resourcePageSize))
		if err != nil {
			return nil, wrapError(err, "failed to get projects")
		}

		for _, project := range projects {
			// The find endpoint does not return a project with the roles populated
			project, _, err := u.client.Project.Get(ctx, project.ID)
			if err != nil {
				return nil, wrapError(err, "failed to get project")
			}
			for _, roleLink := range project.Roles {
				roleId, err := parseRoleIdFromRoleLink(roleLink)
				if err != nil {
					return nil, wrapError(err, "failed to parse role id from role link")
				}
				roleIDToProjectNameMap[roleId] = project.Name
			}
		}

		if isLastPage(len(projects), resourcePageSize) {
			break
		}

		nextPage, err = getPageTokenFromOffset(bag, offset+int64(resourcePageSize))
		if err != nil {
			return nil, err
		}
		if nextPage == "" {
			break
		}
	}

	return roleIDToProjectNameMap, nil
}

func (u *roleResourceType) List(ctx context.Context, _ *v2.ResourceId, _ *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)
	roleIDToProjectName, err := u.mapRoleIDsToProjectNames(ctx)
	if err != nil {
		l.Error(wrapError(err, "failed to map role IDs to project names").Error(), zap.Error(err))
	}
	roles, _, err := u.client.Role.GetList(ctx)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get roles")
	}

	var rv []*v2.Resource
	for _, role := range *roles {
		role := role
		if name, ok := roleIDToProjectName[role.ID]; ok {
			role.Name = fmt.Sprintf("%s - %s", name, role.Name)
		}
		resource, err := roleResource(&role)
		if err != nil {
			return nil, "", nil, wrapError(err, "failed to create role resource")
		}

		rv = append(rv, resource)
	}

	return rv, "", nil, nil
}
