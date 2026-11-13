// Package scenarios owns the Milestone 12 scenario registry: one entry per
// required roadmap scenario, with the classification that says how it is
// proved and where the evidence lives.
//
// The registry is the single source the release gate reads, so the gate
// cannot drift away from the roadmap. A scenario that cannot be executed is
// still registered with its reason; it is never dropped.
package scenarios

// Class describes how a scenario is proved.
type Class string

const (
	// ClassExecuted means a deterministic check in this repository proves it,
	// with no credential and no upstream service.
	ClassExecuted Class = "executed"
	// ClassLiveRequired means the scenario needs a real provider credential,
	// a live Stele deployment, or a live Baymax endpoint. It is never
	// reported as passing without real evidence.
	ClassLiveRequired Class = "live-required"
	// ClassBlockedUpstream means an upstream contract the scenario depends on
	// is not available yet.
	ClassBlockedUpstream Class = "blocked-upstream"
	// ClassNotExecutable means the scenario cannot be automated as written.
	ClassNotExecutable Class = "not-executable"
)

// Scenario is one required roadmap scenario.
type Scenario struct {
	// ID is the stable identifier, S01 through S16.
	ID string
	// Title is the roadmap scenario in short form.
	Title string
	// Class is how the scenario is proved.
	Class Class
	// Proof names the executable check, or the command that proves a
	// live-required scenario.
	Proof string
	// Evidence is where the proof is recorded.
	Evidence string
	// Reason explains the classification and, for anything that is not
	// executed, why.
	Reason string
}

// RequiredIDs lists every required roadmap scenario. The registry invariant
// fails when one of them has no entry.
var RequiredIDs = []string{
	"S01", "S02", "S03", "S04", "S05", "S06", "S07", "S08",
	"S09", "S10", "S11", "S12", "S13", "S14", "S15", "S16",
}

