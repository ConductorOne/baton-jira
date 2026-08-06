package connector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/conductorone/baton-jira/pkg/client"
	"github.com/conductorone/baton-sdk/pkg/pagination"
)

func TestListTicketSchemasIncludesProjectInPermissionError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/rest/api/2/project/search"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"values":     []map[string]interface{}{{"id": "10001", "key": "ENG", "name": "Engineering"}},
				"startAt":    0,
				"maxResults": 50,
				"total":      1,
				"isLast":     true,
			})
		case strings.HasPrefix(r.URL.Path, "/rest/api/3/statuses/search"):
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errorMessages": []string{"No permission to view statuses."},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c, err := client.New(context.Background(), "user", "token", srv.URL)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	j := &Jira{client: c}
	_, _, _, err = j.ListTicketSchemas(context.Background(), &pagination.Token{Size: 50})
	if err == nil {
		t.Fatal("expected an error from a 403 on statuses search")
	}
	if !strings.Contains(err.Error(), "ENG") || !strings.Contains(err.Error(), "10001") {
		t.Fatalf("expected error to identify the project by key and ID, got: %v", err)
	}
}
