package connector

import (
	"testing"
)

func TestGenerateIssueURL(t *testing.T) {
	tests := []struct {
		name        string
		clientURL   string
		originalURL string
		want        string
	}{
		{
			name:      "classic token uses client base URL",
			clientURL: "https://example.atlassian.net",
			want:      "https://example.atlassian.net/browse/PROJ-1",
		},
		{
			name:        "scoped token fallback uses original site URL",
			clientURL:   "https://api.atlassian.com/ex/jira/11111111-2222-3333-4444-555555555555",
			originalURL: "https://example.atlassian.net",
			want:        "https://example.atlassian.net/browse/PROJ-1",
		},
		{
			name:        "original site URL with trailing slash",
			clientURL:   "https://api.atlassian.com/ex/jira/11111111-2222-3333-4444-555555555555",
			originalURL: "https://example.atlassian.net/",
			want:        "https://example.atlassian.net/browse/PROJ-1",
		},
		{
			name:        "original URL already scoped falls back to client base URL",
			clientURL:   "https://api.atlassian.com/ex/jira/11111111-2222-3333-4444-555555555555",
			originalURL: "https://api.atlassian.com/ex/jira/11111111-2222-3333-4444-555555555555",
			want:        "https://api.atlassian.com/browse/PROJ-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := newTestJira(t, tt.clientURL)
			j.originalURL = tt.originalURL

			got, err := j.generateIssueURL("PROJ-1")
			if err != nil {
				t.Fatalf("generateIssueURL returned error: %v", err)
			}
			if got != tt.want {
				t.Errorf("generateIssueURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
