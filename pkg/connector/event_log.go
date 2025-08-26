package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/conductorone/baton-jira/pkg/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	defaultPageSize = 100
)

// auditPageToken handles pagination state for audit log requests.
type auditPageToken struct {
	From        string `json:"from"`         // From time for the current filter.
	FilterIndex int    `json:"filter_index"` // Index of current filter being processed.
	Offset      int    `json:"offset"`       // Offset for the current filter.
}

// marshal converts the page token to a string for pagination.
func (t *auditPageToken) marshal() (string, error) {
	b, err := json.Marshal(t)
	if err != nil {
		return "", fmt.Errorf("failed to marshal page token: %w", err)
	}
	return string(b), nil
}

// unmarshal restores the page token from a string.
func (t *auditPageToken) unmarshal(token string) error {
	if err := json.Unmarshal([]byte(token), t); err != nil {
		return fmt.Errorf("failed to unmarshal page token: %w", err)
	}
	return nil
}

// ListEvents retrieves and converts Jira audit logs into standardized events.
func (c *Jira) ListEvents(
	ctx context.Context,
	earliestEvent *timestamppb.Timestamp,
	pageToken *pagination.StreamToken,
) ([]*v2.Event, *pagination.StreamState, annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	// Initialize or restore pagination token.
	token := &auditPageToken{From: "", FilterIndex: 0, Offset: 0}
	if pageToken != nil && pageToken.Cursor != "" {
		if err := token.unmarshal(pageToken.Cursor); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to unmarshal page token: %w", err)
		}
	}
	if token.From == "" {
		token.From = earliestEvent.AsTime().Format(time.RFC3339)
		token.FilterIndex = 0
		token.Offset = 0
	}

	var events []*v2.Event

	auditResp, _, err := c.client.Jira().Audit.Get(ctx, &client.AuditOptions{
		From:   token.From,
		Offset: token.Offset,
		Limit:  defaultPageSize,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get audit records: %w", err)
	}

	// Convert records to events.
	for _, record := range auditResp.Records {
		if record.AuthorAccountId == "" {
			continue // Skip records without author.
		}
		if record.ObjectItem.ID == "" {
			l.Debug("skipping record without a target resource", zap.Int64("record_id", record.ID), zap.String("category", record.Category), zap.String("description", record.Description))
			continue // Skip records without a target resource.
		}

		event, err := c.parseIntoUsageEvent(&record)
		if err != nil {
			l.Error("failed to convert audit record to event",
				zap.Error(err),
				zap.Int64("record_id", record.ID))
			continue
		}
		events = append(events, event)
	}

	count := len(auditResp.Records)
	token.Offset += count
	hasMore := count > 0

	l.Debug("list events", zap.String("from", token.From), zap.Int("filter_index", token.FilterIndex), zap.Int("offset", token.Offset),
		zap.Int("total", int(auditResp.Total)), zap.Bool("has_more", hasMore))

	// Prepare next page token if there are more events to process.
	var nextToken string
	if hasMore {
		tokenStr, err := token.marshal()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to create next page token: %w", err)
		}
		nextToken = tokenStr
	}

	return events, &pagination.StreamState{
		Cursor:  nextToken,
		HasMore: hasMore,
	}, nil, nil
}

// convertToEvent transforms a Jira audit record into a standardized event format.
func (c *Jira) parseIntoUsageEvent(record *client.AuditRecord) (*v2.Event, error) {
	resourceType := c.determineResourceType(record.ObjectItem.TypeName)

	metadata := map[string]interface{}{
		"category":       record.Category,
		"summary":        record.Summary,
		"remote_address": record.RemoteAddress,
	}
	c.enrichMetadata(metadata, record)

	usageEvent := &v2.Event_UsageEvent{
		UsageEvent: &v2.UsageEvent{
			TargetResource: &v2.Resource{
				Id: &v2.ResourceId{
					ResourceType: resourceType.Id,
					Resource:     record.ObjectItem.ID,
				},
				DisplayName: record.ObjectItem.Name,
			},
			ActorResource: &v2.Resource{
				Id: &v2.ResourceId{
					ResourceType: resourceTypeUser.Id,
					Resource:     record.AuthorAccountId,
				},
			},
		},
	}

	return &v2.Event{
		Id:         strconv.FormatInt(record.ID, 10),
		OccurredAt: timestamppb.New(record.Created.Time),
		Event:      usageEvent,
	}, nil
}

// determineResourceType maps Jira resource types to standardized types.
func (c *Jira) determineResourceType(typeName string) *v2.ResourceType {
	switch typeName {
	case "USER":
		return resourceTypeUser
	case "GROUP":
		return resourceTypeGroup
	case "PROJECT":
		return resourceTypeProject
	case "PROJECT_ROLE":
		return resourceTypeProjectRole
	default:
		return &v2.ResourceType{
			Id:          typeName,
			DisplayName: typeName,
		}
	}
}

// enrichMetadata adds optional fields to the event metadata if they exist.
func (c *Jira) enrichMetadata(metadata map[string]interface{}, record *client.AuditRecord) {
	if len(record.ChangedValues) > 0 {
		metadata["changes"] = record.ChangedValues
	}
	if len(record.AssociatedItems) > 0 {
		metadata["associated_items"] = record.AssociatedItems
	}
	if record.Description != "" {
		metadata["description"] = record.Description
	}
}
