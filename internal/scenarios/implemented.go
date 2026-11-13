package scenarios

// Implemented maps a scenario to the check that proves it.
//
// A scenario classified `executed` must appear here. The registry invariant
// test fails when it does not, so a claim cannot outrun its implementation.
// A proof may be a test in this package, a contract test in another package,
// a parser-service check, or a release-gate command.
//
// The format is "<location>:<TestName>" for a test, and a command line for a
// release-gate command.
var Implemented = map[string]string{
	"S01": "internal/knowledge:TestCompilerBuildsIncrementalPlanWithSectionTreeAndSummary",
	"S03": "internal/scenarios:TestScenarioTaskRevisionAcrossConversations",
	"S04": "internal/tasks:TestOccurrenceCalculatorStableNormalOccurrence",
	"S05": "internal/storage:TestWebhookDeliveryLeaseFencingAndIndependentTerminalStatus",
	"S06": "internal/knowledge:TestCompileCoordinatorBlocksCommitWhenChildFails",
	"S07": "internal/agent/knowledge:TestPlannerProducesDeterministicPlansAndRejectsUnsupportedModes",
	"S08": "internal/tasks:TestHarnessResumesNativeCheckpointThroughAdapter",
	"S09": "internal/storage:TestFailureInjectorCoversGenerationLeaseEffectAndMaterialization",
	"S10": "cmd/release-gate",
	"S11": "internal/scenarios:TestScenarioDeterministicContractIsReproducible",
	"S12": "parser_service:test_format_matrix",
	"S13": "internal/scenarios:TestUpstreamFindingsHaveRegressionEvidence",
	"S14": "internal/server:TestSSEReconnectConvergesOnTerminalState",
	"S15": "internal/server:TestSurfaceSubmissionConvergence",
}
