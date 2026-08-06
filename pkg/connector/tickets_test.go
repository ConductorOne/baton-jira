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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newTicketSchemaServer(t *testing.T, issueTypeFieldsHandler func(w http.ResponseWriter, r *http.Request, projectID, issueTypeID string)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/rest/api/2/project/search"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"values": []map[string]interface{}{
					{
						"id":   "10001",
						"key":  "ENG",
						"name": "Engineering",
						"issueTypes": []map[string]interface{}{
							{"id": "100", "name": "Task"},
							{"id": "101", "name": "Story"},
						},
					},
				},
				"startAt":    0,
				"maxResults": 50,
				"total":      1,
				"isLast":     true,
			})
		case strings.HasPrefix(r.URL.Path, "/rest/api/3/statuses/search"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"values":     []map[string]interface{}{{"id": "1", "name": "Open", "statusCategory": "TODO"}},
				"startAt":    0,
				"maxResults": 50,
				"total":      1,
				"isLast":     true,
			})
		case strings.Contains(r.URL.Path, "/rest/api/2/issue/createmeta/"):
			parts := strings.Split(r.URL.Path, "/")
			projectID := parts[len(parts)-3]
			issueTypeID := parts[len(parts)-1]
			issueTypeFieldsHandler(w, r, projectID, issueTypeID)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestListTicketSchemas_Skips404IssueTypeFields(t *testing.T) {
	srv := newTicketSchemaServer(t, func(w http.ResponseWriter, r *http.Request, projectID, issueTypeID string) {
		if issueTypeID == "100" {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errorMessages": []string{"Issue type not found"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"fields":     []interface{}{},
			"startAt":    0,
			"maxResults": 100,
			"total":      0,
		})
	})
	defer srv.Close()

	c, err := client.New(context.Background(), "user", "token", srv.URL)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	j := &Jira{client: c}
	schemas, _, _, err := j.ListTicketSchemas(context.Background(), &pagination.Token{Size: 50})
	if err != nil {
		t.Fatalf("expected no error when individual issue types return 404, got: %v", err)
	}

	if len(schemas) != 1 {
		t.Fatalf("expected 1 schema (the non-404 issue type), got %d", len(schemas))
	}
	if !strings.Contains(schemas[0].DisplayName, "Story") {
		t.Errorf("expected the surviving schema to be Story, got %q", schemas[0].DisplayName)
	}
}

func TestListTicketSchemas_PropagatesNon404Errors(t *testing.T) {
	srv := newTicketSchemaServer(t, func(w http.ResponseWriter, r *http.Request, projectID, issueTypeID string) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"errorMessages": []string{"Internal server error"},
		})
	})
	defer srv.Close()

	c, err := client.New(context.Background(), "user", "token", srv.URL)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	j := &Jira{client: c}
	_, _, _, err = j.ListTicketSchemas(context.Background(), &pagination.Token{Size: 50})
	if err == nil {
		t.Fatal("expected an error for 500 response, got nil")
	}
	if status.Code(err) != codes.Unavailable {
		t.Errorf("expected Unavailable code for 500 error, got %v", status.Code(err))
	}
}
