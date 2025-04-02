package cloud

import (
	"strings"
)

func WithExpand(expands ...string) UserSearchF {
	return func(s UserSearch) UserSearch {
		s = append(s, UserSearchParam{name: "expand", value: strings.Join(expands, ",")})
		return s
	}
}

func WithProjectId(projectId string) UserSearchF {
	return func(s UserSearch) UserSearch {
		s = append(s, UserSearchParam{name: "projectId", value: projectId})
		return s
	}
}

func WithProjectKey(projectKey string) UserSearchF {
	return func(s UserSearch) UserSearch {
		s = append(s, UserSearchParam{name: "projectKey", value: projectKey})
		return s
	}
}

func WithStatusCategory(statusCategory string) UserSearchF {
	return func(s UserSearch) UserSearch {
		s = append(s, UserSearchParam{name: "statusCategory", value: statusCategory})
		return s
	}
}
