package runtimeprobe

import (
	"testing"
	"time"
)

func TestEvidenceCorrelationCoversAllProbeKinds(t *testing.T) {
	kinds := []Kind{KindChild, KindTeam, KindMailbox, KindScheduler, KindContext, KindMemory}
	events := []Event{{Sequence: 1, ID: "parent", Kind: KindParent, CorrelationID: "corr", Status: StatusPassed, At: time.Unix(0, 0).UTC()}}
	for i, kind := range kinds {
		id := string(kind)
		events = append(events, Event{Sequence: int64(i + 2), ID: id, Kind: kind, ParentID: "parent", CorrelationID: "corr", Status: StatusPassed, At: time.Unix(int64(i+1), 0).UTC()})
	}
	events = append(events, Event{Sequence: int64(len(events) + 1), ID: "terminal", Kind: KindTerminal, ParentID: "memory", CorrelationID: "corr", Status: StatusPassed, At: time.Unix(10, 0).UTC()})
	bundle := Bundle{SchemaVersion: EvidenceSchemaVersion, BaymaxVersion: "baymax", SteleVersion: "stele", Events: events}
	if err := bundle.Validate(); err != nil {
		t.Fatal(err)
	}
}
