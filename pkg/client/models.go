package client

// https://developer.atlassian.com/cloud/jira/platform/rest/v2/api-group-users/#api-rest-api-2-user-post-request
type CreateUserBody struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	Email    string `json:"emailAddress"`
	// Products the new user has access to.
	//	- Valid products are: jira-core, jira-servicedesk, jira-product-discovery, jira-software.
	//	- To create a user without product access, set this field to be an empty array.
	Products []string `json:"products"`
}
