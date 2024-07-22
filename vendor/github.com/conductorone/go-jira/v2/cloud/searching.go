package cloud

import (
	"fmt"
	"strings"
)

type (
	searchParam struct {
		name  string
		value string
	}

	search []searchParam

	searchF func(search) search
)

func WithExpand(expands ...string) searchF {
	return func(s search) search {
		s = append(s, searchParam{name: "expand", value: strings.Join(expands, ",")})
		return s
	}
}

// WithMaxResults sets the max results to return
func WithMaxResults(maxResults int) searchF {
	return func(s search) search {
		s = append(s, searchParam{name: "maxResults", value: fmt.Sprintf("%d", maxResults)})
		return s
	}
}

// WithAccountId sets the account id to search
func WithAccountId(accountId string) searchF {
	return func(s search) search {
		s = append(s, searchParam{name: "accountId", value: accountId})
		return s
	}
}

// WithUsername sets the username to search
func WithUsername(username string) searchF {
	return func(s search) search {
		s = append(s, searchParam{name: "username", value: username})
		return s
	}
}
