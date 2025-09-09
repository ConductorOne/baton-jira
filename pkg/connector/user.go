package connector

import (
	"context"
	"fmt"
	"strings"

	"github.com/conductorone/baton-jira/pkg/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
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
		resourceType     *v2.ResourceType
		client           *client.Client
		skipCustomerUser bool
	}
)

func getResourceTypeAnnotation() annotations.Annotations {
	annotations := annotations.Annotations{}
	annotations.Update(&v2.SkipEntitlementsAndGrants{})

	return annotations
}

func userResource(_ context.Context, user *jira.User) (*v2.Resource, error) {
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
	case "customer":
		return v2.UserTrait_ACCOUNT_TYPE_HUMAN
	default:
		return v2.UserTrait_ACCOUNT_TYPE_UNSPECIFIED
	}
}

func (u *userResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return u.resourceType
}

func userBuilder(c *client.Client, skipCustomerUser bool) *userResourceType {
	return &userResourceType{
		resourceType:     resourceTypeUser,
		client:           c,
		skipCustomerUser: skipCustomerUser,
	}
}

func (u *userResourceType) Entitlements(ctx context.Context, resource *v2.Resource, opts rs.SyncOpAttrs) ([]*v2.Entitlement, *rs.SyncOpResults, error) {
	return nil, nil, nil
}

func (u *userResourceType) Grants(ctx context.Context, resource *v2.Resource, opts rs.SyncOpAttrs) ([]*v2.Grant, *rs.SyncOpResults, error) {
	return nil, nil, nil
}

func (u *userResourceType) List(ctx context.Context, _ *v2.ResourceId, opts rs.SyncOpAttrs) ([]*v2.Resource, *rs.SyncOpResults, error) {
	bag, offset, err := parsePageToken(opts.PageToken.Token, &v2.ResourceId{ResourceType: resourceTypeUser.Id})
	if err != nil {
		return nil, nil, err
	}

	users, resp, err := u.client.Jira().User.Find(ctx, "", jira.WithMaxResults(resourcePageSize), jira.WithStartAt(int(offset)))
	if err != nil {
		var statusCode *int
		if resp != nil {
			statusCode = &resp.StatusCode
		}
		return nil, nil, wrapError(err, "failed to list users", statusCode)
	}

	var resources []*v2.Resource
	for i := range users {
		if u.skipCustomerUser && users[i].AccountType == "customer" {
			continue
		}

		resource, err := userResource(ctx, &users[i])

		if err != nil {
			return nil, nil, err
		}

		resources = append(resources, resource)
	}

	if isLastPage(len(users), resourcePageSize) {
		return resources, nil, nil
	}

	nextPage, err := getPageTokenFromOffset(bag, offset+int64(resourcePageSize))
	if err != nil {
		return nil, nil, err
	}

	return resources, &rs.SyncOpResults{NextPageToken: nextPage}, nil
}

func (o *userResourceType) CreateAccountCapabilityDetails(ctx context.Context) (*v2.CredentialDetailsAccountProvisioning, annotations.Annotations, error) {
	return &v2.CredentialDetailsAccountProvisioning{
		SupportedCredentialOptions: []v2.CapabilityDetailCredentialOption{
			v2.CapabilityDetailCredentialOption_CAPABILITY_DETAIL_CREDENTIAL_OPTION_NO_PASSWORD,
		},
		PreferredCredentialOption: v2.CapabilityDetailCredentialOption_CAPABILITY_DETAIL_CREDENTIAL_OPTION_NO_PASSWORD,
	}, nil, nil
}

func (o *userResourceType) CreateAccount(ctx context.Context, accountInfo *v2.AccountInfo, credentialOptions *v2.LocalCredentialOptions) (
	connectorbuilder.CreateAccountResponse,
	[]*v2.PlaintextData,
	annotations.Annotations,
	error,
) {
	body, err := getCreateInvitationBody(accountInfo)
	if err != nil {
		return nil, nil, nil, err
	}

	user, resp, err := o.client.Jira().User.Create(ctx, &jira.User{
		EmailAddress: body.Email,
		Products:     body.Products,
	})
	if err != nil {
		var statusCode *int
		if resp != nil {
			statusCode = &resp.StatusCode
		}
		return nil, nil, nil, wrapError(err, "failed to create user", statusCode)
	}

	resource, err := userResource(ctx, user)
	if err != nil {
		return nil, nil, nil, wrapError(err, "failed to create user resource", nil)
	}

	return &v2.CreateAccountResponse_SuccessResult{
		Resource: resource,
	}, nil, nil, nil
}

func getCreateInvitationBody(accountInfo *v2.AccountInfo) (*client.CreateUserBody, error) {
	if accountInfo == nil {
		return nil, fmt.Errorf("account info is nil")
	}

	pMap := accountInfo.Profile.AsMap()

	// Default to empty products array
	// Per models.go: "To create a user without product access, set this field to be an empty array."
	var products []string

	// Safely check if products field exists in the map
	if productsValue, exists := pMap["products"]; exists && productsValue != nil {
		// Type assertion with ok check
		productsInterface, ok := productsValue.([]interface{})
		if !ok {
			// Return error instead of panicking
			return nil, fmt.Errorf("products field is not a list: %T", productsValue)
		}

		products = make([]string, 0, len(productsInterface))
		for _, product := range productsInterface {
			productStr, ok := product.(string)
			if !ok {
				return nil, fmt.Errorf("invalid product type: %T", product)
			}
			products = append(products, productStr)
		}
	}

	return &client.CreateUserBody{
		Email:    accountInfo.Login,
		Products: products,
	}, nil
}
