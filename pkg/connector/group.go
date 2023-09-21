package connector

import (
	"context"

	jira "github.com/andygrunwald/go-jira/v2/cloud"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
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
	// TODO: just check but there is no entitlements for groups IMHO
	return nil, "", nil, nil
}

func (u *groupResourceType) Grants(ctx context.Context, resource *v2.Resource, token *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	// TODO: implement
	return nil, "", nil, nil
}

func (u *groupResourceType) List(ctx context.Context, _ *v2.ResourceId, pt *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {

}
