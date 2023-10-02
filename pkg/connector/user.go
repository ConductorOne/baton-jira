package connector

import (
	"context"
	"strings"

	jira "github.com/andygrunwald/go-jira/v2/cloud"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
)

var (
	// TODO: check if this is the correct way to define the resource type
	resourceTypeUser = &v2.ResourceType{
		Id:          "user",
		DisplayName: "User",
		Traits: []v2.ResourceType_Trait{
			v2.ResourceType_TRAIT_USER,
		},
		Annotations: annotations.Annotations{},
	}
)

type (
	userResourceType struct {
		resourceType *v2.ResourceType
		client       *jira.Client
	}
)

func userResource(ctx context.Context, user *jira.User) (*v2.Resource, error) {
	names := strings.Split(user.DisplayName, " ")
	profile := map[string]interface{}{
		"login":      user.EmailAddress,
		"first_name": names[0],
		"user_id":    user.AccountID,
	}
	if len(names) > 1 {
		profile["last_name"] = names[1]
	}

	var userStatus v2.UserTrait_Status_Status
	if user.Active {
		userStatus = v2.UserTrait_Status_STATUS_ENABLED
	} else {
		userStatus = v2.UserTrait_Status_STATUS_DISABLED
	}

	userTraitOptions := []rs.UserTraitOption{
		rs.WithUserProfile(profile),
		rs.WithStatus(userStatus),
		rs.WithAccountType(mapAccountType(user.AccountType)),
	}

	resource, err := rs.NewUserResource(user.DisplayName, resourceTypeUser, user.AccountID, userTraitOptions)
	if err != nil {
		return nil, err
	}

	return resource, nil
}

func mapAccountType(accountType string) v2.UserTrait_AccountType {
	switch accountType {
	case "atlassian":
		return v2.UserTrait_ACCOUNT_TYPE_HUMAN
	case "app":
		return v2.UserTrait_ACCOUNT_TYPE_SERVICE
	default:
		return v2.UserTrait_ACCOUNT_TYPE_UNSPECIFIED
	}
}

func (u *userResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return u.resourceType
}

func userBuilder(client *jira.Client) *userResourceType {
	return &userResourceType{
		resourceType: resourceTypeUser,
		client:       client,
	}
}

func (u *userResourceType) Entitlements(ctx context.Context, resource *v2.Resource, token *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func (u *userResourceType) Grants(ctx context.Context, resource *v2.Resource, token *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func (u *userResourceType) List(ctx context.Context, _ *v2.ResourceId, pt *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	bag, offset, err := parsePageToken(pt.Token, &v2.ResourceId{ResourceType: resourceTypeUser.Id})
	if err != nil {
		return nil, "", nil, err
	}

	users, _, err := u.client.User.Find(ctx, "", jira.WithMaxResults(resourcePageSize), jira.WithStartAt(int(offset)))
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to list users")
	}

	var resources []*v2.Resource
	for i := range users {
		resource, err := userResource(ctx, &users[i])

		if err != nil {
			return nil, "", nil, err
		}

		resources = append(resources, resource)
	}

	if isLastPage(len(users), resourcePageSize) {
		return resources, "", nil, nil
	}

	nextPage, err := handleNextPage(bag, offset+int64(resourcePageSize))
	if err != nil {
		return nil, "", nil, err
	}

	return nil, nextPage, nil, nil
}
