package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/conductorone/baton-jira/pkg/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const testIssueTypeTask = "Task"

// ticketIssueType is a small fixture describing one issue type of a
// fixture project, and what the create-meta fields endpoint should return
// for that (project, issue_type) pair.
type ticketIssueType struct {
	id      string
	name    string
	subtask bool

	// httpStatus, when non-zero, makes the create-meta fields endpoint for
	// this pair fail with that status instead of returning fields.
	httpStatus int

	fields []map[string]interface{}
}

type ticketProjectFixture struct {
	id         string
	key        string
	name       string
	issueTypes []ticketIssueType

	// statuses returned for this project; also used to count how many times
	// the statuses endpoint was hit for this project.
	statuses []map[string]interface{}
}

// newTicketSchemaServer serves the three endpoints ListTicketSchemas hits:
//   - GET /rest/api/2/project/search       - paginated project list
//   - GET /rest/api/3/statuses/search      - paginated per-project statuses
//   - GET /rest/api/2/issue/createmeta/{project}/issuetypes/{issueType} - per-pair fields
//
// projectPageSize controls how many projects a single project-search call
// returns, so tests can force multiple project pages.
func newTicketSchemaServer(t *testing.T, projects []ticketProjectFixture, projectPageSize int, statusesCalls *map[string]int) *httptest.Server {
	t.Helper()

	byKeyOrID := func(idOrKey string) *ticketProjectFixture {
		for i := range projects {
			if projects[i].key == idOrKey || projects[i].id == idOrKey {
				return &projects[i]
			}
		}
		return nil
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/rest/api/2/project/search":
			startAt, _ := strconv.Atoi(r.URL.Query().Get("startAt"))
			end := startAt + projectPageSize
			if end > len(projects) {
				end = len(projects)
			}
			if startAt > len(projects) {
				startAt = len(projects)
			}

			values := make([]map[string]interface{}, 0, end-startAt)
			for _, p := range projects[startAt:end] {
				issueTypes := make([]map[string]interface{}, 0, len(p.issueTypes))
				for _, it := range p.issueTypes {
					issueTypes = append(issueTypes, map[string]interface{}{
						"id":      it.id,
						"name":    it.name,
						"subtask": it.subtask,
					})
				}
				values = append(values, map[string]interface{}{
					"id":         p.id,
					"key":        p.key,
					"name":       p.name,
					"issueTypes": issueTypes,
				})
			}

			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"startAt":    startAt,
				"maxResults": projectPageSize,
				"total":      len(projects),
				"values":     values,
			})

		case "/rest/api/3/statuses/search":
			projectID := r.URL.Query().Get("projectId")
			p := byKeyOrID(projectID)
			if p == nil {
				t.Errorf("unexpected projectId in statuses request: %s", projectID)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if statusesCalls != nil {
				(*statusesCalls)[p.key]++
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"startAt":    0,
				"maxResults": 100,
				"total":      len(p.statuses),
				"values":     p.statuses,
			})

		default:
			// GET /rest/api/2/issue/createmeta/{project}/issuetypes/{issueTypeId}
			var projectIDOrKey, issueTypeID string
			if n, _ := fmt.Sscanf(r.URL.Path, "/rest/api/2/issue/createmeta/%s", &projectIDOrKey); n == 1 {
				parts := splitLast(projectIDOrKey, "/issuetypes/")
				projectIDOrKey, issueTypeID = parts[0], parts[1]
			}

			p := byKeyOrID(projectIDOrKey)
			if p == nil {
				t.Errorf("unexpected project in createmeta request: %s (%s)", projectIDOrKey, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
				return
			}

			var it *ticketIssueType
			for i := range p.issueTypes {
				if p.issueTypes[i].id == issueTypeID {
					it = &p.issueTypes[i]
					break
				}
			}
			if it == nil {
				t.Errorf("unexpected issue type in createmeta request: %s", issueTypeID)
				w.WriteHeader(http.StatusNotFound)
				return
			}

			if it.httpStatus != 0 {
				w.WriteHeader(it.httpStatus)
				return
			}

			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"startAt":    0,
				"maxResults": 100,
				"total":      len(it.fields),
				"fields":     it.fields,
			})
		}
	}))
}

// splitLast splits s on the last occurrence of sep, returning [before, after].
func splitLast(s, sep string) [2]string {
	idx := -1
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			idx = i
		}
	}
	if idx == -1 {
		return [2]string{s, ""}
	}
	return [2]string{s[:idx], s[idx+len(sep):]}
}

