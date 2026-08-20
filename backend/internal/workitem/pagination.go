package workitem

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ListResult struct {
	Items      []*WorkItem
	NextCursor string
}

type workItemCursor struct {
	UpdatedAt   time.Time `json:"updated_at"`
	BacklogRank int64     `json:"backlog_rank,omitempty"`
	ID          string    `json:"id"`
	Sort        string    `json:"sort,omitempty"`
}

func encodeWorkItemCursor(item *WorkItem, sort string) (string, error) {
	if item == nil || item.ID == "" {
		return "", nil
	}
	raw, err := json.Marshal(workItemCursor{UpdatedAt: item.UpdatedAt.UTC(), BacklogRank: item.BacklogRank, ID: item.ID, Sort: sort})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeWorkItemCursor(value string) (workItemCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return workItemCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return workItemCursor{}, fmt.Errorf("invalid cursor")
	}
	var cursor workItemCursor
	if err := json.Unmarshal(raw, &cursor); err != nil || cursor.ID == "" || (cursor.UpdatedAt.IsZero() && cursor.Sort != "backlog") {
		return workItemCursor{}, fmt.Errorf("invalid cursor")
	}
	return cursor, nil
}
