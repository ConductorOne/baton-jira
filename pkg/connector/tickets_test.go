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

// ticketIssueType is a fixture describing one issue type of a fixture project.
type ticketIssueType struct {
	id      string
	name    string
	subtask bool

	// If non-zero, create-meta for this pair fails with this status.
	httpStatus int

	fields []map[string]interface{}
}

type ticketProjectFixture struct {
	id         string
	key        string
	name       string
	issueTypes []ticketIssueType
	statuses   []map[string]interface{}
}

// newTicketSchemaServer fakes the project search, statuses, and create-meta endpoints.
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
			// Honor the caller's maxResults like real Jira does, falling back to the
			// configured default only if none was sent. A mock that ignores maxResults
			// can't reproduce bugs triggered by the requested page size changing between calls.
			maxResults := projectPageSize
			if mr, err := strconv.Atoi(r.URL.Query().Get("maxResults")); err == nil && mr > 0 {
				maxResults = mr
			}
			end := startAt + maxResults
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
				"maxResults": maxResults,
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

	var debugCount, warnCount, rawErrCount int
	for _, entry := range logs.All() {
		if entry.Message == "issue type has no create-meta fields for project, skipping" {
			if entry.Level == zapcore.DebugLevel {
				debugCount++
			}
			if entry.Level == zapcore.WarnLevel {
				warnCount++
			}
		}
		if entry.Message == "error getting issue type fields" && entry.Level >= zapcore.WarnLevel {
			rawErrCount++
		}
	}
	if debugCount != 1 {
		t.Errorf("expected exactly 1 Debug-level skip log, got %d", debugCount)
	}
	if warnCount != 0 {
		t.Errorf("expected 0 Warn-level skip logs (404s must not log at Warn), got %d", warnCount)
	}
	if rawErrCount != 0 {
		t.Errorf("expected 0 Warn/Error logs for the unclassified 404, got %d", rawErrCount)
	}
}