func newTestJira(t *testing.T, baseURL string) *Jira {
	t.Helper()
	c, err := client.New(context.Background(), "user", "token", baseURL)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	return &Jira{client: c}
}

func stringField(fieldID, name string) map[string]interface{} {
	return map[string]interface{}{
		"fieldId":  fieldID,
		"key":      fieldID,
		"name":     name,
		"required": false,
		"schema":   map[string]interface{}{"type": "string", "custom": "com.atlassian.jira.plugin.system.customfieldtypes:textfield"},
	}
}

func componentsField(allowedValueIDs ...string) map[string]interface{} {
	allowed := make([]map[string]interface{}, 0, len(allowedValueIDs))
	for _, id := range allowedValueIDs {
		allowed = append(allowed, map[string]interface{}{"id": id, "name": "Component " + id})
	}
	return map[string]interface{}{
		"fieldId":       componentsFieldID,
		"key":           componentsFieldID,
		"name":          "Component/s",
		"required":      false,
		"schema":        map[string]interface{}{"type": "array", "items": "component"},
		"allowedValues": allowed,
	}
}

// --- Fix A: precise error classification -----------------------------------

func TestListTicketSchemas_404SkippedAtDebug(t *testing.T) {
	projects := []ticketProjectFixture{
		{
			id: "10000", key: "TEST", name: "Test Project",
			issueTypes: []ticketIssueType{
				{id: "1", name: testIssueTypeTask, fields: []map[string]interface{}{stringField("customfield_1", "Foo")}},
				{id: "2", name: "Story", httpStatus: http.StatusNotFound},
				{id: "3", name: "Improvement", fields: []map[string]interface{}{stringField("customfield_2", "Bar")}},
			},
		},
	}
	statusesCalls := map[string]int{}
	srv := newTicketSchemaServer(t, projects, 50, &statusesCalls)
	defer srv.Close()

	j := newTestJira(t, srv.URL)

	core, logs := observer.New(zap.DebugLevel)
	ctx := ctxzap.ToContext(context.Background(), zap.New(core))

	schemas, nextToken, _, err := j.ListTicketSchemas(ctx, &pagination.Token{Size: 50})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if nextToken != "" {
		t.Fatalf("expected pagination to complete, got next token %q", nextToken)
	}
	if len(schemas) != 2 {
		t.Fatalf("expected 2 schemas (404 skipped), got %d", len(schemas))
	}

	var debugCount, warnCount int
	for _, entry := range logs.All() {
		if entry.Message == "issue type has no create-meta fields for project, skipping" {
			if entry.Level == zapcore.DebugLevel {
				debugCount++
			}
			if entry.Level == zapcore.WarnLevel {
				warnCount++
			}
		}
	}
	if debugCount != 1 {
		t.Errorf("expected exactly 1 Debug-level skip log, got %d", debugCount)
	}
	if warnCount != 0 {
		t.Errorf("expected 0 Warn-level skip logs (404s must not log at Warn), got %d", warnCount)
	}
}

func TestListTicketSchemas_NonNotFoundErrorPropagates(t *testing.T) {
	tests := []struct {
		name       string
		httpStatus int
	}{
		{"forbidden", http.StatusForbidden},
		{"server error", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projects := []ticketProjectFixture{
				{
					id: "10000", key: "TEST", name: "Test Project",
					issueTypes: []ticketIssueType{
						{id: "1", name: testIssueTypeTask, httpStatus: tt.httpStatus},
					},
				},
			}
			srv := newTicketSchemaServer(t, projects, 50, nil)
			defer srv.Close()

			j := newTestJira(t, srv.URL)
			ctx := ctxzap.ToContext(context.Background(), zap.NewNop())

			schemas, _, _, err := j.ListTicketSchemas(ctx, &pagination.Token{Size: 50})
			if err == nil {
				t.Fatalf("expected error for HTTP %d, got nil (schemas=%v)", tt.httpStatus, schemas)
			}
		})
	}
}

// --- Fix B: bounded, resumable (project, issue_type) pagination ------------

func buildManyIssueTypesProject(key string, id string, n int) ticketProjectFixture {
	its := make([]ticketIssueType, 0, n)
	for i := 0; i < n; i++ {
		its = append(its, ticketIssueType{
			id:     fmt.Sprintf("%d", i+1),
			name:   fmt.Sprintf("Type%d", i+1),
			fields: []map[string]interface{}{stringField(fmt.Sprintf("customfield_%d", i+1), fmt.Sprintf("Field %d", i+1))},
		})
	}
	return ticketProjectFixture{id: id, key: key, name: key, issueTypes: its}
}

