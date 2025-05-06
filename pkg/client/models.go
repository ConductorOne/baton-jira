package client

type CreateUserBody struct {
	Name        string `json:"name"`
	Password    string `json:"password"`
	Email       string `json:"emailAddress"`
	DisplayName string `json:"displayName"`
	Active      bool   `json:"active"`
	Key         string `json:"key"`
}

type CreateUserResponse struct {
	Name        string `json:"name"`
	Email       string `json:"emailAddress"`
	DisplayName string `json:"displayName"`
	Key         string `json:"key"`
	Self        string `json:"self"` // url
}
