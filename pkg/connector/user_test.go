package connector

import (
	"context"
	"strings"
	"testing"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
)

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
