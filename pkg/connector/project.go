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
	project, err := p.client.GetProject(ctx, resource.Id.Resource)
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
		leadGrants, err := p.getLeadGrants(ctx, resource, project)
		if err != nil {
			return nil, "", nil, wrapError(err, "failed to get lead grants")
		}
		rv = append(rv, leadGrants...)
	}

	if p.skipProjectParticipants {
		return rv, "", nil, nil
	}

	participateGrants, isLastPage, err := p.getGrantsForProjectUsers(ctx, resource, project, int(offset), resourcePageSize)
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

	projects, _, err := u.client.Jira().Project.Find(ctx, jira.WithStartAt(int(offset)), jira.WithMaxResults(resourcePageSize))
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get projects")
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

func (o *projectResourceType) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) (annotations.Annotations, error) {
	// spaceID := entitlement.Resource.Id.Resource
	// role := strings.Split(entitlement.Id, ":")[2]
	//
	// resUser, err := o.client.GetUserByID(ctx, principal.Id.Resource)
	// if err != nil {
	// 	return nil, err
	// }
	// if len(resUser.Items) == 0 {
	// 	return nil, fmt.Errorf("baton-contentful: no user found for ID %s", principal.Id.Resource)
	// }
	//
	// roleID := ""
	// admin := role == spaceAdmin
	// if !admin {
	// 	resSpaceRoles, err := o.client.ListSpaceRoles(ctx, spaceID)
	// 	if err != nil {
	// 		return nil, fmt.Errorf("baton-contentful: failed to list space roles: %w", err)
	// 	}
	//
	// 	for _, item := range resSpaceRoles.Items {
	// 		if item.Name == role {
	// 			roleID = item.Sys.ID
	// 			break
	// 		}
	// 	}
	// }
	//
	// email := resUser.Items[0].Email
	// _, err = o.client.CreateSpaceMembership(ctx, spaceID, email, roleID, admin)
	// if err != nil {
	// 	return nil, err
	// }
	// return nil, nil
	return nil, nil
}

func (o *projectResourceType) Revoke(ctx context.Context, grant *v2.Grant) (annotations.Annotations, error) {
	// principal := grant.Principal
	// entitlement := grant.Entitlement
	// spaceID := entitlement.Resource.Id.Resource
	//
	// resSpaceMembership, err := o.client.GetSpaceMembershipByUser(ctx, spaceID, principal.Id.Resource)
	// if err != nil {
	// 	return nil, err
	// }
	//
	// if len(resSpaceMembership.Items) == 0 {
	// 	return annotations.New(&v2.GrantAlreadyRevoked{}), nil
	// }
	//
	// spaceMembershipID := resSpaceMembership.Items[0].Sys.ID
	// err = o.client.DeleteSpaceMembership(ctx, spaceID, spaceMembershipID)
	// if err != nil {
	// 	return nil, fmt.Errorf("baton-contentful: failed to delete team membership: %w", err)
	// }
	// return nil, nil
	return nil, nil
}
