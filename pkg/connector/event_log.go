package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

type auditPageToken struct {
	Offset int `json:"offset"`
}

func (t *auditPageToken) marshal() (string, error) {
	b, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (t *auditPageToken) unmarshal(token string) error {
	return json.Unmarshal([]byte(token), t)
}

func (c *Jira) ListEvents(
	ctx context.Context,
	earliestEvent *timestamppb.Timestamp,
	pToken *pagination.StreamToken,
) ([]*v2.Event, *pagination.StreamState, annotations.Annotations, error) {
	const pageSize = 100
	l := ctxzap.Extract(ctx)

	token := &auditPageToken{}
	if pToken != nil && pToken.Cursor != "" {
		if err := token.unmarshal(pToken.Cursor); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to unmarshal token: %w", err)
		}
	}

	var fromTime time.Time
	if earliestEvent != nil {
		fromTime = earliestEvent.AsTime()
	}

	auditResp, resp, err := c.client.Jira().Audit.Get(ctx, fromTime, token.Offset, pageSize)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := time.Now().Add(time.Minute)
			annos := annotations.Annotations{}
			annos.Update(&v2.RateLimitDescription{
				Limit:   1000,
				ResetAt: timestamppb.New(retryAfter),
			})
			return nil, nil, annos, nil
		}
		return nil, nil, nil, fmt.Errorf("failed to get audit logs: %w", err)
	}

	var events []*v2.Event
	for _, record := range auditResp.Records {
		if record.AuthorAccountId == "" {
			continue
		}
		event, err := c.createJiraEvent(&record)
		if err != nil {
			l.Error("failed to create event", zap.Error(err), zap.Int64("record_id", record.ID))
			continue
		}
		events = append(events, event)
	}

	nextOffset := token.Offset + pageSize
	hasMore := int64(nextOffset) < auditResp.Total

	var nextToken string
	if hasMore {
		nextToken, err = (&auditPageToken{Offset: nextOffset}).marshal()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to marshal next token: %w", err)
		}
	}

	return events, &pagination.StreamState{
		Cursor:  nextToken,
		HasMore: hasMore,
	}, nil, nil
}

func (c *Jira) createJiraEvent(record *client.AuditRecord) (*v2.Event, error) {
	var resourceType *v2.ResourceType
	switch record.ObjectItem.TypeName {
	case "USER":
		resourceType = resourceTypeUser
	case "GROUP":
		resourceType = resourceTypeGroup
	case "PROJECT":
		resourceType = resourceTypeProject
	case "PROJECT_ROLE":
		resourceType = resourceTypeProjectRole
	default:
		resourceType = &v2.ResourceType{
			Id:          record.ObjectItem.TypeName,
			DisplayName: record.ObjectItem.TypeName,
		}
	}

	metadata := map[string]interface{}{
		"category":       record.Category,
		"summary":        record.Summary,
		"remote_address": record.RemoteAddress,
	}

	if len(record.ChangedValues) > 0 {
		metadata["changes"] = record.ChangedValues
	}
	if len(record.AssociatedItems) > 0 {
		metadata["associated_items"] = record.AssociatedItems
	}
	if record.Description != "" {
		metadata["description"] = record.Description
	}

	usageEvent := &v2.UsageEvent{
		TargetResource: &v2.Resource{
			Id: &v2.ResourceId{
				ResourceType: "user",
				Resource:     record.AuthorAccountId,
			},
			DisplayName: record.AuthorKey,
		},
		ActorResource: &v2.Resource{
			Id: &v2.ResourceId{
				ResourceType: resourceType.Id,
				Resource:     record.ObjectItem.ID,
			},
			DisplayName: record.ObjectItem.Name,
		},
	}

	return &v2.Event{
		Id:         strconv.FormatInt(record.ID, 10),
		OccurredAt: timestamppb.New(record.Created.Time),
		Event: &v2.Event_UsageEvent{
			UsageEvent: usageEvent,
		},
	}, nil
}
