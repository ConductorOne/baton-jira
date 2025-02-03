package connector

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	jira "github.com/conductorone/go-jira/v2/cloud"
)

func wrapError(err error, message string) error {
	return fmt.Errorf("jira-connector: %s: %w", message, err)
}

func parsePageToken(i string, resourceID *v2.ResourceId) (*pagination.Bag, int64, error) {
	b := &pagination.Bag{}
	err := b.Unmarshal(i)
	if err != nil {
		return nil, 0, err
	}

	if b.Current() == nil {
		b.Push(pagination.PageState{
			ResourceTypeID: resourceID.ResourceType,
			ResourceID:     resourceID.Resource,
		})
	}

	offset, err := getOffsetFromPageToken(b.PageToken())
	if err != nil {
		return nil, 0, err
	}

	return b, offset, nil
}

func getOffsetFromPageToken(token string) (int64, error) {
	if token == "" {
		return 0, nil
	}

	page, err := strconv.ParseInt(token, 10, 64)
	if err != nil {
		return 0, err
	}

	return page, nil
}

func isLastPage(count int, pageSize int) bool {
	return count < pageSize
}

func getPageTokenFromOffset(bag *pagination.Bag, offset int64) (string, error) {
	nextPage := fmt.Sprintf("%d", offset)
	pageToken, err := bag.NextToken(nextPage)
	if err != nil {
		return "", err
	}

	return pageToken, nil
}

var RoleIDNotFoundErr = fmt.Errorf("role id not found in role link")

// Unfortunatelly, the Jira API does not provide a way to get the role id from project.
// It only provides a link to the role. Like this: https://your-domain.atlassian.net/rest/api/3/project/10001/role/10002
// So, we need to parse the role id from the link.
func parseRoleIdFromRoleLink(roleLink string) (int, error) {
	// Parse the URL
	parsedURL, err := url.Parse(roleLink)
	if err != nil {
		return 0, fmt.Errorf("failed to parse URL: %w", err)
	}

	// Split the path of the URL
	pathElems := strings.Split(parsedURL.Path, "/")
	// Find the index of the "role" element in the path, the next element should be the role id
	roleIndex := slices.Index(pathElems, "role")
	if roleIndex == -1 || roleIndex+1 >= len(pathElems) {
		return 0, RoleIDNotFoundErr
	}
	regexPattern := `\d+` // Regex pattern to match any number in the URL path
	r := regexp.MustCompile(regexPattern)
	matches := r.FindStringSubmatch(pathElems[roleIndex+1])
	// If there are no matches, return an error
	if len(matches) == 0 {
		return 0, RoleIDNotFoundErr
	}
	roleID, err := strconv.Atoi(matches[0])
	if err != nil {
		return 0, fmt.Errorf("failed to parse role id: %w", err)
	}
	return roleID, nil
}

func projectRoleID(project *jira.Project, role *jira.Role) string {
	return fmt.Sprintf("%s:%d", project.ID, role.ID)
}

func parseProjectRoleID(resourceID string) (string, int, error) {
	parts := strings.SplitN(resourceID, ":", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid resource ID")
	}

	projectID := parts[0]
	roleID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("invalid role ID")
	}

	return projectID, int(roleID), nil
}
