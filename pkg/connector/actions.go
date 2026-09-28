package connector

import (
	"context"
	"fmt"

	config "github.com/conductorone/baton-sdk/pb/c1/config/v1"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/actions"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	ActionEnableUser  = "enable_user"
	ActionDisableUser = "disable_user"
)

func userIDArg() []*config.Field {
	return []*config.Field{
		{
			Name:        "user_id",
			DisplayName: "User ID",
			Field: &config.Field_ResourceIdField{
				ResourceIdField: &config.ResourceIdField{
					Rules: &config.ResourceIDRules{
						AllowedResourceTypeIds: []string{resourceTypeUser.Id},
					},
				},
			},
			IsRequired: true,
		},
	}
}

func successReturn() []*config.Field {
	return []*config.Field{
		{Name: "success", DisplayName: "Success", Field: &config.Field_BoolField{}},
	}
}

var enableUserAction = &v2.BatonActionSchema{
	Name:        ActionEnableUser,
	Arguments:   userIDArg(),
	ReturnTypes: successReturn(),
	ActionType:  []v2.ActionType{v2.ActionType_ACTION_TYPE_ACCOUNT_ENABLE},
}

var disableUserAction = &v2.BatonActionSchema{
	Name:        ActionDisableUser,
	Arguments:   userIDArg(),
	ReturnTypes: successReturn(),
	ActionType:  []v2.ActionType{v2.ActionType_ACTION_TYPE_ACCOUNT_DISABLE},
}

func (o *Jira) GlobalActions(ctx context.Context, registry actions.ActionRegistry) error {
	if err := registry.Register(ctx, enableUserAction, o.enableUser); err != nil {
		return err
	}
	if err := registry.Register(ctx, disableUserAction, o.disableUser); err != nil {
		return err
	}
	return nil
}

func (o *Jira) enableUser(ctx context.Context, args *structpb.Struct) (*structpb.Struct, annotations.Annotations, error) {
	accountID, err := o.userIDFromArgs(args)
	if err != nil {
		return nil, nil, err
	}
	// Repeat calls return 204, but Atlassian also documents a 409 conflict; treat it as already done.
	if err := o.atlassianClient.EnableUser(ctx, accountID); err != nil && status.Code(err) != codes.AlreadyExists {
		return nil, nil, fmt.Errorf("baton-jira: failed to enable user: %w", err)
	}
	return actionSuccess(), nil, nil
}

func (o *Jira) disableUser(ctx context.Context, args *structpb.Struct) (*structpb.Struct, annotations.Annotations, error) {
	accountID, err := o.userIDFromArgs(args)
	if err != nil {
		return nil, nil, err
	}
	if err := o.atlassianClient.DisableUser(ctx, accountID); err != nil && status.Code(err) != codes.AlreadyExists {
		return nil, nil, fmt.Errorf("baton-jira: failed to disable user: %w", err)
	}
	return actionSuccess(), nil, nil
}

func actionSuccess() *structpb.Struct {
	return &structpb.Struct{Fields: map[string]*structpb.Value{"success": structpb.NewBoolValue(true)}}
}

func (o *Jira) userIDFromArgs(args *structpb.Struct) (string, error) {
	if o.atlassianClient == nil {
		return "", fmt.Errorf("baton-jira: enable/disable user requires Atlassian organization credentials (atlassian-orgId and atlassian-api-token)")
	}
	rid, ok := actions.GetResourceIDArg(args, "user_id")
	if !ok || rid.GetResource() == "" {
		return "", fmt.Errorf("baton-jira: missing required argument user_id")
	}
	if rid.GetResourceType() != resourceTypeUser.Id {
		return "", fmt.Errorf("baton-jira: user_id must be a user resource")
	}
	return rid.GetResource(), nil
}
