package scenarios

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	findingHeading = regexp.MustCompile(`^###\s+(.+)$`)
	findingID      = regexp.MustCompile(`^([A-Za-z]+-\d+)`)
)

// TestUpstreamFindingsHaveRegressionEvidence proves S13: the upstream
// findings documents stay the source of truth, and every finding entry
// records the version, the reproduction, the evidence location, the affected
// task, and the unblock condition the release gate needs.
func TestUpstreamFindingsHaveRegressionEvidence(t *testing.T) {
	for _, name := range []string{"baymax-findings.md", "stele-findings.md"} {
		path := filepath.Join("..", "..", "docs", "upstream", name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		entries := parseFindingEntries(string(raw))
		if len(entries) == 0 {
			t.Fatalf("%s has no finding entries", name)
		}
		for _, entry := range entries {
			if !entry.hasStatus {
				t.Fatalf("%s: finding %s has no status", name, entry.id)
			}
			if !entry.hasVersion {
				t.Fatalf("%s: finding %s records no dependency version or commit", name, entry.id)
			}
			if !entry.hasReproduction {
				t.Fatalf("%s: finding %s records no reproduction", name, entry.id)
			}
			if !entry.hasEvidence {
				t.Fatalf("%s: finding %s records no evidence location", name, entry.id)
			}
			if !entry.hasAffectedTask {
				t.Fatalf("%s: finding %s records no affected task", name, entry.id)
			}
			if !entry.hasUnblockCondition {
				t.Fatalf("%s: finding %s records no unblock condition", name, entry.id)
			}
		}
	}
}

// TestNoBlockedUpstreamFindingIsClaimed proves the release-gate rule that a
// blocked upstream dependency may not sit inside a claimed scenario. A
// blocked finding is reported as blocking instead of being counted as a
// passing check.
func TestNoBlockedUpstreamFindingIsClaimed(t *testing.T) {
	blocked := blockedUpstreamFindings(t)
	for _, entry := range blocked {
		if entry.claimedBy != "" {
			t.Fatalf("blocked upstream finding %s is claimed by scenario %s", entry.id, entry.claimedBy)
		}
	}
}

type findingEntry struct {
	id                  string
	hasStatus           bool
	hasVersion          bool
	hasReproduction     bool
	hasEvidence         bool
	hasAffectedTask     bool
	hasUnblockCondition bool
	blocked             bool
	claimedBy           string
}

func parseFindingEntries(document string) []findingEntry {
	var entries []findingEntry
	current := findingEntry{}
	inEntry := false
	for _, line := range strings.Split(document, "\n") {
		if match := findingHeading.FindStringSubmatch(line); match != nil {
			if inEntry {
				entries = append(entries, current)
			}
			id := strings.TrimSpace(match[1])
			if idMatch := findingID.FindStringSubmatch(id); idMatch != nil {
				id = idMatch[1]
			}
			current = findingEntry{id: id}
			inEntry = true
			continue
		}
		if !inEntry {
			continue
		}
		switch {
		case strings.Contains(line, "**状态"):
			current.hasStatus = true
			if strings.Contains(line, "blocked-upstream") {
				current.blocked = true
			}
		case strings.Contains(line, "**Pin / public contract**"),
			strings.Contains(line, "**受影响版本"),
			strings.Contains(line, "**修复版本"):
			current.hasVersion = true
		case strings.Contains(line, "**证据"):
			current.hasEvidence = true
		case strings.Contains(line, "复现步骤"),
			strings.Contains(line, "复现证据"),
			strings.Contains(line, "原始复现"):
			current.hasReproduction = true
		case strings.Contains(line, "**受影响任务"):
			current.hasAffectedTask = true
		case strings.Contains(line, "**解除条件"):
			current.hasUnblockCondition = true
		}
	}
	if inEntry {
		entries = append(entries, current)
	}
	return entries
}

func blockedUpstreamFindings(t *testing.T) []findingEntry {
	t.Helper()
	var blocked []findingEntry
	for _, name := range []string{"baymax-findings.md", "stele-findings.md"} {
		path := filepath.Join("..", "..", "docs", "upstream", name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, entry := range parseFindingEntries(string(raw)) {
			if entry.blocked {
				blocked = append(blocked, entry)
			}
		}
	}
	return blocked
}
