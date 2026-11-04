package connector

import (
	"testing"

	"github.com/conductorone/baton-jira/pkg/client/atlassianclient"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
)

func TestParseIntoUserResourceStatus(t *testing.T) {
	tests := []struct {
		name             string
		status           string
		accountStatus    string
		membershipStatus string
		want             v2.Status_ResourceStatus
	}{
		{"active", "active", "active", "active", v2.Status_RESOURCE_STATUS_ENABLED},
		{"suspended", "suspended", "active", "suspended", v2.Status_RESOURCE_STATUS_DISABLED},
		{"not invited", "not_invited", "active", "no_membership", v2.Status_RESOURCE_STATUS_DISABLED},
		{"deactivated", "deactivated", "inactive", "active", v2.Status_RESOURCE_STATUS_DISABLED},
		{"for deletion", "for_deletion", "active", "active", v2.Status_RESOURCE_STATUS_DISABLED},
		{"unknown status, closed account", "something_new", "closed", "active", v2.Status_RESOURCE_STATUS_DISABLED},
		{"unknown status, suspended membership", "something_new", "active", "suspended", v2.Status_RESOURCE_STATUS_DISABLED},
		{"unknown status, active account and membership", "something_new", "active", "active", v2.Status_RESOURCE_STATUS_ENABLED},
		{"unknown status, missing sub-statuses", "something_new", "", "", v2.Status_RESOURCE_STATUS_UNSPECIFIED},
		{"empty everything", "", "", "", v2.Status_RESOURCE_STATUS_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := atlassianclient.User{
				AccountId:        "acc-1",
				AccountType:      "atlassian",
				Name:             "Test User",
				Email:            "test@example.com",
				Status:           tt.status,
				AccountStatus:    tt.accountStatus,
				MembershipStatus: tt.membershipStatus,
			}

			resource, err := parseIntoUserResource(user)
			if err != nil {
				t.Fatalf("parseIntoUserResource returned error: %v", err)
			}

			if got := resource.GetStatus().GetStatus(); got != tt.want {
				t.Errorf("status = %v, want %v", got, tt.want)
			}
			if got := resource.GetStatus().GetDetails(); got != tt.status {
				t.Errorf("status details = %q, want %q", got, tt.status)
			}

			profile := resource.GetProfile().AsMap()
			for key, want := range map[string]string{
				"status":            tt.status,
				"account_status":    tt.accountStatus,
				"membership_status": tt.membershipStatus,
			} {
				if got := profile[key]; got != want {
					t.Errorf("profile[%q] = %v, want %q", key, got, want)
				}
			}
		})
	}
}
