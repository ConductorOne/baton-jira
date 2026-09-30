package connector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/conductorone/baton-jira/pkg/client/atlassianclient"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"google.golang.org/protobuf/types/known/structpb"
)

const testSiteURL = "https://example.atlassian.net"

// rewriteTransport sends requests for api.atlassian.com to the test server, keeping the path untouched.
type rewriteTransport struct{ target *url.URL }

func (t rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme = t.target.Scheme
	r.URL.Host = t.target.Host
	return http.DefaultTransport.RoundTrip(r)
}

// newLifecycleClient answers every lifecycle call with lifecycleStatus and records its request URI.
func newLifecycleClient(t *testing.T, lifecycleStatus int, calls *[]string) *atlassianclient.AtlassianClient {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/admin/v2/orgs/org-1/workspaces" {
			_, _ = w.Write([]byte(`{"data":[{"id":"site-1","attributes":{"hostUrl":"` + testSiteURL + `"}}],"links":{}}`))
			return
		}
		if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/users/") {
			t.Errorf("unexpected request %s %s", r.Method, r.RequestURI)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		*calls = append(*calls, r.RequestURI)
		w.WriteHeader(lifecycleStatus)
		if lifecycleStatus >= 400 {
			_, _ = w.Write([]byte(`{"errors":[{"detail":"lifecycle failed"}]}`))
		}
	}))
	t.Cleanup(srv.Close)

	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	wrapper := uhttp.NewBaseHttpClient(&http.Client{Transport: rewriteTransport{target: target}})

	ac, _, err := atlassianclient.New(context.Background(), testSiteURL,
		atlassianclient.WithAccessToken("token"),
		atlassianclient.WithOrganizationID("org-1"),
		atlassianclient.WithHTTPClient(wrapper),
	)
	if err != nil {
		t.Fatalf("building atlassian client: %v", err)
	}
	return ac
}

func userIDArgs(resourceType, id string) *structpb.Struct {
	return &structpb.Struct{Fields: map[string]*structpb.Value{
		"user_id": structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{
			"resource_type_id": structpb.NewStringValue(resourceType),
			"resource_id":      structpb.NewStringValue(id),
		}}),
	}}
}

func TestUserDeleteRequiresOrgCredentials(t *testing.T) {
	u := userBuilder(nil, nil, false, nil)

	_, err := u.Delete(context.Background(), &v2.ResourceId{ResourceType: resourceTypeUser.Id, Resource: "acct-1"})
	if err == nil {
		t.Fatal("expected error when Atlassian org credentials are absent, got nil")
	}
	if !strings.Contains(err.Error(), "organization credentials") {
		t.Errorf("expected org-credentials error, got %v", err)
	}
}

func TestUserDelete(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		wantErr bool
	}{
		{name: "deleted", status: http.StatusNoContent},
		{name: "unknown account propagates", status: http.StatusNotFound, wantErr: true},
		{name: "server error propagates", status: http.StatusInternalServerError, wantErr: true},
		{name: "forbidden propagates", status: http.StatusForbidden, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			u := userBuilder(nil, newLifecycleClient(t, tt.status, &calls), false, nil)

			_, err := u.Delete(context.Background(), &v2.ResourceId{ResourceType: resourceTypeUser.Id, Resource: "557058:abc/def"})
			if tt.wantErr != (err != nil) {
				t.Fatalf("wantErr=%v, got %v", tt.wantErr, err)
			}
			if len(calls) != 1 || calls[0] != "/users/557058:abc%2Fdef/manage/lifecycle/delete" {
				t.Errorf("expected one escaped delete call, got %v", calls)
			}
		})
	}
}

func TestEnableDisableUserActions(t *testing.T) {
	type handler func(*Jira, context.Context, *structpb.Struct) (*structpb.Struct, error)
	enable := func(j *Jira, ctx context.Context, a *structpb.Struct) (*structpb.Struct, error) {
		r, _, err := j.enableUser(ctx, a)
		return r, err
	}
	disable := func(j *Jira, ctx context.Context, a *structpb.Struct) (*structpb.Struct, error) {
		r, _, err := j.disableUser(ctx, a)
		return r, err
	}

	tests := []struct {
		name    string
		action  handler
		op      string
		status  int
		wantErr bool
	}{
		{name: "enable", action: enable, op: "enable", status: http.StatusNoContent},
		{name: "enable already enabled", action: enable, op: "enable", status: http.StatusConflict},
		{name: "enable server error", action: enable, op: "enable", status: http.StatusInternalServerError, wantErr: true},
		{name: "disable", action: disable, op: "disable", status: http.StatusNoContent},
		{name: "disable already disabled", action: disable, op: "disable", status: http.StatusConflict},
		{name: "disable not found propagates", action: disable, op: "disable", status: http.StatusNotFound, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			j := &Jira{atlassianClient: newLifecycleClient(t, tt.status, &calls)}

			res, err := tt.action(j, context.Background(), userIDArgs(resourceTypeUser.Id, "acct-1"))
			if tt.wantErr != (err != nil) {
				t.Fatalf("wantErr=%v, got %v", tt.wantErr, err)
			}
			if !tt.wantErr && !res.GetFields()["success"].GetBoolValue() {
				t.Errorf("expected success=true, got %v", res)
			}
			want := "/users/acct-1/manage/lifecycle/" + tt.op
			if len(calls) != 1 || calls[0] != want {
				t.Errorf("expected one call to %s, got %v", want, calls)
			}
		})
	}
}

func TestUserActionArgValidation(t *testing.T) {
	var calls []string
	j := &Jira{atlassianClient: newLifecycleClient(t, http.StatusNoContent, &calls)}

	tests := []struct {
		name string
		args *structpb.Struct
	}{
		{name: "missing user_id", args: &structpb.Struct{Fields: map[string]*structpb.Value{}}},
		{name: "empty id", args: userIDArgs(resourceTypeUser.Id, "")},
		{name: "non-user resource", args: userIDArgs(resourceTypeGroup.Id, "grp-1")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := j.enableUser(context.Background(), tt.args); err == nil {
				t.Error("enable: expected error")
			}
			if _, _, err := j.disableUser(context.Background(), tt.args); err == nil {
				t.Error("disable: expected error")
			}
		})
	}
	if len(calls) != 0 {
		t.Errorf("invalid args must not reach the API, got %v", calls)
	}

	noOrg := &Jira{}
	if _, _, err := noOrg.enableUser(context.Background(), userIDArgs(resourceTypeUser.Id, "acct-1")); err == nil ||
		!strings.Contains(err.Error(), "organization credentials") {
		t.Errorf("expected org-credentials error, got %v", err)
	}
}
