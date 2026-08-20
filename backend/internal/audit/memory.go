package audit

import (
	"context"
	"sort"
	"sync"
)

type MemoryWriter struct {
	mu      sync.Mutex
	records []Record
}

func NewMemoryWriter() *MemoryWriter { return &MemoryWriter{} }

func (w *MemoryWriter) Record(_ context.Context, record Record) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.records = append(w.records, record)
	return nil
}

func (w *MemoryWriter) Records() []Record {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]Record(nil), w.records...)
}

func (w *MemoryWriter) List(_ context.Context, organizationID string, filter Filter) ([]Record, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	result := make([]Record, 0)
	for _, record := range w.records {
		if record.OrganizationID != organizationID || (filter.ResourceType != "" && record.ResourceType != filter.ResourceType) || (filter.ResourceID != "" && record.ResourceID != filter.ResourceID) {
			continue
		}
		result = append(result, record)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

var _ Reader = (*MemoryWriter)(nil)
