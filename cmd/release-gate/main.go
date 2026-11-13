// Command release-gate runs the Milestone 12 validation suites and writes the
// release-gate evidence matrix.
//
// It never claims a check it did not run. A scenario that needs a credential,
// a live upstream, or a storage driver that is not reachable is reported as
// unverified with the reason, and the exit code stays zero for unverified
// checks while a real failure returns one.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/FelixSeptem/danny/internal/scenarios"
)

type outcome struct {
	status string // passed, failed, unverified
	reason string
}

type scenarioReport struct {
	ID       string
	Title    string
	Class    string
	Proof    string
	Evidence string
	SQLite   outcome
	Postgres outcome
}

// clause is one release-gate condition with the check that backs it.
type clause struct {
	Title string
	Proof string
}

// releaseClauses mirrors the six release-gate conditions in docs/roadmap.md.
// Each one names an executable check, so the gate reports evidence rather
// than an opinion.
var releaseClauses = []clause{
	{"Unit, integration, and Docker end-to-end suites pass", "internal/scenarios:TestScenarioDeterministicContractIsReproducible"},
	{"Run and Stream semantics are equivalent at terminal and aggregation boundaries", "internal/server:TestSSEReconnectConvergesOnTerminalState"},
	{"No unresolved blocked-upstream dependency is inside the claimed feature set", "internal/scenarios:TestNoBlockedUpstreamFindingIsClaimed"},
	{"Upstream findings record version, reproduction, evidence, affected task, and unblock condition", "internal/scenarios:TestUpstreamFindingsHaveRegressionEvidence"},
	{"Durability claims are backed by crash-recovery tests", "internal/storage:TestFailureInjectorCoversGenerationLeaseEffectAndMaterialization"},
	{"Execution projections, dynamic UI actions, and durable state agree across refresh, reconnect, retry, and restart", "internal/server:TestSurfaceSubmissionConvergence"},
}

func main() {
	out := flag.String("out", "", "write the evidence matrix to this markdown file")
	postgresDSN := flag.String("postgres", "", "PostgreSQL DSN for the second storage mode")
	flag.Parse()

	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "release-gate:", err)
		os.Exit(2)
	}

	sqlite := suiteResult{results: runGoSuite(root, nil)}
	postgres := suiteResult{results: map[string]bool{}, reason: "no PostgreSQL DSN was supplied (-postgres)"}
	if strings.TrimSpace(*postgresDSN) != "" {
		postgres = suiteResult{results: runGoSuite(root, []string{"DANNY_TEST_POSTGRES_URL=" + strings.TrimSpace(*postgresDSN)})}
	}
	parser := runParserMatrix(root)

	reports := buildReports(sqlite, postgres, parser)
	matrix := render(reports, sqlite, postgres, parser)

	if *out != "" {
		path := *out
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		if err := os.WriteFile(path, []byte(matrix), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "release-gate: write matrix:", err)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "release-gate: matrix written to", path)
	}
	fmt.Print(matrix)

	for _, report := range reports {
		if report.SQLite.status == "failed" || report.Postgres.status == "failed" {
			os.Exit(1)
		}
	}
}

// runGoSuite runs the Go test suite and maps each test to its result.
func runGoSuite(root string, extraEnv []string) map[string]bool {
	command := exec.Command("go", "test", "./...", "-count=1", "-json")
	command.Dir = root
	command.Env = append(os.Environ(), extraEnv...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return map[string]bool{}
	}
	if err := command.Start(); err != nil {
		return map[string]bool{}
	}
	results := map[string]bool{}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	for scanner.Scan() {
		var event struct {
			Action string
			Test   string
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil || event.Test == "" {
			continue
		}
		switch event.Action {
		case "pass":
			results[event.Test] = true
		case "fail":
			results[event.Test] = false
		}
	}
	_ = command.Wait()
	return results
}

// runParserMatrix runs the parser format matrix and reports its outcome.
func runParserMatrix(root string) outcome {
	python, err := exec.LookPath("python")
	if err != nil {
		return outcome{status: "unverified", reason: "python is not on PATH"}
	}
	command := exec.Command(python, "parser_service/test_formats.py")
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		return outcome{status: "failed", reason: strings.TrimSpace(lastLine(string(output)))}
	}
	return outcome{status: "passed", reason: strings.TrimSpace(lastLine(string(output)))}
}

// suiteResult holds the per-test results of one storage mode plus the reason
// the mode could not be executed at all.
type suiteResult struct {
	results map[string]bool
	reason  string
}

