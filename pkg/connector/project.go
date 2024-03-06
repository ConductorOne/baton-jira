package connector

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	grant "github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	jira "github.com/conductorone/go-jira/v2/cloud"
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

func (p *projectResourceType) getRolesForProject(ctx context.Context, project *jira.Project) ([]jira.Role, error) {
	var rv []jira.Role

	for _, roleLink := range project.Roles {
		roleId, err := parseRoleIdFromRoleLink(roleLink)
		if err != nil {
			return nil, err
		}

		role, _, err := p.client.Role.Get(ctx, roleId)
		if err != nil {
			return nil, err
		}

		rv = append(rv, *role)
	}

	return rv, nil
}

func (p *projectResourceType) getRolesForProjectId(ctx context.Context, projectID string) ([]jira.Role, error) {
	project, _, err := p.client.Project.Get(ctx, projectID)
	if err != nil {
		return nil, err
	}

	return p.getRolesForProject(ctx, project)
}

func (u *projectResourceType) Entitlements(ctx context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
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

	roles, err := u.getRolesForProjectId(ctx, resource.Id.Resource)
	if err != nil {
		return nil, "", nil, err
	}
	rv = append(rv, getPermissionEntitlementsFromRoles(resource, roles)...)

	return rv, "", nil, nil
}

func getPermissionEntitlementsFromRoles(resource *v2.Resource, roles []jira.Role) []*v2.Entitlement {
	var rv []*v2.Entitlement

	for _, role := range roles {
		permissionOptions := []ent.EntitlementOption{
			ent.WithGrantableTo(resourceTypeUser),
			ent.WithDescription(fmt.Sprintf("Role in %s project", resource.DisplayName)),
			ent.WithDisplayName(fmt.Sprintf("%s project %s", resource.DisplayName, role.Name)),
		}

		entitlement := ent.NewPermissionEntitlement(resource, role.Name, permissionOptions...)
		rv = append(rv, entitlement)
	}

	return rv
}

func (p *projectResourceType) Grants(ctx context.Context, resource *v2.Resource, pt *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	project, _, err := p.client.Project.Get(ctx, resource.Id.Resource)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get project")
	}

	var rv []*v2.Grant

	bag, offset, err := parsePageToken(pt.Token, &v2.ResourceId{ResourceType: resourceTypeProject.Id})
	if err != nil {
		return nil, "", nil, err
	}

	if offset == 0 {
		// handle grants without pagination
		leadGrants, err := getLeadGrants(ctx, resource, project)
		if err != nil {
			return nil, "", nil, wrapError(err, "failed to get lead grants")
		}
		rv = append(rv, leadGrants...)

		projectRoles, err := p.getRolesForProject(ctx, project)
		if err != nil {
			return nil, "", nil, wrapError(err, "failed to get roles for project")
		}

		roleGrants, err := getRoleGrants(ctx, p, resource, projectRoles)
		if err != nil {
			return nil, "", nil, wrapError(err, "failed to get role grants")
		}
		rv = append(rv, roleGrants...)
	}

	participateGrants, isLastPage, err := getGrantsForAllUsersIfProjectIsPublic(ctx, p, resource, project, int(offset), resourcePageSize)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get participate grants")
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

func getGrantsForAllUsersIfProjectIsPublic(ctx context.Context, p *projectResourceType, resource *v2.Resource, project *jira.Project, offset int, count int) ([]*v2.Grant, bool, error) {
	var rv []*v2.Grant

	lastPage := true
	if !project.IsPrivate {
		users, _, err := p.client.User.Find(ctx, "", jira.WithStartAt(offset), jira.WithMaxResults(count))
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
	}

	return rv, lastPage, nil
}

func getRoleGrants(ctx context.Context, p *projectResourceType, resource *v2.Resource, roles []jira.Role) ([]*v2.Grant, error) {
	var rv []*v2.Grant

	for _, role := range roles {
		role := role
		roleResource, err := roleResource(&role)
		if err != nil {
			return nil, err
		}

		grant := grant.NewGrant(
			resource,
			participateEntitlement,
			roleResource.Id,
			grant.WithAnnotation(
				&v2.GrantExpandable{
					EntitlementIds:  []string{fmt.Sprintf("role:%d:%s", role.ID, appointedEntitlement)},
					Shallow:         true,
					ResourceTypeIds: []string{resourceTypeUser.Id},
				},
			),
		)
		rv = append(rv, grant)
	}

	return rv, nil
}

// Unfortunatelly, the Jira API does not provide a way to get the role id from project.
// It only provides a link to the role. Like this: https://your-domain.atlassian.net/rest/api/3/project/10001/role/10002
// So, we need to parse the role id from the link.
func parseRoleIdFromRoleLink(roleLink string) (int, error) {
	regexPattern := `\/(\d+)\/?$` // Regex pattern to match the last number in the URL path
	r := regexp.MustCompile(regexPattern)

	matches := r.FindStringSubmatch(roleLink)

	if len(matches) < 2 {
		return 0, fmt.Errorf("failed to parse role id from role link")
	}

	lastNumber, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, err
	}

	return lastNumber, nil
}

func (u *projectResourceType) List(ctx context.Context, _ *v2.ResourceId, p *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	bag, offset, err := parsePageToken(p.Token, &v2.ResourceId{ResourceType: resourceTypeGroup.Id})
	if err != nil {
		return nil, "", nil, err
	}

	projects, _, err := u.client.Project.Find(ctx, jira.WithStartAt(int(offset)), jira.WithMaxResults(resourcePageSize))
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get projects")
	}

	var resources []*v2.Resource
	for _, project := range projects {
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

	if isLastPage(len(projects), resourcePageSize) {
		return resources, "", nil, nil
	}

	nextPage, err := getPageTokenFromOffset(bag, offset+int64(resourcePageSize))
	if err != nil {
		return nil, "", nil, err
	}

	return resources, nextPage, nil, nil
}
