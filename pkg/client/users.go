package client

import (
	"context"
	"fmt"

	jira "github.com/conductorone/go-jira/v2/cloud"
)

// https://docs.atlassian.com/software/jira/docs/api/REST/9.14.0/#api/2/user-createUser
func (c *Client) CreateUser(ctx context.Context, body *CreateUserBody) (*jira.User, error) {
	user, _, err := c.Jira().User.Create(ctx, &jira.User{
		Name:         body.Name,
		Password:     body.Password,
		EmailAddress: body.Email,
		DisplayName:  body.DisplayName,
		Active:       body.Active,
		Key:          body.Key,
	})
	if err != nil {
		return user, fmt.Errorf("failed to create user: %w", err)
	}
	return nil, nil
}