func TestListTicketSchemas_FullExhaustionNoDuplicatesNoOmissions(t *testing.T) {
	projects := []ticketProjectFixture{
		buildManyIssueTypesProject("P1", "1", 5),
		buildManyIssueTypesProject("P2", "2", 3),
		buildManyIssueTypesProject("P3", "3", 7),
	}
	// A couple of guaranteed 404s interspersed among valid pairs.
	projects[0].issueTypes[2].httpStatus = http.StatusNotFound
	projects[2].issueTypes[0].httpStatus = http.StatusNotFound

	srv := newTicketSchemaServer(t, projects, 2, nil) // force multiple project pages too
	defer srv.Close()

	j := newTestJira(t, srv.URL)
	ctx := ctxzap.ToContext(context.Background(), zap.NewNop())

	origCap := maxIssueTypePairsPerPage
	maxIssueTypePairsPerPage = 3 // force resumes within and across projects
	defer func() { maxIssueTypePairsPerPage = origCap }()

	seen := map[string]int{}
	var allSchemas []*v2.TicketSchema
	token := &pagination.Token{Size: 2}
	calls := 0
	totalPairs := 5 + 3 + 7

	for {
		calls++
		if calls > 2*totalPairs {
			t.Fatalf("pagination did not terminate within %d calls (ghost-state / infinite loop)", 2*totalPairs)
		}

		schemas, nextToken, _, err := j.ListTicketSchemas(ctx, token)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", calls, err)
		}
		for _, s := range schemas {
			seen[s.Id]++
			allSchemas = append(allSchemas, s)
		}

		if nextToken == "" {
			break
		}
		if nextToken == token.Token {
			t.Fatalf("call %d: next page token repeated (%q) - infinite loop risk", calls, nextToken)
		}
		token = &pagination.Token{Size: 2, Token: nextToken}
	}

	wantSchemas := totalPairs - 2 // minus the two 404s
	if len(allSchemas) != wantSchemas {
		t.Fatalf("expected %d total schemas across all pages, got %d", wantSchemas, len(allSchemas))
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("schema %s returned %d times, want exactly 1 (duplicate)", id, count)
		}
	}
}

func TestListTicketSchemas_CapNeverExceeded(t *testing.T) {
	projects := []ticketProjectFixture{buildManyIssueTypesProject("BIG", "1", 20)}
	srv := newTicketSchemaServer(t, projects, 50, nil)
	defer srv.Close()

	j := newTestJira(t, srv.URL)
	ctx := ctxzap.ToContext(context.Background(), zap.NewNop())

	origCap := maxIssueTypePairsPerPage
	maxIssueTypePairsPerPage = 4
	defer func() { maxIssueTypePairsPerPage = origCap }()

	token := &pagination.Token{Size: 50}
	for i := 0; i < 10; i++ {
		schemas, nextToken, _, err := j.ListTicketSchemas(ctx, token)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(schemas) > maxIssueTypePairsPerPage {
			t.Fatalf("call returned %d schemas, exceeding cap of %d", len(schemas), maxIssueTypePairsPerPage)
		}
		if nextToken == "" {
			return
		}
		token = &pagination.Token{Size: 50, Token: nextToken}
	}
	t.Fatal("pagination did not terminate in time")
}

