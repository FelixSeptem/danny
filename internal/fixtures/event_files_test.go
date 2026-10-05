package fixtures

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEventFixtureContainsCorrelatedEnvelopeKinds(t *testing.T) {
	file, err := os.Open(filepath.Join("..", "..", "testdata", "fixtures", "events", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	wantKinds := map[string]bool{"parent": false, "child": false, "team": false, "mailbox": false, "scheduler": false, "terminal": false}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event struct {
			EventID  string `json:"event_id"`
			Kind     string `json:"kind"`
			RunID    string `json:"run_id"`
			ParentID string `json:"parent_id"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		if event.EventID == "" || event.RunID == "" || event.Kind == "" {
			t.Fatalf("event missing required fields: %#v", event)
		}
		if event.Kind != "parent" && event.ParentID == "" {
			t.Fatalf("non-parent event missing parent_id: %#v", event)
		}
		if _, ok := wantKinds[event.Kind]; ok {
			wantKinds[event.Kind] = true
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	for kind, present := range wantKinds {
		if !present {
			t.Errorf("event kind %q is missing", kind)
		}
	}
}
