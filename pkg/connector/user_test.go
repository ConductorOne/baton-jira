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
		{"unknown status, unrecognised membership status", "unknown", "active", "pending", v2.Status_RESOURCE_STATUS_DISABLED},
		{"empty status, active account and membership", "", "active", "active", v2.Status_RESOURCE_STATUS_ENABLED},
		{"empty everything", "", "", "", v2.Status_RESOURCE_STATUS_UNSPECIFIED},
	}

	// Unknown composite status: every accountStatus x membershipStatus combination.
	const (
		enabled     = v2.Status_RESOURCE_STATUS_ENABLED
		disabled    = v2.Status_RESOURCE_STATUS_DISABLED
		unspecified = v2.Status_RESOURCE_STATUS_UNSPECIFIED
	)
	membershipStatuses := []string{"active", "suspended", "no_membership", ""}
	fallbackMatrix := []struct {
		accountStatus string
		want          []v2.Status_ResourceStatus // indexed like membershipStatuses
	}{
		{"active", []v2.Status_ResourceStatus{enabled, disabled, disabled, unspecified}},
		{"inactive", []v2.Status_ResourceStatus{disabled, disabled, disabled, disabled}},
		{"closed", []v2.Status_ResourceStatus{disabled, disabled, disabled, disabled}},
		{"", []v2.Status_ResourceStatus{unspecified, disabled, disabled, unspecified}},
	}
	for _, row := range fallbackMatrix {
		for i, membershipStatus := range membershipStatuses {
			tests = append(tests, struct {
				name             string
				status           string
				accountStatus    string
				membershipStatus string
				want             v2.Status_ResourceStatus
			}{
				name:             "unknown status, account=" + orMissing(row.accountStatus) + ", membership=" + orMissing(membershipStatus),
				status:           "unknown",
				accountStatus:    row.accountStatus,
				membershipStatus: membershipStatus,
				want:             row.want[i],
			})
		}
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

func orMissing(s string) string {
	if s == "" {
		return "missing"
	}
	return s
}
