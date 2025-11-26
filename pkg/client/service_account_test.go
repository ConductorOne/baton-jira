package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsServiceAccount(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		expected bool
	}{
		{
			name:     "service account email",
			email:    "my-service-account-all-x1axxw94zz@serviceaccount.atlassian.com",
			expected: true,
		},
		{
			name:     "regular user email",
			email:    "user@company.com",
			expected: false,
		},
		{
			name:     "admin email",
			email:    "admin@atlassian.com",
			expected: false,
		},
		{
			name:     "empty email",
			email:    "",
			expected: false,
		},
		{
			name:     "service account with extra characters",
			email:    "test123@serviceaccount.atlassian.com",
			expected: true,
		},
		{
			name:     "almost service account but different domain",
			email:    "test@serviceaccount.atlassian.org",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isServiceAccount(tt.email)
			if result != tt.expected {
				t.Errorf("isServiceAccount(%s) = %v, want %v", tt.email, result, tt.expected)
			}
		})
	}
}

func TestResolveCloudID(t *testing.T) {
	tests := []struct {
		name            string
		setupServer     func() *httptest.Server
		jiraURL         string
		expectedCloudID string
		expectError     bool
		errorContains   string
	}{
		{
			name: "successful cloud ID resolution",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/_edge/tenant_info" {
						t.Errorf("Expected path /_edge/tenant_info, got %s", r.URL.Path)
					}
					response := tenantInfo{CloudID: "test-cloud-id-123"}
					_ = json.NewEncoder(w).Encode(response)
				}))
			},
			expectedCloudID: "test-cloud-id-123",
			expectError:     false,
		},
		{
			name: "tenant info endpoint returns 404",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			expectError:   true,
			errorContains: "returned status 404",
		},
		{
			name: "tenant info endpoint returns invalid JSON",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte("invalid json"))
				}))
			},
			expectError:   true,
			errorContains: "failed to decode",
		},
		{
			name: "tenant info endpoint returns empty cloud ID",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					response := tenantInfo{CloudID: ""}
					_ = json.NewEncoder(w).Encode(response)
				}))
			},
			expectError:   true,
			errorContains: "cloudId field not found or empty",
		},
		{
			name:          "empty jira URL",
			jiraURL:       "",
			expectError:   true,
			errorContains: "jira URL cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var testJiraURL string

			if tt.setupServer != nil {
				server := tt.setupServer()
				defer server.Close()
				testJiraURL = server.URL
			} else {
				testJiraURL = tt.jiraURL
			}

			ctx := context.Background()
			cloudID, err := resolveCloudID(ctx, testJiraURL, nil)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, but got nil")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, but got %q", tt.errorContains, err.Error())
				}
				if cloudID != "" {
					t.Errorf("expected empty cloudID, but got %q", cloudID)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, but got %v", err)
				}
				if cloudID != tt.expectedCloudID {
					t.Errorf("expected cloudID %q, but got %q", tt.expectedCloudID, cloudID)
				}
			}
		})
	}
}

func TestResolveURL(t *testing.T) {
	tests := []struct {
		name          string
		email         string
		jiraURL       string
		setupServer   func() *httptest.Server
		expectedURL   string
		expectError   bool
		errorContains string
	}{
		{
			name:        "regular account uses original URL",
			email:       "user@company.com",
			jiraURL:     "https://company.atlassian.net",
			expectedURL: "https://company.atlassian.net",
			expectError: false,
		},
		{
			name:  "service account resolves cloud ID and constructs API URL",
			email: "test@serviceaccount.atlassian.com",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					response := tenantInfo{CloudID: "abc123-def456-ghi789"}
					_ = json.NewEncoder(w).Encode(response)
				}))
			},
			expectedURL: "https://api.atlassian.com/ex/jira/abc123-def456-ghi789",
			expectError: false,
		},
		{
			name:          "empty email",
			email:         "",
			jiraURL:       "https://company.atlassian.net",
			expectError:   true,
			errorContains: "email cannot be empty",
		},
		{
			name:          "empty jira URL",
			email:         "user@company.com",
			jiraURL:       "",
			expectError:   true,
			errorContains: "jira URL cannot be empty",
		},
		{
			name:  "service account with tenant info failure",
			email: "test@serviceaccount.atlassian.com",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
			},
			expectError:   true,
			errorContains: "failed to resolve cloud ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var testJiraURL string

			if tt.setupServer != nil {
				server := tt.setupServer()
				defer server.Close()
				testJiraURL = server.URL
			} else {
				testJiraURL = tt.jiraURL
			}

			ctx := context.Background()
			resolvedURL, err := ResolveURL(ctx, tt.email, testJiraURL, nil)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, but got nil")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, but got %q", tt.errorContains, err.Error())
				}
				if resolvedURL != "" {
					t.Errorf("expected empty resolvedURL, but got %q", resolvedURL)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, but got %v", err)
				}
				if resolvedURL != tt.expectedURL {
					t.Errorf("expected resolvedURL %q, but got %q", tt.expectedURL, resolvedURL)
				}
			}
		})
	}
}

func TestResolveURLJiraURLWithTrailingSlash(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_edge/tenant_info" {
			t.Errorf("Expected path /_edge/tenant_info, got %s", r.URL.Path)
		}
		response := tenantInfo{CloudID: "test-cloud-id"}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	ctx := context.Background()

	resolvedURL, err := ResolveURL(ctx, "test@serviceaccount.atlassian.com", server.URL+"/", nil)
	if err != nil {
		t.Errorf("expected no error, but got %v", err)
	}
	expectedURL := "https://api.atlassian.com/ex/jira/test-cloud-id"
	if resolvedURL != expectedURL {
		t.Errorf("expected resolvedURL %q, but got %q", expectedURL, resolvedURL)
	}
}
