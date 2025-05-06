package connector

import (
	"context"
	"fmt"
	"strings"

	"github.com/conductorone/baton-jira/pkg/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	jira "github.com/conductorone/go-jira/v2/cloud"
)

var (
	// TODO: check if this is the correct way to define the resource type
	resourceTypeUser = &v2.ResourceType{
		Id:          "user",
		DisplayName: "User",
		Traits: []v2.ResourceType_Trait{
			v2.ResourceType_TRAIT_USER,
		},
		Annotations: getResourceTypeAnnotation(),
	}
)

type (
	userResourceType struct {
		resourceType *v2.ResourceType
		client       *client.Client
	}
)

func getResourceTypeAnnotation() annotations.Annotations {
	annotations := annotations.Annotations{}
	annotations.Update(&v2.SkipEntitlementsAndGrants{})

	return annotations
}

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

	if user.EmailAddress != "" {
		userTraitOptions = append(userTraitOptions, rs.WithEmail(user.EmailAddress, true))
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

func userBuilder(c *client.Client) *userResourceType {
	return &userResourceType{
		resourceType: resourceTypeUser,
		client:       c,
	}
}

func (u *userResourceType) Entitlements(ctx context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func (u *userResourceType) Grants(ctx context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func (u *userResourceType) List(ctx context.Context, _ *v2.ResourceId, p *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	bag, offset, err := parsePageToken(p.Token, &v2.ResourceId{ResourceType: resourceTypeUser.Id})
	if err != nil {
		return nil, "", nil, err
	}

	users, _, err := u.client.Jira().User.Find(ctx, "", jira.WithMaxResults(resourcePageSize), jira.WithStartAt(int(offset)))
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

	nextPage, err := getPageTokenFromOffset(bag, offset+int64(resourcePageSize))
	if err != nil {
		return nil, "", nil, err
	}

	return resources, nextPage, nil, nil
}

func (o *userResourceType) CreateAccountCapabilityDetails(ctx context.Context) (*v2.CredentialDetailsAccountProvisioning, annotations.Annotations, error) {
	return &v2.CredentialDetailsAccountProvisioning{
		SupportedCredentialOptions: []v2.CapabilityDetailCredentialOption{
			v2.CapabilityDetailCredentialOption_CAPABILITY_DETAIL_CREDENTIAL_OPTION_NO_PASSWORD,
		},
		PreferredCredentialOption: v2.CapabilityDetailCredentialOption_CAPABILITY_DETAIL_CREDENTIAL_OPTION_NO_PASSWORD,
	}, nil, nil
}

func (o *userResourceType) CreateAccount(ctx context.Context, accountInfo *v2.AccountInfo, credentialOptions *v2.CredentialOptions) (
	connectorbuilder.CreateAccountResponse,
	[]*v2.PlaintextData,
	annotations.Annotations,
	error,
) {
	body, err := getCreateInvitationBody(accountInfo)
	if err != nil {
		return nil, nil, nil, err
	}

	user, err := o.client.CreateUser(ctx, body)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("baton-contentful: failed to create user: %w", err)
	}

	resource, err := userResource(ctx, user)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("baton-contentful: failed to create user resource: %w", err)
	}

	return &v2.CreateAccountResponse_SuccessResult{
		Resource: resource,
	}, nil, nil, nil
}

func getCreateInvitationBody(accountInfo *v2.AccountInfo) (*client.CreateUserBody, error) {
	pMap := accountInfo.Profile.AsMap()
	name := pMap["name"].(string)
	password := pMap["password"].(string)
	displayName := pMap["displayName"].(string)

	return &client.CreateUserBody{
		Name:        name,
		DisplayName: displayName,
		Password:    password,
		Email:       accountInfo.Login,
	}, nil
}
