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

var resourceTypeGroup = &v2.ResourceType{
	Id:          "group",
	DisplayName: "Group",
	Traits: []v2.ResourceType_Trait{
		v2.ResourceType_TRAIT_GROUP,
	},
	Annotations: annotations.Annotations{},
}

type groupResourceType struct {
	resourceType *v2.ResourceType
	client       *jira.Client
}

func groupResource(ctx context.Context, group *jira.Group) (*v2.Resource, error) {
	profile := map[string]interface{}{
		"name": group.Name,
	}

	groupTraitOptions := []rs.GroupTraitOption{
		rs.WithGroupProfile(profile),
	}

	resource, err := rs.NewGroupResource(group.Name, resourceTypeGroup, group.Name, groupTraitOptions)
	if err != nil {
		return nil, err
	}

	return resource, nil
}

func (g *groupResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return g.resourceType
}

func groupBuilder(client *jira.Client) *groupResourceType {
	return &groupResourceType{
		resourceType: resourceTypeGroup,
		client:       client,
	}
}

func (u *groupResourceType) Entitlements(ctx context.Context, resource *v2.Resource, token *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	var rv []*v2.Entitlement

	assigmentOptions := []ent.EntitlementOption{
		ent.WithGrantableTo(resourceTypeUser),
		ent.WithDescription(fmt.Sprintf("Member of %s group", resource.DisplayName)),
		ent.WithDisplayName(fmt.Sprintf("%s group %s", resource.DisplayName, memberEntitlement)),
	}

	en := ent.NewAssignmentEntitlement(resource, memberEntitlement, assigmentOptions...)
	rv = append(rv, en)

	return rv, "", nil, nil
}

func (u *groupResourceType) Grants(ctx context.Context, resource *v2.Resource, token *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	groupMembers, _, err := u.client.Group.Get(ctx, resource.DisplayName, nil)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get group members")
	}

	var rv []*v2.Grant
	for _, groupMember := range groupMembers {
		user, err := userResource(ctx, &jira.User{
			Name:         groupMember.Name,
			Key:          groupMember.Key,
			AccountID:    groupMember.AccountID,
			EmailAddress: groupMember.EmailAddress,
			DisplayName:  groupMember.DisplayName,
			Active:       groupMember.Active,
			TimeZone:     groupMember.TimeZone,
			AccountType:  groupMember.AccountType,
		})
		if err != nil {
			return nil, "", nil, err
		}

		grant := grant.NewGrant(resource, memberEntitlement, user.Id)
		rv = append(rv, grant)
	}

	return rv, "", nil, nil
}

func (u *groupResourceType) List(ctx context.Context, _ *v2.ResourceId, pt *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	groups, _, _, err := u.client.Group.Find(ctx)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to list groups")
	}

	var resources []*v2.Resource
	for i := range groups {
		resource, err := groupResource(ctx, &groups[i])

		if err != nil {
			return nil, "", nil, err
		}

		resources = append(resources, resource)
	}

	return resources, "", nil, nil
}
