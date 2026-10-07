package stele

import (
	"context"
	"fmt"
	"strings"

	"github.com/FelixSeptem/danny/internal/runtimeprobe"
)

type ScheduledMemoryRequest struct {
	OccurrenceID string
	EffectKey    string
	ExecutionID  string
	Content      string
}

// ScheduledMemory uses the existing public Stele provider/events contract.
// The occurrence effect key is sent in both metadata and the HTTP
// Idempotency-Key header by Client.request; no Danny-side deduplication is
// performed.
func (c *Client) ScheduledMemory(ctx context.Context, req ScheduledMemoryRequest) (runtimeprobe.ProbeOutcome, error) {
	if strings.TrimSpace(req.OccurrenceID) == "" || strings.TrimSpace(req.EffectKey) == "" || strings.TrimSpace(req.ExecutionID) == "" {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("scheduled memory requires occurrence, effect, and execution identities")
	}
	return c.Memory(ctx, runtimeprobe.ProbeRequest{CorrelationID: req.EffectKey, ParentID: req.ExecutionID, Capability: "scheduled_memory", Input: req.Content})
}
