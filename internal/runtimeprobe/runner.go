package runtimeprobe

import (
	"context"
	"fmt"
	"time"
)

// OfflineRunner exercises only deterministic adapter paths. It never creates a
// network client by itself; a nil Stele adapter is represented explicitly as a
// skipped capability in the evidence bundle.
type OfflineRunner struct {
	Baymax BaymaxAdapter
	Stele  SteleAdapter
}

func (r OfflineRunner) Run(ctx context.Context, input string) (Bundle, error) {
	if r.Baymax == nil {
		return Bundle{}, fmt.Errorf("baymax adapter is required")
	}
	events := make([]Event, 0, 8)
	for _, item := range []struct {
		name string
		call func(context.Context, ProbeRequest) (ProbeOutcome, error)
	}{
		{"run", r.Baymax.Run},
		{"stream", r.Baymax.Stream},
	} {
		corr := "offline:" + item.name
		out, err := item.call(ctx, ProbeRequest{CorrelationID: corr, Input: input, Capability: item.name})
		if err != nil {
			return Bundle{}, fmt.Errorf("%s probe: %w", item.name, err)
		}
		events = append(events, out.Events...)
	}
	if r.Stele != nil {
		out, err := r.Stele.Context(ctx, ProbeRequest{CorrelationID: "offline:stele", ParentID: "offline:stele:parent", Input: input, Capability: "context"})
		if err != nil {
			return Bundle{}, fmt.Errorf("stele context probe: %w", err)
		}
		events = append(events, out.Events...)
	} else {
		now := time.Unix(0, 0).UTC()
		events = append(events,
			Event{Sequence: 1, ID: "offline:stele:parent", Kind: KindParent, CorrelationID: "offline:stele", Capability: "context", Status: StatusSkippedUnconfigured, At: now},
			Event{Sequence: 2, ID: "offline:stele:terminal", Kind: KindTerminal, ParentID: "offline:stele:parent", CorrelationID: "offline:stele", Capability: "context", Status: StatusSkippedUnconfigured, At: now.Add(time.Nanosecond)},
		)
	}
	for index := range events {
		events[index].Sequence = int64(index + 1)
	}
	bundle := Bundle{SchemaVersion: EvidenceSchemaVersion, BaymaxVersion: r.Baymax.Version(), SteleVersion: "unconfigured", Events: events}
	if r.Stele != nil {
		bundle.SteleVersion = r.Stele.Version()
	}
	return bundle, bundle.Validate()
}
