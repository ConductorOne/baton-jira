package connector

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/conductorone/baton-jira/pkg/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/types"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	grant "github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	jira "github.com/conductorone/go-jira/v2/cloud"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var resourceTypeProjectRole = &v2.ResourceType{
	Id:          "project-role",
	DisplayName: "Project Role",
	Traits: []v2.ResourceType_Trait{
		v2.ResourceType_TRAIT_ROLE,
	},
}

type projectRoleResourceType struct {
	resourceType *v2.ResourceType
	client       *client.Client
}

func projectRoleResource(project *jira.Project, role *jira.Role) (*v2.Resource, error) {
	profile := map[string]interface{}{
		"name":        role.Name,
		"role_id":     role.ID,
		"project_id":  project.ID,
		"description": role.Description,
	}

	displayName := fmt.Sprintf("%s - %s", project.Name, role.Name)
	resourceID := projectRoleID(project, role)
	roleTraitOptions := []rs.RoleTraitOption{
		rs.WithRoleProfile(profile),
	}

	resource, err := rs.NewRoleResource(displayName, resourceTypeProjectRole, resourceID, roleTraitOptions)
	if err != nil {
		return nil, err
	}

	return resource, nil
}

func (p *projectRoleResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return p.resourceType
}

func projectRoleBuilder(c *client.Client) *projectRoleResourceType {
	return &projectRoleResourceType{
		resourceType: resourceTypeProjectRole,
		client:       c,
	}
}

func (u *projectRoleResourceType) Entitlements(ctx context.Context, resource *v2.Resource, _ *pagination.Token, rso types.ResourceSyncerOptions) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	var rv []*v2.Entitlement

	projectID, roleID, err := parseProjectRoleID(resource.Id.Resource)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to parse project role ID", nil)
	}

	project, err := u.client.GetProject(ctx, rso.Session, projectID)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get project", nil)
	}

	role, err := u.client.GetRole(ctx, rso.Session, roleID)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get role", nil)
	}

	assigmentOptions := []ent.EntitlementOption{
		ent.WithGrantableTo(resourceTypeUser, resourceTypeGroup),
		ent.WithDescription(fmt.Sprintf("Assigned to %s role on the %s project", role.Name, project.Name)),
		ent.WithDisplayName(fmt.Sprintf("%s Assignment", resource.DisplayName)),
	}
	rv = append(rv, ent.NewAssignmentEntitlement(resource, assignedEntitlement, assigmentOptions...))

	return rv, "", nil, nil
}

func (p *projectRoleResourceType) Grants(ctx context.Context, resource *v2.Resource, pt *pagination.Token, _ types.ResourceSyncerOptions) ([]*v2.Grant, string, annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	projectID, roleID, err := parseProjectRoleID(resource.Id.Resource)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to parse project role ID", nil)
	}

	var rv []*v2.Grant

	projectRoleActors, resp, err := p.client.Jira().Role.GetRoleActorsForProject(ctx, projectID, roleID)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil, "", nil, status.Error(codes.NotFound, fmt.Sprintf("failed to get role actors for project: %v", err))
		}
		return nil, "", nil, wrapError(err, "failed to get role actors for project", nil)
	}

	for _, actor := range projectRoleActors {
		var g *v2.Grant
		switch actor.Type {
		case atlassianUserRoleActor:
			userActor := &v2.ResourceId{
				ResourceType: resourceTypeUser.Id,
				Resource:     actor.ActorUser.AccountID,
			}
			g = grant.NewGrant(resource, assignedEntitlement, userActor)

		case atlassianGroupRoleActor:
			groupActor := &v2.ResourceId{
				ResourceType: resourceTypeGroup.Id,
				Resource:     actor.ActorGroup.GroupID,
			}
			g = grant.NewGrant(resource, assignedEntitlement, groupActor, grant.WithAnnotation(&v2.GrantExpandable{
				EntitlementIds:  []string{fmt.Sprintf("group:%s:%s", actor.ActorGroup.GroupID, memberEntitlement)},
				ResourceTypeIds: []string{resourceTypeUser.Id},
			}))

		default:
			l.Warn("unknown role actor type", zap.String("type", actor.Type))
			continue
		}

		rv = append(rv, g)
	}

	return rv, "", nil, nil
}

