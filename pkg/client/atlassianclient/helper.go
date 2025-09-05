package atlassianclient

import (
	"fmt"
	"net/url"
	"strconv"
)

// maxItemsPerPage that the API allows is 100. The default value is 20.
const maxItemsPerPage = 100

type ReqOpt func(reqURL *url.URL)

func WithPageSize(pageSize int) ReqOpt {
	if pageSize < 0 {
		pageSize = 0
	}
	if pageSize > maxItemsPerPage {
		pageSize = maxItemsPerPage
	}

	return WithQueryParam("limit", strconv.Itoa(pageSize))
}

func WithPageToken(pageToken string) ReqOpt {
	return WithQueryParam("cursor", pageToken)
}

func WithQueryParam(key string, value string) ReqOpt {
	return func(reqURL *url.URL) {
		q := reqURL.Query()
		q.Set(key, value)
		reqURL.RawQuery = q.Encode()
	}
}

type RoleAssignmentBody struct {
	Role     string `json:"role"`
	Resource string `json:"resource"`
}

func (er *APIError) Message() string {
	if len(er.Errors) > 0 {
		return fmt.Sprintf("API error response detail: %s", er.Errors[0].Detail)
	}
	return "Error response empty"
}
