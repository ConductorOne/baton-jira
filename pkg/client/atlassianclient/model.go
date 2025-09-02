package atlassianclient

import "time"

type APIError struct {
	Errors []struct {
		Id     string `json:"id"`
		Status string `json:"status"`
		Code   string `json:"code"`
		Title  string `json:"title"`
		Detail string `json:"detail"`
	} `json:"errors"`
}

type UserResponse struct {
	Data  []User `json:"data"`
	Links struct {
		Next string `json:"next"`
		Prev string `json:"prev"`
		Self string `json:"self"`
	} `json:"links"`
}

type User struct {
	AccountId        string    `json:"accountId"`
	AccountType      string    `json:"accountType"`
	Status           string    `json:"status"`
	AccountStatus    string    `json:"accountStatus"`
	MembershipStatus string    `json:"membershipStatus"`
	AddedToOrg       time.Time `json:"addedToOrg"`
	Name             string    `json:"name"`
	Nickname         string    `json:"nickname"`
	Email            string    `json:"email"`
	EmailVerified    bool      `json:"emailVerified"`
	ClaimStatus      string    `json:"claimStatus"`
	PlatformRoles    []string  `json:"platformRoles"`
	Picture          string    `json:"picture"`
	Avatar           string    `json:"avatar"`
	Counts           struct {
		Resources int `json:"resources"`
	} `json:"counts"`
	Links struct {
		Self string `json:"self"`
	} `json:"links"`
}

type WorkspaceResponse struct {
	Data  []Workspace `json:"data"`
	Links struct {
		Next string `json:"next"`
		Prev string `json:"prev"`
		Self string `json:"self"`
	} `json:"links"`
}

type Workspace struct {
	Id         string `json:"id"`
	Type       string `json:"type"`
	Attributes struct {
		Name          string   `json:"name"`
		TypeKey       string   `json:"typeKey"`
		Type          string   `json:"type"`
		Owner         string   `json:"owner"`
		Status        string   `json:"status"`
		StatusDetails []string `json:"statusDetails"`
		Icons         struct {
		} `json:"icons"`
		Avatars struct {
		} `json:"avatars"`
		Labels  []string `json:"labels"`
		Sandbox struct {
			Type string `json:"type"`
		} `json:"sandbox"`
		Usage     int      `json:"usage"`
		Capacity  int      `json:"capacity"`
		CreatedAt string   `json:"createdAt"`
		CreatedBy string   `json:"createdBy"`
		UpdatedAt string   `json:"updatedAt"`
		HostUrl   string   `json:"hostUrl"`
		Realm     string   `json:"realm"`
		Regions   []string `json:"regions"`
	} `json:"attributes"`
	Links struct {
		Self string `json:"self"`
	} `json:"links"`
	Relationships struct {
	} `json:"relationships"`
}

type GroupResponse struct {
	Data  []Group `json:"data"`
	Links struct {
		Next string `json:"next"`
		Prev string `json:"prev"`
		Self string `json:"self"`
	} `json:"links"`
}

type Group struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	DirectoryId string `json:"directoryId"`
	Counts      struct {
		Users     int `json:"users"`
		Resources int `json:"resources"`
	} `json:"counts"`
	Links struct {
		Self string `json:"self"`
	} `json:"links"`
}

type RoleAssignmentsResponse struct {
	Data  []RoleAssignment `json:"data"`
	Links struct {
		Next string `json:"next"`
		Prev string `json:"prev"`
		Self string `json:"self"`
	} `json:"links"`
}

type RoleAssignment struct {
	ResourceId    string   `json:"resourceId"`
	ResourceOwner string   `json:"resourceOwner"`
	Roles         []string `json:"roles"`
}

type DomainResponse struct {
	Data  []Domain `json:"data"`
	Links struct {
		Next string `json:"next"`
		Prev string `json:"prev"`
		Self string `json:"self"`
	} `json:"links"`
}

type Domain struct {
	Id         string `json:"id"`
	Type       string `json:"type"`
	Attributes struct {
		Name string `json:"name"`
	} `json:"attributes"`
	Links struct {
		Self string `json:"self"`
	} `json:"links"`
}
