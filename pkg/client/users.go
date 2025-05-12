package client

import (
	"context"
	"fmt"
	"io"

	jira "github.com/conductorone/go-jira/v2/cloud"
)

// https://docs.atlassian.com/software/jira/docs/api/REST/9.14.0/#api/2/user-createUser
func (c *Client) CreateUser(ctx context.Context, body *CreateUserBody) (*jira.User, error) {
	user, res, err := c.Jira().User.Create(ctx, &jira.User{
		Name:         body.Name,
		Password:     body.Password,
		EmailAddress: body.Email,
		Products:     body.Products,
	})
	if err != nil {
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		// __AUTO_GENERATED_PRINT_VAR_START__
		fmt.Println(fmt.Sprintf("CreateUser res.Body: %+v", string(b))) // __AUTO_GENERATED_PRINT_VAR_END__
		return user, fmt.Errorf("failed to create user: %w", err)
	}
	return nil, nil
}
