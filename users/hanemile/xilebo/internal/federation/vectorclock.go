package federation

import (
	"encoding/json"
	"fmt"
)

// VectorClock tracks causal ordering across federated servers.
// Maps server hostname -> logical counter.
type VectorClock map[string]uint64

// NewVectorClock creates an empty vector clock.
func NewVectorClock() VectorClock {
	return make(VectorClock)
}

// Increment advances the clock for a given server.
func (vc VectorClock) Increment(server string) {
	vc[server]++
}

// Get returns the counter for a server.
func (vc VectorClock) Get(server string) uint64 {
	return vc[server]
}

// Merge combines two vector clocks, taking the max of each entry.
func (vc VectorClock) Merge(other VectorClock) VectorClock {
	merged := make(VectorClock, len(vc))
	for k, v := range vc {
		merged[k] = v
	}
	for k, v := range other {
		if v > merged[k] {
			merged[k] = v
		}
	}
	return merged
}

// HappensBefore returns true if vc causally precedes other.
// vc < other iff all entries in vc are <= other AND at least one is strictly <.
func (vc VectorClock) HappensBefore(other VectorClock) bool {
	atLeastOneLess := false

	for k, v := range vc {
		ov := other[k]
		if v > ov {
			return false
		}
		if v < ov {
			atLeastOneLess = true
		}
	}

	// Check entries in other that aren't in vc
	for k, v := range other {
		if _, ok := vc[k]; !ok && v > 0 {
			atLeastOneLess = true
		}
	}

	return atLeastOneLess
}

// Concurrent returns true if neither clock happens before the other.
func (vc VectorClock) Concurrent(other VectorClock) bool {
	return !vc.HappensBefore(other) && !other.HappensBefore(vc)
}

// String returns a JSON representation.
func (vc VectorClock) String() string {
	data, _ := json.Marshal(vc)
	return string(data)
}

// ParseVectorClock parses a vector clock from JSON.
func ParseVectorClock(data string) (VectorClock, error) {
	if data == "" || data == "{}" {
		return NewVectorClock(), nil
	}
	var vc VectorClock
	if err := json.Unmarshal([]byte(data), &vc); err != nil {
		return nil, fmt.Errorf("parse vector clock: %w", err)
	}
	return vc, nil
}