func (p *projectRoleResourceType) List(ctx context.Context, _ *v2.ResourceId, token *pagination.Token, rso types.ResourceSyncerOptions) ([]*v2.Resource, string, annotations.Annotations, error) {
	bag, offset, err := parsePageToken(token.Token, &v2.ResourceId{ResourceType: resourceTypeProjectRole.Id})
	if err != nil {
		return nil, "", nil, err
	}
	projects, _, err := p.client.Jira().Project.Find(ctx, jira.WithStartAt(int(offset)), jira.WithMaxResults(resourcePageSize))
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get projects", nil)
	}

	var ret []*v2.Resource

	err = p.client.SetProjects(ctx, rso.Session, projects)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get projects", nil)
	}

	for _, prj := range projects {
		roleIDs := make([]int, 0, len(prj.Roles))
		for _, roleLink := range prj.Roles {
			roleID, err := parseRoleIdFromRoleLink(roleLink)
			if err != nil {
				return nil, "", nil, wrapError(err, "failed to parse role id from role link", nil)
			}
			roleIDs = append(roleIDs, roleID)
		}
		if len(roleIDs) == 0 {
			continue
		}

		roles, err := p.client.GetRoles(ctx, rso.Session, roleIDs)
		if err != nil {
			return nil, "", nil, wrapError(err, "failed to get roles", nil)
		}

		for _, role := range roles {
			prr, err := projectRoleResource(&prj, role)
			if err != nil {
				return nil, "", nil, wrapError(err, "failed to create project role resource", nil)
			}
			ret = append(ret, prr)
		}
	}

	if isLastPage(len(projects), resourcePageSize) {
		return ret, "", nil, nil
	}

	nextPage, err := getPageTokenFromOffset(bag, offset+int64(resourcePageSize))
	if err != nil {
		return nil, "", nil, err
	}

	return ret, nextPage, nil, nil
}

func (p *projectRoleResourceType) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	if principal.Id.ResourceType != resourceTypeUser.Id {
		err := fmt.Errorf("baton-jira: only users can be granted to groups")

		l.Warn(
			err.Error(),
			zap.String("principal_type", principal.Id.ResourceType),
			zap.String("principal_id", principal.Id.Resource),
		)

		return nil, err
	}

	if entitlement.Id != ent.NewEntitlementID(entitlement.Resource, assignedEntitlement) {
		err := fmt.Errorf("baton-jira: invalid entitlement ID")

		l.Warn(
			err.Error(),
			zap.String("entitlement_id", entitlement.Id),
		)
		return nil, err
	}

	projectID, roleID, err := parseProjectRoleID(entitlement.Resource.Id.Resource)
	if err != nil {
		return nil, wrapError(err, "failed to parse project role ID", nil)
	}

	_, err = p.client.Jira().Role.AddUserToRole(ctx, projectID, roleID, principal.Id.Resource)
	if err != nil {
		if strings.Contains(err.Error(), "already a member of the project role.") {
			l.Info("user already a member of the project role",
				zap.String("project_id", projectID),
				zap.Int("role_id", roleID),
				zap.String("user", principal.Id.Resource),
			)
			return nil, nil
		}

		l.Error(
			"failed to add user to project role",
			zap.Error(err),
			zap.String("project_id", projectID),
			zap.Int("role_id", roleID),
			zap.String("user", principal.Id.Resource),
		)

		return nil, err
	}

	return nil, nil
}

func (p *projectRoleResourceType) Revoke(ctx context.Context, grant *v2.Grant) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	projectID, roleID, err := parseProjectRoleID(grant.Entitlement.Resource.Id.Resource)
	if err != nil {
		return nil, wrapError(err, "failed to parse project role ID", nil)
	}

	_, err = p.client.Jira().Role.RemoveUserFromRole(ctx, projectID, roleID, grant.Principal.Id.Resource)
	if err != nil {
		return nil, wrapError(err, "failed to remove user from project role", nil)
	}

	l.Info("removed user from project role",
		zap.String("project_id", projectID),
		zap.Int("role_id", roleID),
		zap.String("user", grant.Principal.Id.Resource),
	)

	return nil, nil
}
