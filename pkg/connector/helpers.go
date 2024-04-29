package connector

import (
	"fmt"
	"regexp"
	"strconv"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/pagination"
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

// Unfortunatelly, the Jira API does not provide a way to get the role id from project.
// It only provides a link to the role. Like this: https://your-domain.atlassian.net/rest/api/3/project/10001/role/10002
// So, we need to parse the role id from the link.
func parseRoleIdFromRoleLink(roleLink string) (int, error) {
	regexPattern := `\/(\d+)\/?$` // Regex pattern to match the last number in the URL path
	r := regexp.MustCompile(regexPattern)

	matches := r.FindStringSubmatch(roleLink)

	if len(matches) < 2 {
		return 0, fmt.Errorf("failed to parse role id from role link")
	}

	lastNumber, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, err
	}

	return lastNumber, nil
}
