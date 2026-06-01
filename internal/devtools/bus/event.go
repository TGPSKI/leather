// Package bus provides an in-memory event bus for DevTools consumers.
package bus

import (
	"encoding/json"
)

// Event is the canonical DevTools event envelope.
type Event struct {
	Seq        uint64          `json:"seq"`
	At         int64           `json:"at"`
	Kind       string          `json:"kind"`
	Source     string          `json:"source,omitempty"`
	EntityKind string          `json:"entity_kind,omitempty"`
	EntityID   string          `json:"entity_id,omitempty"`
	ParentSeq  uint64          `json:"parent_seq,omitempty"`
	CausesSeq  []uint64        `json:"causes_seq,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	Err        string          `json:"err,omitempty"`
}

// Stats reports current bus capacity and runtime counters.
type Stats struct {
	Capacity    int    `json:"capacity"`
	Size        int    `json:"size"`
	OldestSeq   uint64 `json:"oldest_seq"`
	NewestSeq   uint64 `json:"newest_seq"`
	Published   uint64 `json:"published"`
	Evicted     uint64 `json:"evicted"`
	Dropped     uint64 `json:"dropped"`
	Subscribers int    `json:"subscribers"`
}
