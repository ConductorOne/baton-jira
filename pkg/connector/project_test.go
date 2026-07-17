package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/conductorone/baton-jira/pkg/client"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-sdk/pkg/types/sessions"
	jira "github.com/conductorone/go-jira/v2/cloud"
)

// memorySessionStore is a minimal map-backed sessions.SessionStore for tests.
// Namespace options are ignored; test keys do not collide across namespaces.
type memorySessionStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMemorySessionStore() *memorySessionStore {
	return &memorySessionStore{data: map[string][]byte{}}
}

func (m *memorySessionStore) Get(_ context.Context, key string, _ ...sessions.SessionStoreOption) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	return v, ok, nil
}

func (m *memorySessionStore) GetMany(_ context.Context, keys []string, _ ...sessions.SessionStoreOption) (map[string][]byte, []string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found := map[string][]byte{}
	var missing []string
	for _, k := range keys {
		if v, ok := m.data[k]; ok {
			found[k] = v
		} else {
			missing = append(missing, k)
		}
	}
	return found, missing, nil
}

func (m *memorySessionStore) Set(_ context.Context, key string, value []byte, _ ...sessions.SessionStoreOption) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *memorySessionStore) SetMany(_ context.Context, values map[string][]byte, _ ...sessions.SessionStoreOption) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range values {
		m.data[k] = v
	}
	return nil
}

func (m *memorySessionStore) Delete(_ context.Context, key string, _ ...sessions.SessionStoreOption) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *memorySessionStore) Clear(_ context.Context, _ ...sessions.SessionStoreOption) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = map[string][]byte{}
	return nil
}

func (m *memorySessionStore) GetAll(_ context.Context, _ string, _ ...sessions.SessionStoreOption) (map[string][]byte, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string][]byte, len(m.data))
	for k, v := range m.data {
		out[k] = v
	}
	return out, "", nil
}

const testAccountType = "atlassian"

type fakeUser struct {
	AccountID    string `json:"accountId"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
	Active       bool   `json:"active"`
	AccountType  string `json:"accountType"`
}

// newParticipantServer serves the two endpoints project Grants hits:
//   - GET /rest/api/2/project/{id} — project metadata
//   - GET /rest/api/2/user/viewissue/search — participant pages, sliced from
//     the filtered participant list exactly like Jira Cloud does (startAt
//     indexes the filtered list; empirically validated for CXP-762).
//
// It records every startAt it receives so tests can assert window placement.
func newParticipantServer(t *testing.T, participants []fakeUser, startAts *[]int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/rest/api/2/project/"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":   "10000",
				"key":  "TEST",
				"name": "Test Project",
				"lead": map[string]interface{}{
					"accountId":   "lead-1",
					"displayName": "Lead User",
					"active":      true,
					"accountType": testAccountType,
				},
			})
		case r.URL.Path == "/rest/api/2/user/viewissue/search":
			startAt, _ := strconv.Atoi(r.URL.Query().Get("startAt"))
			maxResults, _ := strconv.Atoi(r.URL.Query().Get("maxResults"))
			*startAts = append(*startAts, startAt)

			end := startAt + maxResults
			if startAt > len(participants) {
				startAt = len(participants)
			}
			if end > len(participants) {
				end = len(participants)
			}
			_ = json.NewEncoder(w).Encode(participants[startAt:end])
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// TestProjectGrantsPagination drives Grants through the full SDK-style token
// loop against a fake Jira and asserts that pagination advances without
// overlapping windows, emits every participant exactly once, and terminates.
func TestProjectGrantsPagination(t *testing.T) {
	// 7 participants with a page size of 3 → pages of 3, 3, 1, then the
	// deliberate empty confirmation page.
	participants := make([]fakeUser, 0, 7)
	for i := 0; i < 7; i++ {
		participants = append(participants, fakeUser{
			AccountID:    fmt.Sprintf("user-%d", i),
			DisplayName:  fmt.Sprintf("User %d", i),
			EmailAddress: fmt.Sprintf("user%d@example.com", i),
			Active:       true,
			AccountType:  testAccountType,
		})
	}

	var startAts []int
	srv := newParticipantServer(t, participants, &startAts)
	defer srv.Close()

	origPageSize := participantPageSize
	participantPageSize = 3
	defer func() { participantPageSize = origPageSize }()

	c, err := client.New("user", "token", srv.URL)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	builder := projectBuilder(c, false)
	resource, err := projectResource(context.Background(), &jira.Project{ID: "10000", Name: "Test Project"})
	if err != nil {
		t.Fatalf("failed to build project resource: %v", err)
	}

	attrs := rs.SyncOpAttrs{
		Session:   newMemorySessionStore(),
		PageToken: pagination.Token{},
	}

	principals := map[string]int{}
	leadGrants := 0
	pages := 0
	for {
		pages++
		if pages > 20 {
			t.Fatal("pagination did not terminate")
		}

		grants, results, err := builder.Grants(context.Background(), resource, attrs)
		if err != nil {
			t.Fatalf("Grants failed on page %d: %v", pages, err)
		}
		for _, g := range grants {
			switch {
			case strings.Contains(g.Entitlement.Id, leadEntitlement):
				leadGrants++
			default:
				principals[g.Principal.Id.Resource]++
			}
		}

		if results == nil || results.NextPageToken == "" {
			break
		}
		attrs.PageToken = pagination.Token{Token: results.NextPageToken}
	}

	// Windows advance by the requested page size: 0, 3, 6, then the empty
	// confirmation page at 9.
	wantStartAts := []int{0, 3, 6, 9}
	if len(startAts) != len(wantStartAts) {
		t.Fatalf("expected startAt sequence %v, got %v", wantStartAts, startAts)
	}
	for i, want := range wantStartAts {
		if startAts[i] != want {
			t.Fatalf("expected startAt sequence %v, got %v", wantStartAts, startAts)
		}
	}

	if leadGrants != 1 {
		t.Errorf("expected exactly 1 lead grant, got %d", leadGrants)
	}
	if len(principals) != len(participants) {
		t.Errorf("expected %d distinct participants, got %d", len(participants), len(principals))
	}
	for id, n := range principals {
		if n != 1 {
			t.Errorf("participant %s granted %d times, want exactly 1 (duplicate grants)", id, n)
		}
	}
}
