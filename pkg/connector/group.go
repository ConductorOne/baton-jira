package connector

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/conductorone/baton-jira/pkg/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	grant "github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	jira "github.com/conductorone/go-jira/v2/cloud"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

var resourceTypeGroup = &v2.ResourceType{
	Id:          "group",
	DisplayName: "Group",
	Traits: []v2.ResourceType_Trait{
		v2.ResourceType_TRAIT_GROUP,
	},
}

type groupResourceType struct {
	resourceType *v2.ResourceType
	client       *client.Client
}

func groupResource(_ context.Context, group *jira.Group) (*v2.Resource, error) {
	profile := map[string]interface{}{
		"id":   group.ID,
		"name": group.Name,
	}

	groupTraitOptions := []rs.GroupTraitOption{
		rs.WithGroupProfile(profile),
	}

	resource, err := rs.NewGroupResource(group.Name, resourceTypeGroup, group.ID, groupTraitOptions)
	if err != nil {
		return nil, err
	}

	return resource, nil
}

func (g *groupResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return g.resourceType
}

func groupBuilder(c *client.Client) *groupResourceType {
	return &groupResourceType{
		resourceType: resourceTypeGroup,
		client:       c,
	}
}

func (u *groupResourceType) Entitlements(ctx context.Context, resource *v2.Resource, opts rs.SyncOpAttrs) ([]*v2.Entitlement, *rs.SyncOpResults, error) {
	var rv []*v2.Entitlement

	assigmentOptions := []ent.EntitlementOption{
		ent.WithGrantableTo(resourceTypeUser),
		ent.WithDescription(fmt.Sprintf("Member of %s group", resource.DisplayName)),
		ent.WithDisplayName(fmt.Sprintf("%s group %s", resource.DisplayName, memberEntitlement)),
	}

	en := ent.NewAssignmentEntitlement(resource, memberEntitlement, assigmentOptions...)
	rv = append(rv, en)

	return rv, nil, nil
}

func (u *groupResourceType) Grants(ctx context.Context, resource *v2.Resource, opts rs.SyncOpAttrs) ([]*v2.Grant, *rs.SyncOpResults, error) {
	bag, offset, err := parsePageToken(opts.PageToken.Token, &v2.ResourceId{ResourceType: resourceTypeGroup.Id})
	if err != nil {
		return nil, nil, err
	}

	groupMembers, resp, err := u.client.Jira().Group.GetGroupMembers(
		ctx,
		resource.Id.Resource,
		jira.WithStartAt(int(offset)),
		jira.WithMaxResults(resourcePageSize),
	)
	if err != nil {
		var statusCode *int
		if resp != nil {
			statusCode = &resp.StatusCode
		}
		return nil, nil, wrapError(err, "failed to get group members", statusCode)
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
			return nil, nil, err
		}

		grant := grant.NewGrant(resource, memberEntitlement, user.Id)
		rv = append(rv, grant)
	}

	if isLastPage(len(groupMembers), resourcePageSize) {
		return rv, nil, nil
	}

	nextPage, err := getPageTokenFromOffset(bag, offset+int64(resourcePageSize))
	if err != nil {
		return nil, nil, err
	}

	return rv, &rs.SyncOpResults{NextPageToken: nextPage}, nil
}

func (u *groupResourceType) List(ctx context.Context, _ *v2.ResourceId, opts rs.SyncOpAttrs) ([]*v2.Resource, *rs.SyncOpResults, error) {
	bag, offset, err := parsePageToken(opts.PageToken.Token, &v2.ResourceId{ResourceType: resourceTypeGroup.Id})
	if err != nil {
		return nil, nil, err
	}

	groups, resp, err := u.client.Jira().Group.Bulk(ctx, jira.WithMaxResults(resourcePageSize), jira.WithStartAt(int(offset)))
	if err != nil {
		var statusCode *int
		if resp != nil {
			statusCode = &resp.StatusCode
		}
		return nil, nil, wrapError(err, "failed to list groups", statusCode)
	}

	var resources []*v2.Resource
	for i := range groups {
		group := jira.Group{
			ID:   groups[i].ID,
			Name: groups[i].Name,
		}
		resource, err := groupResource(ctx, &group)

		if err != nil {
			return nil, nil, err
		}

		resources = append(resources, resource)
	}

	if isLastPage(len(groups), resourcePageSize) {
		return resources, nil, nil
	}

	nextPage, err := getPageTokenFromOffset(bag, offset+int64(resourcePageSize))
	if err != nil {
		return nil, nil, err
	}

	return resources, &rs.SyncOpResults{NextPageToken: nextPage}, nil
}

func (u *groupResourceType) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) (annotations.Annotations, error) {
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

	resp, err := u.client.Jira().Group.AddUserByGroupId(ctx, entitlement.Resource.Id.Resource, principal.Id.Resource)
	if err != nil {
		if strings.Contains(err.Error(), "User is already a member of") {
			return annotations.New(&v2.GrantAlreadyExists{}), nil
		}

		l.Error(
			"failed to add user to group",
			zap.Error(err),
			zap.String("group", entitlement.Resource.Id.Resource),
			zap.String("user", principal.Id.Resource),
		)

		return nil, err
	}

	if resp.StatusCode != http.StatusCreated {
		err := fmt.Errorf("baton-jira: failed to add user to group: %s", resp.Status)

		l.Error(
			err.Error(),
			zap.String("group", entitlement.Resource.Id.Resource),
			zap.String("user", principal.Id.Resource),
			zap.Int("status_code", resp.StatusCode),
		)

		return nil, err
	}

	return nil, nil
}

func (u *groupResourceType) Revoke(ctx context.Context, grant *v2.Grant) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	entitlement := grant.Entitlement
	principal := grant.Principal

	if principal.Id.ResourceType != resourceTypeUser.Id {
		err := fmt.Errorf("baton-jira: only users can be revoked from groups")

		l.Warn(
			err.Error(),
			zap.String("principal_type", principal.Id.ResourceType),
			zap.String("principal_id", principal.Id.Resource),
		)

		return nil, err
	}

	resp, err := u.client.Jira().Group.RemoveUserByGroupId(ctx, entitlement.Resource.Id.Resource, principal.Id.Resource)
	if err != nil {
		if strings.Contains(err.Error(), "not a member of") {
			return annotations.New(&v2.GrantAlreadyRevoked{}), nil
		}

		l.Error(
			"failed to remove user from group",
			zap.Error(err),
			zap.String("group", entitlement.Resource.Id.Resource),
			zap.String("user", principal.Id.Resource),
		)

		return nil, err
	}

	if resp.StatusCode >= 300 {
		err := fmt.Errorf("baton-jira: failed to remove user from group: %s", resp.Status)

		l.Error(
			err.Error(),
			zap.String("group", entitlement.Resource.Id.Resource),
			zap.String("user", principal.Id.Resource),
			zap.Int("status_code", resp.StatusCode),
		)

		return nil, err
	}

	return nil, nil
}