func TestListTicketSchemas_ServerErrorPropagates(t *testing.T) {
	tests := []struct {
		name       string
		httpStatus int
	}{
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

func TestListTicketSchemas_ClientErrorsSkippedNotPropagated(t *testing.T) {
	// One inaccessible (project, issue_type) pair must be skipped, not abort the sync.
	tests := []struct {
		name       string
		httpStatus int
	}{
		{"forbidden", http.StatusForbidden},
		{"unauthorized", http.StatusUnauthorized},
		{"not found", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projects := []ticketProjectFixture{
				{
					id: "10000", key: "TEST", name: "Test Project",
					issueTypes: []ticketIssueType{
						{id: "1", name: testIssueTypeTask, httpStatus: tt.httpStatus},
						{id: "2", name: "Story", fields: []map[string]interface{}{stringField("customfield_1", "Field 1")}},
					},
				},
			}
			srv := newTicketSchemaServer(t, projects, 50, nil)
			defer srv.Close()

			j := newTestJira(t, srv.URL)
			ctx := ctxzap.ToContext(context.Background(), zap.NewNop())

			schemas, _, _, err := j.ListTicketSchemas(ctx, &pagination.Token{Size: 50})
			if err != nil {
				t.Fatalf("expected HTTP %d to be skipped, got error: %v", tt.httpStatus, err)
			}
			if len(schemas) != 1 {
				t.Fatalf("expected the accessible issue type to still produce a schema, got %d schemas", len(schemas))
			}
			if schemas[0].DisplayName != "Story" {
				t.Errorf("expected the surviving schema to be for Story, got %q", schemas[0].DisplayName)
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
	j.maxIssueTypePairsPerPage = 3 // force resumes within and across projects
	ctx := ctxzap.ToContext(context.Background(), zap.NewNop())

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
	j.maxIssueTypePairsPerPage = 4
	ctx := ctxzap.ToContext(context.Background(), zap.NewNop())

	token := &pagination.Token{Size: 50}
	for i := 0; i < 10; i++ {
		schemas, nextToken, _, err := j.ListTicketSchemas(ctx, token)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(schemas) > j.maxIssueTypePairsPerPage {
			t.Fatalf("call returned %d schemas, exceeding cap of %d", len(schemas), j.maxIssueTypePairsPerPage)
		}
		if nextToken == "" {
			return
		}
		token = &pagination.Token{Size: 50, Token: nextToken}
	}
	t.Fatal("pagination did not terminate in time")
}

// TestListTicketSchemas_SurvivesShrinkingCallerPageSize is the CXP-936 regression: C1's
// driver shrinks the caller's page size as it nears its own result cap (e.g. 8, 8, 4, 4, ...).
// The project-window size used to compute a resumed ProjectIndexInPage must stay pinned to
// what it was when that index was stashed, not be recomputed from whatever the resuming
// call happens to send - otherwise a resume can land past the end of a smaller refetched
// window, silently drop the rest of that window, and duplicate part of it on the call after.
func TestListTicketSchemas_SurvivesShrinkingCallerPageSize(t *testing.T) {
	const numProjects = 10
	const issueTypesPerProject = 2

	projects := make([]ticketProjectFixture, 0, numProjects)
	for i := 0; i < numProjects; i++ {
		projects = append(projects, buildManyIssueTypesProject(
			fmt.Sprintf("P%d", i), fmt.Sprintf("%d", i+1), issueTypesPerProject))
	}
	// Fixture issue type names collide across projects (Type1, Type2); schema IDs are
	// projectKey:issueTypeID, so they stay unique even though names repeat.

	srv := newTicketSchemaServer(t, projects, resourcePageSize, nil)
	defer srv.Close()

	j := newTestJira(t, srv.URL)
	j.maxIssueTypePairsPerPage = 3 // force several resumes per project window
	ctx := ctxzap.ToContext(context.Background(), zap.NewNop())

	// Mimic C1's driver: page size shrinks toward a result cap as results accumulate.
	// The shrink from 8 to 2 must land below the project index already reached inside
	// the size-8 window (index 3), which is what actually triggers the defect.
	callerSizes := []int{8, 8, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2}

	seen := map[string]int{}
	var order []string
	var token *pagination.Token
	totalPairs := numProjects * issueTypesPerProject

	for call := 0; ; call++ {
		if call >= len(callerSizes) {
			t.Fatalf("pagination did not terminate within %d calls", len(callerSizes))
		}
		size := callerSizes[call]
		if token == nil {
			token = &pagination.Token{Size: size}
		} else {
			token = &pagination.Token{Size: size, Token: token.Token}
		}

		schemas, nextToken, _, err := j.ListTicketSchemas(ctx, token)
		if err != nil {
			t.Fatalf("call %d (size=%d): unexpected error: %v", call, size, err)
		}
		for _, s := range schemas {
			seen[s.Id]++
			order = append(order, s.Id)
		}

		if nextToken == "" {
			break
		}
		if nextToken == token.Token {
			t.Fatalf("call %d: next page token repeated (%q) - infinite loop risk", call, nextToken)
		}
		token = &pagination.Token{Token: nextToken}
	}

	if len(order) != totalPairs {
		t.Fatalf("expected %d total schemas emitted across all pages, got %d: %v", totalPairs, len(order), order)
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("schema %s emitted %d times, want exactly 1 (duplicate caused by a page-size change mid-enumeration)", id, count)
		}
	}
	if len(seen) != totalPairs {
		t.Errorf("expected %d distinct schemas, got %d (tail lost after a page-size change mid-enumeration)", totalPairs, len(seen))
	}
}

// TestListTicketSchemas_GuardsShrinkingResumeWindow covers the guard added after CXP-936: if
// the pinned project window returns fewer projects on a resumed call than it did when the
// token was issued (a project was deleted, or access to it was lost, mid-sync), the stashed
// ProjectIndexInPage can land past the end of the shrunk window. Without the guard, the resume
// loop never executes, the shrunk window looks like the last page, and ListTicketSchemas
// returns an empty page with an empty next-page token - silently ending the whole sync and
// dropping every remaining project instead of just the one that disappeared.
func TestListTicketSchemas_GuardsShrinkingResumeWindow(t *testing.T) {
	p1 := buildManyIssueTypesProject("P1", "1", 2)
	p2 := buildManyIssueTypesProject("P2", "2", 2)
	p3 := buildManyIssueTypesProject("P3", "3", 2)
	full := []ticketProjectFixture{p1, p2, p3}
	shrunk := []ticketProjectFixture{p1} // P2 and P3 vanish before the resume is served

	byKeyOrID := func(projects []ticketProjectFixture, idOrKey string) *ticketProjectFixture {
		for i := range projects {
			if projects[i].key == idOrKey || projects[i].id == idOrKey {
				return &projects[i]
			}
		}
		return nil
	}

	searchCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		active := full
		if searchCalls > 0 {
			active = shrunk
		}

		switch r.URL.Path {
		case "/rest/api/2/project/search":
			searchCalls++
			startAt, _ := strconv.Atoi(r.URL.Query().Get("startAt"))
			end := len(active)
			if startAt > end {
				startAt = end
			}
			page := active[startAt:end]

			values := make([]map[string]interface{}, 0, len(page))
			for _, p := range page {
				issueTypes := make([]map[string]interface{}, 0, len(p.issueTypes))
				for _, it := range p.issueTypes {
					issueTypes = append(issueTypes, map[string]interface{}{
						"id": it.id, "name": it.name, "subtask": it.subtask,
					})
				}
				values = append(values, map[string]interface{}{
					"id": p.id, "key": p.key, "name": p.name, "issueTypes": issueTypes,
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"startAt": startAt, "maxResults": len(page), "total": len(active), "values": values,
			})

		case "/rest/api/3/statuses/search":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"startAt": 0, "maxResults": 100, "total": 0, "values": []map[string]interface{}{},
			})

		default:
			var projectIDOrKey, issueTypeID string
			if n, _ := fmt.Sscanf(r.URL.Path, "/rest/api/2/issue/createmeta/%s", &projectIDOrKey); n == 1 {
				parts := splitLast(projectIDOrKey, "/issuetypes/")
				projectIDOrKey, issueTypeID = parts[0], parts[1]
			}
			p := byKeyOrID(active, projectIDOrKey)
			if p == nil {
				t.Errorf("unexpected create-meta request for project %s", projectIDOrKey)
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
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"startAt": 0, "maxResults": 100, "total": len(it.fields), "fields": it.fields,
			})
		}
	}))
	defer srv.Close()

	j := newTestJira(t, srv.URL)
	j.maxIssueTypePairsPerPage = 2 // exhausts P1's 2 pairs, stashing mid-window at P2 (index 1)
	ctx := ctxzap.ToContext(context.Background(), zap.NewNop())

	// First call: full 3-project window; the cap hits right after P1, stashing
	// ProjectIndexInPage=1 (P2) against a window pinned at size 3.
	first, nextToken, _, err := j.ListTicketSchemas(ctx, &pagination.Token{Size: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("expected 2 schemas from P1, got %d", len(first))
	}
	if nextToken == "" {
		t.Fatal("expected a next page token (P2/P3 still pending)")
	}

	// Resume: the pinned window now only returns P1 (P2/P3 disappeared), so the stashed
	// index (1) is out of bounds. The guard must log at Debug and advance the window
	// instead of falling through to the terminating return.
	core, logs := observer.New(zap.DebugLevel)
	debugCtx := ctxzap.ToContext(context.Background(), zap.New(core))

	second, nextToken2, _, err := j.ListTicketSchemas(debugCtx, &pagination.Token{Size: 3, Token: nextToken})
	if err != nil {
		t.Fatalf("unexpected error on resume: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("expected no schemas on the guard call, got %d", len(second))
	}
	if nextToken2 == "" {
		t.Fatal("expected pagination to continue past the shrunk window, got empty next token")
	}

	foundDebug := false
	for _, entry := range logs.All() {
		if entry.Message == "ticket schema project window shrank on resume, advancing to next window" {
			foundDebug = true
		}
	}
	if !foundDebug {
		t.Error("expected a Debug log for the shrunk resume window")
	}

	// Final call: the underlying data set is now exhausted, so pagination must terminate
	// cleanly rather than looping or erroring.
	third, nextToken3, _, err := j.ListTicketSchemas(ctx, &pagination.Token{Size: 3, Token: nextToken2})
	if err != nil {
		t.Fatalf("unexpected error on final call: %v", err)
	}
	if len(third) != 0 {
		t.Fatalf("expected no schemas past the end of the shrunk data set, got %d", len(third))
	}
	if nextToken3 != "" {
		t.Fatalf("expected pagination to terminate, got next token %q", nextToken3)
	}
}

func TestListTicketSchemas_RelocatesResumeAfterFrontOfWindowShift(t *testing.T) {
	p1 := buildManyIssueTypesProject("P1", "1", 2)
	p2 := buildManyIssueTypesProject("P2", "2", 3)
	p2.statuses = []map[string]interface{}{{"id": "1", "name": "P2Done"}}
	p3 := buildManyIssueTypesProject("P3", "3", 2)
	p3.statuses = []map[string]interface{}{{"id": "2", "name": "P3Done"}}

	full := []ticketProjectFixture{p1, p2, p3}
	shifted := []ticketProjectFixture{p2, p3} // P1 vanishes from the FRONT before the resume

	byKeyOrID := func(projects []ticketProjectFixture, idOrKey string) *ticketProjectFixture {
		for i := range projects {
			if projects[i].key == idOrKey || projects[i].id == idOrKey {
				return &projects[i]
			}
		}
		return nil
	}

	// active only changes inside the project/search branch, so it stays fixed for the rest of that call.
	active := full
	searchCalls := 0
	statusesCalls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/rest/api/2/project/search":
			if searchCalls > 0 {
				active = shifted
			}
			searchCalls++
			startAt, _ := strconv.Atoi(r.URL.Query().Get("startAt"))
			end := len(active)
			if startAt > end {
				startAt = end
			}
			page := active[startAt:end]

			values := make([]map[string]interface{}, 0, len(page))
			for _, p := range page {
				issueTypes := make([]map[string]interface{}, 0, len(p.issueTypes))
				for _, it := range p.issueTypes {
					issueTypes = append(issueTypes, map[string]interface{}{
						"id": it.id, "name": it.name, "subtask": it.subtask,
					})
				}
				values = append(values, map[string]interface{}{
					"id": p.id, "key": p.key, "name": p.name, "issueTypes": issueTypes,
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"startAt": startAt, "maxResults": len(page), "total": len(active), "values": values,
			})

		case "/rest/api/3/statuses/search":
			projectID := r.URL.Query().Get("projectId")
			p := byKeyOrID(active, projectID)
			if p == nil {
				t.Errorf("unexpected projectId in statuses request: %s", projectID)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			statusesCalls[p.key]++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"startAt": 0, "maxResults": 100, "total": len(p.statuses), "values": p.statuses,
			})

		default:
			var projectIDOrKey, issueTypeID string
			if n, _ := fmt.Sscanf(r.URL.Path, "/rest/api/2/issue/createmeta/%s", &projectIDOrKey); n == 1 {
				parts := splitLast(projectIDOrKey, "/issuetypes/")
				projectIDOrKey, issueTypeID = parts[0], parts[1]
			}
			p := byKeyOrID(active, projectIDOrKey)
			if p == nil {
				t.Errorf("unexpected create-meta request for project %s", projectIDOrKey)
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
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"startAt": 0, "maxResults": 100, "total": len(it.fields), "fields": it.fields,
			})
		}
	}))
	defer srv.Close()

	j := newTestJira(t, srv.URL)
	j.maxIssueTypePairsPerPage = 3 // P1's 2 pairs + P2's first pair, stashing mid-P2 (index 1, issue type 1)
	ctx := ctxzap.ToContext(context.Background(), zap.NewNop())

	// First call: full 3-project window; the cap hits mid-P2, stashing P2's index and statuses.
	first, nextToken, _, err := j.ListTicketSchemas(ctx, &pagination.Token{Size: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(first) != 3 { // P1 x2 + P2's first issue type
		t.Fatalf("expected 3 schemas (P1 x2, P2 x1), got %d", len(first))
	}
	if nextToken == "" {
		t.Fatal("expected a next page token (P2 remainder / P3 still pending)")
	}
	if statusesCalls["P2"] != 1 {
		t.Fatalf("expected exactly 1 statuses call for P2 on the first call, got %d", statusesCalls["P2"])
	}

	// Resume: P1 disappears from the front of the window, shifting P2 into the index the token stashed for P3, so the fix must relocate by key instead of trusting the stale index.
	core, logs := observer.New(zap.DebugLevel)
	debugCtx := ctxzap.ToContext(context.Background(), zap.New(core))

	second, nextToken2, _, err := j.ListTicketSchemas(debugCtx, &pagination.Token{Size: 3, Token: nextToken})
	if err != nil {
		t.Fatalf("unexpected error on resume: %v", err)
	}
	// P2 finishes and the leftover cap budget starts P3 too; what matters is each schema carries the right project's statuses.
	if len(second) != 3 {
		t.Fatalf("expected 3 schemas (P2's remainder x2, P3's first x1), got %d", len(second))
	}
	for _, s := range second[:2] {
		if len(s.Statuses) != 1 || s.Statuses[0].DisplayName != "P2Done" {
			t.Fatalf("schema %s: expected P2's stashed statuses, got %v", s.Id, s.Statuses)
		}
	}
	if len(second[2].Statuses) != 1 || second[2].Statuses[0].DisplayName != "P3Done" {
		t.Fatalf("schema %s: expected P3's statuses, got %v", second[2].Id, second[2].Statuses)
	}
	if statusesCalls["P2"] != 1 {
		t.Errorf("expected P2's statuses to be reused from the stash, not refetched; got %d calls", statusesCalls["P2"])
	}
	if statusesCalls["P3"] != 1 {
		t.Errorf("expected exactly 1 fresh statuses call for P3, got %d", statusesCalls["P3"])
	}

	foundRelocate := false
	for _, entry := range logs.All() {
		if entry.Message == "ticket schema project window shifted on resume, relocating stashed project" {
			foundRelocate = true
		}
	}
	if !foundRelocate {
		t.Error("expected a Debug log for the relocated resume position")
	}

	if nextToken2 == "" {
		t.Fatal("expected pagination to continue to P3's remaining issue type")
	}

	// Final call: P3's remaining issue type must still be synced, not skipped or double-counted.
	third, nextToken3, _, err := j.ListTicketSchemas(ctx, &pagination.Token{Size: 3, Token: nextToken2})
	if err != nil {
		t.Fatalf("unexpected error on final call: %v", err)
	}
	if len(third) != 1 {
		t.Fatalf("expected 1 remaining schema from P3, got %d", len(third))
	}
	for _, s := range third {
		if len(s.Statuses) != 1 || s.Statuses[0].DisplayName != "P3Done" {
			t.Fatalf("schema %s: expected P3's statuses, got %v", s.Id, s.Statuses)
		}
	}
	if nextToken3 != "" {
		t.Fatalf("expected pagination to terminate, got next token %q", nextToken3)
	}
}

func TestListTicketSchemas_ResumesMidProjectNotFromZero(t *testing.T) {
	projects := []ticketProjectFixture{buildManyIssueTypesProject("MID", "1", 5)}
	srv := newTicketSchemaServer(t, projects, 50, nil)
	defer srv.Close()

	j := newTestJira(t, srv.URL)
	j.maxIssueTypePairsPerPage = 3
	ctx := ctxzap.ToContext(context.Background(), zap.NewNop())

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

func TestListTicketSchemas_StatusesNotRefetchedAcrossResume(t *testing.T) {
	projects := []ticketProjectFixture{buildManyIssueTypesProject("MID", "1", 10)}
	projects[0].statuses = []map[string]interface{}{{"id": "1", "name": "Done"}}

	statusesCalls := map[string]int{}
	srv := newTicketSchemaServer(t, projects, 50, &statusesCalls)
	defer srv.Close()

	j := newTestJira(t, srv.URL)
	j.maxIssueTypePairsPerPage = 3
	ctx := ctxzap.ToContext(context.Background(), zap.NewNop())

	token := &pagination.Token{Size: 50}
	for {
		schemas, nextToken, _, err := j.ListTicketSchemas(ctx, token)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, s := range schemas {
			if len(s.Statuses) != 1 || s.Statuses[0].DisplayName != "Done" {
				t.Fatalf("schema %s missing expected statuses: %v", s.Id, s.Statuses)
			}
		}
		if nextToken == "" {
			break
		}
		token = &pagination.Token{Size: 50, Token: nextToken}
	}

	if statusesCalls["MID"] != 1 {
		t.Errorf("expected exactly 1 getTicketStatuses call across all resumed pages, got %d", statusesCalls["MID"])
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

	allowedA := componentsA.GetPickMultipleObjectValues().GetAllowedValues()
	allowedB := componentsB.GetPickMultipleObjectValues().GetAllowedValues()
	if len(allowedA) == 0 || len(allowedB) == 0 {
		t.Fatalf("expected both components fields to have allowed values")
	}
	if allowedA[0] != allowedB[0] {
		t.Errorf("expected allowed values to be reused across issue types, got distinct objects")
	}

	if len(schemas[0].Statuses) != 2 || len(schemas[1].Statuses) != 2 {
		t.Fatalf("expected both schemas to carry the project's 2 statuses")
	}
}

func TestListTicketSchemas_ProjectScopedFieldRequiredVariesPerIssueType(t *testing.T) {
	// Required must not leak from one issue type's cached field to another.
	requiredComponents := componentsField("c1", "c2")
	requiredComponents["required"] = true
	optionalComponents := componentsField("c1", "c2")
	optionalComponents["required"] = false

	projects := []ticketProjectFixture{
		{
			id: "10000", key: "TEST", name: "Test Project",
			issueTypes: []ticketIssueType{
				{id: "1", name: testIssueTypeTask, fields: []map[string]interface{}{requiredComponents}},
				{id: "2", name: "Story", fields: []map[string]interface{}{optionalComponents}},
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

	componentsTask := schemas[0].CustomFields[componentsFieldID]
	componentsStory := schemas[1].CustomFields[componentsFieldID]
	if componentsTask == nil || componentsStory == nil {
		t.Fatalf("expected both schemas to have a components custom field")
	}
	if !componentsTask.GetRequired() {
		t.Errorf("expected components to be required for the Task issue type")
	}
	if componentsStory.GetRequired() {
		t.Errorf("expected components to be optional for the Story issue type, got required (stale value leaked from cache)")
	}
}

func fixVersionsField(allowedValueIDs ...string) map[string]interface{} {
	allowed := make([]map[string]interface{}, 0, len(allowedValueIDs))
	for _, id := range allowedValueIDs {
		allowed = append(allowed, map[string]interface{}{"id": id, "name": "v" + id})
	}
	return map[string]interface{}{
		"fieldId":       fixVersionsFieldID,
		"key":           fixVersionsFieldID,
		"name":          "Fix Version/s",
		"required":      true,
		"schema":        map[string]interface{}{"type": "array", "items": "version"},
		"allowedValues": allowed,
	}
}

func TestListTicketSchemas_EmptyAllowedValuesNotCachedOverNonEmpty(t *testing.T) {
	// A project-scoped field with no choices on one issue type must not poison the
	// cache for a later issue type that does have choices.
	projects := []ticketProjectFixture{
		{
			id: "10000", key: "TEST", name: "Test Project",
			issueTypes: []ticketIssueType{
				{id: "1", name: testIssueTypeTask, fields: []map[string]interface{}{fixVersionsField()}},
				{id: "2", name: "Story", fields: []map[string]interface{}{fixVersionsField("1", "2")}},
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

	fixVersionsStory := schemas[1].CustomFields[fixVersionsFieldID]
	if fixVersionsStory == nil {
		t.Fatalf("expected the Story schema to have a fixVersions custom field")
	}
	allowed := fixVersionsStory.GetPickMultipleObjectValues().GetAllowedValues()
	if len(allowed) != 2 {
		t.Errorf("expected 2 allowed values for Story's fixVersions, got %d (empty result from Task leaked via cache)", len(allowed))
	}
}

func TestListTicketSchemas_IssueTypeScopedFieldsStayDistinct(t *testing.T) {
	// Non-project-scoped fields must be computed independently per issue type.
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
