package decisionrecord

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	// ErrReplay marks a record ID or correlation ID that was already consumed.
	ErrReplay = errors.New("decision record: replay detected")
	// ErrReplayStore marks a missing, invalid, or unavailable replay store.
	ErrReplayStore = errors.New("decision record: replay store failed")
)

// ReplayClaim is the signed identity material a ReplayStore consumes.
type ReplayClaim struct {
	Domain        string `json:"domain"`
	Subject       string `json:"subject"`
	CorrelationID string `json:"correlation_id"`
	RecordID      string `json:"record_id"`
	RecordHash    string `json:"record_hash"`
}

// ReplayStore atomically consumes both (domain, record ID) and (domain,
// correlation ID). It returns ErrReplay if either identity was consumed and
// leaves no partial claim on any failure.
type ReplayStore interface {
	Consume(context.Context, ReplayClaim) error
}

// MemoryReplayStore is a process-local reference implementation. It is safe
// for concurrent use but is not durable and must not back a multi-process
// production authorization path. It must not be copied after first use.
type MemoryReplayStore struct {
	mu           sync.Mutex
	records      map[replayRecordKey]struct{}
	correlations map[replayCorrelationKey]struct{}
}

type replayRecordKey struct {
	domain   string
	recordID string
}

type replayCorrelationKey struct {
	domain        string
	correlationID string
}

// NewMemoryReplayStore constructs an empty process-local replay store.
func NewMemoryReplayStore() *MemoryReplayStore {
	return &MemoryReplayStore{
		records:      make(map[replayRecordKey]struct{}),
		correlations: make(map[replayCorrelationKey]struct{}),
	}
}

// Consume atomically records a claim or fails if either replay identity exists.
func (store *MemoryReplayStore) Consume(ctx context.Context, claim ReplayClaim) error {
	if store == nil {
		return replayStoreError("memory store is nil")
	}
	if ctx == nil {
		return replayStoreError("context is nil")
	}
	if err := ctx.Err(); err != nil {
		return replayStoreError("context: %v", err)
	}
	if claim.Domain == "" || claim.Subject == "" || claim.CorrelationID == "" || claim.RecordID == "" || claim.RecordHash == "" {
		return replayStoreError("claim is incomplete")
	}

	recordKey := replayRecordKey{domain: claim.Domain, recordID: claim.RecordID}
	correlationKey := replayCorrelationKey{domain: claim.Domain, correlationID: claim.CorrelationID}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return replayStoreError("context: %v", err)
	}
	if _, exists := store.records[recordKey]; exists {
		return replayError("record_id %q was already consumed", claim.RecordID)
	}
	if _, exists := store.correlations[correlationKey]; exists {
		return replayError("correlation_id %q was already consumed", claim.CorrelationID)
	}
	if store.records == nil {
		store.records = make(map[replayRecordKey]struct{})
	}
	if store.correlations == nil {
		store.correlations = make(map[replayCorrelationKey]struct{})
	}
	store.records[recordKey] = struct{}{}
	store.correlations[correlationKey] = struct{}{}
	return nil
}

func replayError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrReplay, fmt.Sprintf(format, args...))
}

func replayStoreError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrReplayStore, fmt.Sprintf(format, args...))
}