// Registry returns the required scenario registry.
func Registry() []Scenario {
	return []Scenario{
		{
			ID:       "S01",
			Title:    "Import two Markdown documents and answer a cited question",
			Class:    ClassExecuted,
			Proof:    "internal/knowledge:TestCompilerBuildsIncrementalPlanWithSectionTreeAndSummary",
			Evidence: "internal/knowledge/compiler_test.go",
			Reason:   "Proved through the Danny compile pipeline over parsed sources; the plan carries the section tree, the summary, and the citations that the answer refers to.",
		},
		{
			ID:       "S02",
			Title:    "Save a personal preference to Stele and retrieve it in a new conversation",
			Class:    ClassLiveRequired,
			Proof:    "cmd/runtime-probe stele-smoke",
			Evidence: "testdata/runtimeprobe/stele-19fc901-danny-smoke.evidence.json",
			Reason:   "Needs a live Stele deployment and provider credential. Never reported as passing without a recorded live evidence bundle.",
		},
		{
			ID:       "S03",
			Title:    "Create, update, and complete a task across conversations with revision checks",
			Class:    ClassExecuted,
			Proof:    "internal/scenarios:TestScenarioTaskRevisionAcrossConversations",
			Evidence: "internal/scenarios/scenarios_test.go",
			Reason:   "The durable task revision check is a Danny-owned contract and needs no upstream service.",
		},
		{
			ID:       "S04",
			Title:    "Run a daily natural-language summary schedule and save the result to memory",
			Class:    ClassExecuted,
			Proof:    "internal/tasks:TestOccurrenceCalculatorStableNormalOccurrence",
			Evidence: "internal/tasks/schedule_test.go",
			Reason:   "Occurrence calculation and the occurrence-scoped effect key are Danny-owned contracts.",
		},
		{
			ID:       "S05",
			Title:    "Execute a fixed reminder with a failing Webhook and verify independent retry status",
			Class:    ClassExecuted,
			Proof:    "internal/storage:TestWebhookDeliveryLeaseFencingAndIndependentTerminalStatus",
			Evidence: "internal/storage/schedule_repositories_test.go",
			Reason:   "Delivery retry state is independent of task status and is a Danny-owned contract.",
		},
		{
			ID:       "S06",
			Title:    "Compile a multi-section document with subagents, team aggregation, citation validation, and an atomic generation commit",
			Class:    ClassExecuted,
			Proof:    "internal/knowledge:TestCompileCoordinatorBlocksCommitWhenChildFails",
			Evidence: "internal/knowledge/coordinator_test.go",
			Reason:   "The coordinator refuses to commit when a child fails or the vote is not approved, so only a fully validated plan reaches the single commit.",
		},
		{
			ID:       "S07",
			Title:    "Trigger dynamic planner choices for direct, subagent, team, workflow, mailbox, and scheduled modes",
			Class:    ClassExecuted,
			Proof:    "internal/agent/knowledge:TestPlannerProducesDeterministicPlansAndRejectsUnsupportedModes",
			Evidence: "internal/agent/knowledge/agent_test.go",
			Reason:   "Planner mode selection is a deterministic Danny decision and needs no provider.",
		},
		{
			ID:       "S08",
			Title:    "Pause and resume a run from a harness checkpoint",
			Class:    ClassExecuted,
			Proof:    "internal/tasks:TestHarnessResumesNativeCheckpointThroughAdapter",
			Evidence: "internal/tasks/harness_test.go",
			Reason:   "Checkpoint commit and resume through the native restore adapter are Danny-owned contracts.",
		},
		{
			ID:       "S09",
			Title:    "Crash at every documented outbox, lease, materialization, and webhook boundary and verify recovery",
			Class:    ClassExecuted,
			Proof:    "internal/storage:TestFailureInjectorCoversGenerationLeaseEffectAndMaterialization",
			Evidence: "internal/storage/storage_test.go",
			Reason:   "The storage failure injector drives a crash at each boundary; a committed generation rebuilds its derived materialization after the crash.",
		},
		{
			ID:       "S10",
			Title:    "Run the full suite in SQLite and PostgreSQL modes",
			Class:    ClassExecuted,
			Proof:    "cmd/release-gate",
			Evidence: "docs/release-gate.md",
			Reason:   "The gate runs the suite against both drivers; PostgreSQL runs through the Milestone 11 Compose override.",
		},
		{
			ID:       "S11",
			Title:    "Deterministic fake-model contract tests and an OpenAI-compatible live smoke test",
			Class:    ClassExecuted,
			Proof:    "internal/scenarios:TestScenarioDeterministicContractIsReproducible",
			Evidence: "internal/scenarios/scenarios_test.go",
			Reason:   "The deterministic half is proved here with the scripted doubles. The live half stays live-required and runs as `cmd/runtime-probe live-smoke` with a provider credential.",
		},
		{
			ID:       "S12",
			Title:    "Import PDF, DOCX, PPTX, XLSX, XLS, HTML, CSV, TXT, and Markdown through the parser service",
			Class:    ClassExecuted,
			Proof:    "parser_service:test_format_matrix",
			Evidence: "parser_service/test_formats.py",
			Reason:   "Each format must produce canonical output or a structured failure; a missing optional dependency is itself a structured failure.",
		},
		{
			ID:       "S13",
			Title:    "Verify active upstream findings have regression fixtures and blocked tasks stay blocked",
			Class:    ClassExecuted,
			Proof:    "internal/scenarios:TestUpstreamFindingsHaveRegressionEvidence",
			Evidence: "internal/scenarios/upstream_test.go",
			Reason:   "The findings documents stay the source of truth for finding state, and the linkage check enforces the five required fields per finding.",
		},
		{
			ID:       "S14",
			Title:    "Reconnect during direct, child, team, workflow, mailbox, and scheduled runs and verify the projection converges",
			Class:    ClassExecuted,
			Proof:    "internal/server:TestSSEReconnectConvergesOnTerminalState",
			Evidence: "internal/server/agentui_test.go",
			Reason:   "A late client with a stale cursor receives the snapshot plus bounded replay and ends on exactly one terminal event that matches the durable record.",
		},
		{
			ID:       "S15",
			Title:    "Render a restricted A2UI surface and reject stale, replayed, unauthorized, malformed, and untrusted submissions",
			Class:    ClassExecuted,
			Proof:    "internal/server:TestSurfaceSubmissionConvergence",
			Evidence: "internal/server/agentui_test.go",
			Reason:   "The server gate owns action authorization, so the submission matrix is a Danny contract.",
		},
		{
			ID:       "S16",
			Title:    "Verify Baymax execution recovery and Stele bounded context and restart behavior",
			Class:    ClassLiveRequired,
			Proof:    "cmd/runtime-probe live-pause-restore",
			Evidence: "testdata/runtimeprobe/baymax-live-pause-restore.evidence.json",
			Reason:   "The live half needs a live Baymax endpoint. The deterministic restart half is covered by the Milestone 8 context contract tests.",
		},
	}
}

// ByID returns the registry entry for an identifier.
func ByID(id string) (Scenario, bool) {
	for _, scenario := range Registry() {
		if scenario.ID == id {
			return scenario, true
		}
	}
	return Scenario{}, false
}