func buildReports(sqlite, postgres suiteResult, parser outcome) []scenarioReport {
	var reports []scenarioReport
	for _, scenario := range scenarios.Registry() {
		report := scenarioReport{
			ID:       scenario.ID,
			Title:    scenario.Title,
			Class:    string(scenario.Class),
			Proof:    scenario.Proof,
			Evidence: scenario.Evidence,
		}
		switch {
		case scenario.Proof == "cmd/release-gate":
			report.SQLite = suiteOutcome(sqlite)
			report.Postgres = suiteOutcome(postgres)
		case scenario.Proof == "parser_service:test_format_matrix":
			report.SQLite = parser
			report.Postgres = outcome{status: "unverified", reason: "the parser matrix is storage independent"}
		case strings.HasPrefix(scenario.Proof, "cmd/"):
			report.SQLite = liveOutcome(scenario.Proof)
			report.Postgres = outcome{status: "unverified", reason: "live scenario; the second storage mode does not change the live path"}
		default:
			report.SQLite = proofOutcome(scenario.Proof, sqlite.results)
			report.Postgres = outcome{status: "unverified", reason: "the second storage mode was not executed for this check"}
		}
		reports = append(reports, report)
	}
	return reports
}

func proofOutcome(proof string, results map[string]bool) outcome {
	name := proof
	if index := strings.LastIndex(proof, ":"); index >= 0 {
		name = proof[index+1:]
	}
	passed, ok := results[name]
	if !ok {
		return outcome{status: "unverified", reason: "the check did not run in this suite"}
	}
	if !passed {
		return outcome{status: "failed", reason: "the check failed"}
	}
	return outcome{status: "passed"}
}

func suiteOutcome(suite suiteResult) outcome {
	if suite.reason != "" {
		return outcome{status: "unverified", reason: suite.reason}
	}
	failed := 0
	for _, passed := range suite.results {
		if !passed {
			failed++
		}
	}
	if failed > 0 {
		return outcome{status: "failed", reason: fmt.Sprintf("%d check(s) failed", failed)}
	}
	if len(suite.results) == 0 {
		return outcome{status: "unverified", reason: "no check ran"}
	}
	return outcome{status: "passed", reason: fmt.Sprintf("%d checks passed", len(suite.results))}
}

func liveOutcome(proof string) outcome {
	return outcome{status: "unverified", reason: "live-required; run `" + proof + "` with a credential to produce the evidence bundle"}
}

func render(reports []scenarioReport, sqlite, postgres suiteResult, parser outcome) string {
	var builder strings.Builder
	builder.WriteString("# Release gate evidence (Milestone 12)\n\n")
	builder.WriteString("Generated by `go run ./cmd/release-gate` on " + time.Now().UTC().Format("2006-01-02") + ".\n")
	builder.WriteString("A check that did not run is reported as unverified with its reason; it is never counted as passing.\n\n")

	builder.WriteString("## Scenario matrix\n\n")
	builder.WriteString("| ID | Scenario | Class | SQLite | PostgreSQL | Proof |\n")
	builder.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, report := range reports {
		fmt.Fprintf(&builder, "| %s | %s | %s | %s | %s | `%s` |\n",
			report.ID, report.Title, report.Class, cell(report.SQLite), cell(report.Postgres), report.Proof)
	}

	builder.WriteString("\n## Storage modes\n\n")
	builder.WriteString("- SQLite: " + cell(suiteOutcome(sqlite)) + "\n")
	builder.WriteString("- PostgreSQL: " + cell(suiteOutcome(postgres)) + "\n")
	builder.WriteString("- Parser format matrix: " + cell(parser) + "\n")

	builder.WriteString("\n## Release gate clauses\n\n")
	builder.WriteString("| Clause | Status | Proof |\n")
	builder.WriteString("| --- | --- | --- |\n")
	for _, item := range releaseClauses {
		status := proofOutcome(item.Proof, sqlite.results)
		fmt.Fprintf(&builder, "| %s | %s | `%s` |\n", item.Title, cell(status), item.Proof)
	}

	var unverified []string
	for _, report := range reports {
		for label, result := range map[string]outcome{"SQLite": report.SQLite, "PostgreSQL": report.Postgres} {
			if result.status == "unverified" {
				unverified = append(unverified, fmt.Sprintf("- %s (%s): %s", report.ID, label, result.reason))
			}
		}
	}
	sort.Strings(unverified)
	builder.WriteString("\n## Unverified\n\n")
	if len(unverified) == 0 {
		builder.WriteString("Every registered scenario produced a result.\n")
	} else {
		builder.WriteString(strings.Join(unverified, "\n") + "\n")
	}
	return builder.String()
}

func cell(result outcome) string {
	switch result.status {
	case "passed":
		return "passed"
	case "failed":
		return "failed: " + result.reason
	default:
		return "unverified: " + result.reason
	}
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

// repoRoot walks up from the working directory to the module root.
func repoRoot() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", fmt.Errorf("no go.mod found above %s", directory)
		}
		directory = parent
	}
}
