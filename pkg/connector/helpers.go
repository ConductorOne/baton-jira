package connector

import (
	"fmt"
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

	page, err := convertPageToken(b.PageToken())
	if err != nil {
		return nil, 0, err
	}

	return b, page, nil
}

func convertPageToken(token string) (int64, error) {
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

func handleNextPage(bag *pagination.Bag, page int64) (string, error) {
	nextPage := fmt.Sprintf("%d", page)
	pageToken, err := bag.NextToken(nextPage)
	if err != nil {
		return "", err
	}

	return pageToken, nil
}