func TestListTicketSchemas_ResumesMidProjectNotFromZero(t *testing.T) {
	projects := []ticketProjectFixture{buildManyIssueTypesProject("MID", "1", 5)}
	srv := newTicketSchemaServer(t, projects, 50, nil)
	defer srv.Close()

	j := newTestJira(t, srv.URL)
	ctx := ctxzap.ToContext(context.Background(), zap.NewNop())

	origCap := maxIssueTypePairsPerPage
	maxIssueTypePairsPerPage = 3
	defer func() { maxIssueTypePairsPerPage = origCap }()

	// First call: issue types 1-3.
	firstSchemas, nextToken, _, err := j.ListTicketSchemas(ctx, &pagination.Token{Size: 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(firstSchemas) != 3 {
		t.Fatalf("expected 3 schemas on first call, got %d", len(firstSchemas))
	}
	if nextToken == "" {
		t.Fatal("expected a next page token (project has more issue types)")
	}

	// Second call must resume at issue type 4, not restart at issue type 1.
	secondSchemas, nextToken2, _, err := j.ListTicketSchemas(ctx, &pagination.Token{Size: 50, Token: nextToken})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(secondSchemas) != 2 {
		t.Fatalf("expected 2 remaining schemas (issue types 4-5), got %d", len(secondSchemas))
	}
	if nextToken2 != "" {
		t.Fatalf("expected pagination to complete, got %q", nextToken2)
	}

	firstIDs := map[string]bool{}
	for _, s := range firstSchemas {
		firstIDs[s.Id] = true
	}
	for _, s := range secondSchemas {
		if firstIDs[s.Id] {
			t.Fatalf("schema %s returned on both calls - resumed from zero instead of mid-project", s.Id)
		}
	}
}

// --- Fix C: project-scoped data reuse ---------------------------------------

func TestListTicketSchemas_ProjectScopedFieldsReusedNotRebuilt(t *testing.T) {
	projects := []ticketProjectFixture{
		{
			id: "10000", key: "TEST", name: "Test Project",
			statuses: []map[string]interface{}{
				{"id": "1", "name": "Done"},
				{"id": "2", "name": "Closed"},
			},
			issueTypes: []ticketIssueType{
				{id: "1", name: testIssueTypeTask, fields: []map[string]interface{}{componentsField("c1", "c2")}},
				{id: "2", name: "Story", fields: []map[string]interface{}{componentsField("c1", "c2")}},
			},
		},
	}
	srv := newTicketSchemaServer(t, projects, 50, nil)
	defer srv.Close()

	j := newTestJira(t, srv.URL)
	ctx := ctxzap.ToContext(context.Background(), zap.NewNop())

	schemas, _, _, err := j.ListTicketSchemas(ctx, &pagination.Token{Size: 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(schemas) != 2 {
		t.Fatalf("expected 2 schemas, got %d", len(schemas))
	}

	componentsA := schemas[0].CustomFields[componentsFieldID]
	componentsB := schemas[1].CustomFields[componentsFieldID]
	if componentsA == nil || componentsB == nil {
		t.Fatalf("expected both schemas to have a components custom field")
	}
	if componentsA != componentsB {
		t.Errorf("expected the components field to be reused (same pointer) across issue types in the same project, got distinct objects")
	}

	// Statuses must also be the exact same slice reused, not rebuilt.
	if len(schemas[0].Statuses) != 2 || len(schemas[1].Statuses) != 2 {
		t.Fatalf("expected both schemas to carry the project's 2 statuses")
	}
}

func TestListTicketSchemas_IssueTypeScopedFieldsStayDistinct(t *testing.T) {
	// A field that is NOT in projectScopedCustomFieldIDs (a regular custom
	// picklist) must still be computed independently per issue type, even
	// if its allowed values happen to differ between issue types.
	projects := []ticketProjectFixture{
		{
			id: "10000", key: "TEST", name: "Test Project",
			issueTypes: []ticketIssueType{
				{id: "1", name: testIssueTypeTask, fields: []map[string]interface{}{
					{
						"fieldId":  "customfield_99",
						"key":      "customfield_99",
						"name":     "Severity",
						"required": false,
						"schema":   map[string]interface{}{"type": "object", "custom": "com.atlassian.jira.plugin.system.customfieldtypes:select"},
						"allowedValues": []map[string]interface{}{
							{"id": "1", "name": "High"},
						},
					},
				}},
				{id: "2", name: "Story", fields: []map[string]interface{}{
					{
						"fieldId":  "customfield_99",
						"key":      "customfield_99",
						"name":     "Severity",
						"required": false,
						"schema":   map[string]interface{}{"type": "object", "custom": "com.atlassian.jira.plugin.system.customfieldtypes:select"},
						"allowedValues": []map[string]interface{}{
							{"id": "2", "name": "Low"},
						},
					},
				}},
			},
		},
	}
	srv := newTicketSchemaServer(t, projects, 50, nil)
	defer srv.Close()

	j := newTestJira(t, srv.URL)
	ctx := ctxzap.ToContext(context.Background(), zap.NewNop())

	schemas, _, _, err := j.ListTicketSchemas(ctx, &pagination.Token{Size: 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(schemas) != 2 {
		t.Fatalf("expected 2 schemas, got %d", len(schemas))
	}

	fieldA := schemas[0].CustomFields["customfield_99"]
	fieldB := schemas[1].CustomFields["customfield_99"]
	if fieldA == nil || fieldB == nil {
		t.Fatalf("expected both schemas to have customfield_99")
	}
	if fieldA == fieldB {
		t.Fatalf("issue-type-scoped field must not be shared across issue types with differing allowed values")
	}
}
