package stele

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/FelixSeptem/danny/internal/runtimeboundary"
	"github.com/FelixSeptem/danny/internal/runtimeprobe"
	"github.com/FelixSeptem/danny/internal/storage"
	"github.com/FelixSeptem/stele/openapi"
)

const PinnedVersion = "v0.0.0-20261025080141-f38016ec0cba"

// ErrIdempotencyConflict means the provider rejected reuse of an exact
// idempotency key with a different payload. Callers must not retry it as a
// transport failure or execute the product operation again.
var ErrIdempotencyConflict = errors.New("stele: idempotency key conflict")

// Adapter is the HTTP/OpenAPI boundary for Stele's public runtime surface.
// Stele's internal Go packages are intentionally not imported by this project.
type Adapter interface {
	runtimeprobe.SteleAdapter
}

type Client struct {
	BaseURL   string
	HTTP      *http.Client
	APIKey    string
	BindingID string
	SessionID string
	Tenant    string
	Project   string
	Namespace string
}

// ProjectionRequest is the narrow product request for bounded context
// disclosure. It intentionally carries a query and budget rather than a
// request to export the Stele store.
type ProjectionRequest struct {
	RunID, Query, Role, Scope, SessionID, TurnID, PolicyDigest                    string
	Budget, MaxBytes, MaxReferences                                               int
	MemoryPath, PathPrefix, ProjectionIntent, ContinuityMode, ContinuityReference string
}

type ProjectionReference struct {
	ID           string               `json:"id"`
	Kind         string               `json:"kind"`
	Version      string               `json:"version"`
	MemoryPath   string               `json:"memory_path"`
	Class        string               `json:"class"`
	Availability string               `json:"availability"`
	Source       SourceMetadata       `json:"source_metadata"`
	Citation     []ProjectionCitation `json:"citations,omitempty"`
}

type SourceMetadata struct {
	Version        string `json:"version"`
	Trust          string `json:"trust"`
	Classification string `json:"classification"`
}

func runtimeReferenceSourceMetadata() SourceMetadata {
	return SourceMetadata{
		Version:        "source-metadata-v1",
		Trust:          "untrusted",
		Classification: "medium",
	}
}

type ProjectionCitation struct {
	SourceKind   string `json:"source_kind"`
	Reference    string `json:"reference"`
	Version      string `json:"version"`
	Availability string `json:"availability"`
}
type SelectionEvidence struct {
	Outcome             string   `json:"outcome"`
	OmissionReasons     []string `json:"omission_reasons"`
	UnsupportedSections []string `json:"unsupported_sections"`
}
type BudgetEvidence struct {
	ContractVersion      string            `json:"contract_version"`
	PackingPolicyVersion string            `json:"packing_policy_version"`
	Requested            map[string]any    `json:"requested"`
	Effective            map[string]any    `json:"effective"`
	Usage                map[string]any    `json:"usage"`
	Selection            SelectionEvidence `json:"selection"`
}
type DisclosureEvidence struct {
	Profile               string `json:"profile"`
	PolicyVersion         string `json:"policy_version"`
	ProjectionIntent      string `json:"projection_intent"`
	SourceMetadataVersion string `json:"source_metadata_version"`
}
type ContinuityEvidence struct {
	ContractVersion string `json:"contract_version"`
	ReadSemantics   string `json:"read_semantics"`
	Outcome         string `json:"outcome"`
	Reference       string `json:"reference,omitempty"`
	ExpiresAt       string `json:"expires_at,omitempty"`
}

type BoundedProjection struct {
	References                                 []ProjectionReference           `json:"references"`
	Digest                                     string                          `json:"digest"`
	SourceDigest                               string                          `json:"source_digest"`
	Scope, SessionID, TurnID, BindingID, RunID string                          `json:"-"`
	Disclosure                                 DisclosureEvidence              `json:"disclosure"`
	Budget                                     BudgetEvidence                  `json:"budget"`
	Continuity                                 ContinuityEvidence              `json:"continuity"`
	Envelope                                   runtimeboundary.ContextEnvelope `json:"-"`
	Content                                    string                          `json:"-"`
}

func (c *Client) BoundedProjection(ctx context.Context, req ProjectionRequest) (BoundedProjection, error) {
	if c == nil || strings.TrimSpace(c.BaseURL) == "" || strings.TrimSpace(req.RunID) == "" || strings.TrimSpace(req.Query) == "" {
		return BoundedProjection{}, fmt.Errorf("stele projection request is not configured")
	}
	if req.Budget <= 0 {
		req.Budget = 100
	}
	if req.MaxBytes <= 0 {
		req.MaxBytes = 1 << 20
	}
	if req.MaxReferences <= 0 {
		req.MaxReferences = req.Budget
	}
	if req.MemoryPath != "" && req.PathPrefix != "" {
		return BoundedProjection{}, fmt.Errorf("stele path and path_prefix are mutually exclusive")
	}
	if err := validatePathSelector(req.MemoryPath, "path"); err != nil {
		return BoundedProjection{}, err
	}
	if err := validatePathSelector(req.PathPrefix, "path_prefix"); err != nil {
		return BoundedProjection{}, err
	}
	if req.SessionID == "" {
		req.SessionID = c.SessionID
	}
	if req.Scope == "" {
		req.Scope = strings.Join([]string{c.Tenant, c.Project, c.Namespace}, "/")
	}
	expectedScope := strings.Join([]string{c.Tenant, c.Project, c.Namespace}, "/")
	if req.TurnID == "" || req.SessionID == "" || req.Scope == "//" || expectedScope == "//" || req.Scope != expectedScope {
		return BoundedProjection{}, fmt.Errorf("stele projection requires bound session, turn, and scope")
	}
	if req.ProjectionIntent == "" {
		req.ProjectionIntent = "task_context"
	}
	if req.ProjectionIntent != "conversation" && req.ProjectionIntent != "task_context" && req.ProjectionIntent != "compilation" && req.ProjectionIntent != "handoff" {
		return BoundedProjection{}, fmt.Errorf("stele projection has unsupported intent %q", req.ProjectionIntent)
	}
	if req.ContinuityMode != "" && req.ContinuityMode != "issue" && req.ContinuityMode != "compare" {
		return BoundedProjection{}, fmt.Errorf("stele continuity mode %q is unsupported", req.ContinuityMode)
	}
	if req.ContinuityMode == "issue" && req.ContinuityReference != "" {
		return BoundedProjection{}, fmt.Errorf("stele continuity issue cannot include a reference")
	}
	if req.ContinuityMode == "compare" && !validContinuityReference(req.ContinuityReference) {
		return BoundedProjection{}, fmt.Errorf("stele continuity compare requires a valid reference")
	}
	input := map[string]any{"query": req.Query, "budget": req.Budget, "turn_id": req.TurnID, "projection_intent": req.ProjectionIntent, "response_mode": "reference_only", "budget_limits": map[string]any{"max_response_bytes": req.MaxBytes, "max_citations": req.MaxReferences}}
	if req.ContinuityMode != "" {
		continuity := map[string]any{"mode": req.ContinuityMode}
		if req.ContinuityReference != "" {
			continuity["reference"] = req.ContinuityReference
		}
		input["continuity"] = continuity
	}
	if req.MemoryPath != "" {
		input["path"] = req.MemoryPath
	}
	if req.PathPrefix != "" {
		input["path_prefix"] = req.PathPrefix
	}
	body := map[string]any{
		"metadata": map[string]any{"request_id": req.RunID, "operation_id": "context_projection", "schema_version": "provider-v1", "idempotency_key": req.RunID + ":projection"},
		"input":    input,
	}
	var response providerContextResponse
	if err := c.requestLimited(ctx, http.MethodPost, "/v1/provider/context", body, &response, req.MaxBytes); err != nil {
		return BoundedProjection{}, err
	}
	if err := response.validate(req); err != nil {
		return BoundedProjection{}, err
	}
	refs := response.Result.references()
	if len(refs) > req.MaxReferences {
		return BoundedProjection{}, fmt.Errorf("stele context exceeded reference bound")
	}
	envelope := runtimeboundary.ContextEnvelope{Version: runtimeboundary.ContextContractV1, RunID: req.RunID, Scope: req.Scope, MemoryPath: req.MemoryPath, PathPrefix: req.PathPrefix, SessionID: req.SessionID, TurnID: req.TurnID, Role: req.Role, ProjectionIntent: req.ProjectionIntent, Budget: runtimeboundary.ContextBudget{Unit: runtimeboundary.BudgetItems, Limit: req.Budget}, PolicyDigest: req.PolicyDigest, SourceDigest: response.Result.ContextDigest.Value, References: make([]runtimeboundary.ContextReference, 0, len(refs)), DiagnosticsState: runtimeboundary.DiagnosticsOmitted}
	for _, ref := range refs {
		envelope.References = append(envelope.References, runtimeboundary.ContextReference{ID: ref.ID, Citation: ref.ID, Provenance: runtimeboundary.Provenance{SourceID: ref.ID, Path: ref.MemoryPath, Digest: ref.Version}})
	}
	envelope.ProjectionDigest = envelope.ComputeProjectionDigest()
	contextRequest := runtimeboundary.ContextRequest{Version: runtimeboundary.ContextContractV1, RunID: req.RunID, Scope: req.Scope, MemoryPath: req.MemoryPath, PathPrefix: req.PathPrefix, SessionID: req.SessionID, TurnID: req.TurnID, Query: req.Query, ProjectionIntent: req.ProjectionIntent, Budget: envelope.Budget, MaxBytes: req.MaxBytes, MaxReferences: req.MaxReferences, Role: req.Role, PolicyDigest: req.PolicyDigest}
	if err := envelope.Validate(contextRequest); err != nil {
		return BoundedProjection{}, err
	}
	return BoundedProjection{References: refs, Digest: envelope.ProjectionDigest, SourceDigest: response.Result.ContextDigest.Value, Scope: req.Scope, SessionID: req.SessionID, TurnID: req.TurnID, BindingID: c.BindingID, RunID: req.RunID, Disclosure: response.Result.Disclosure, Budget: response.Result.Budget, Continuity: response.Result.Continuity, Envelope: envelope}, nil
}

// Assemble implements the Danny-owned context boundary. It accepts only the
// normalized contract and returns the validated, detached envelope.
func (c *Client) Assemble(ctx context.Context, req runtimeboundary.ContextRequest) (runtimeboundary.ContextEnvelope, error) {
	if err := req.Validate(); err != nil {
		return runtimeboundary.ContextEnvelope{}, err
	}
	if req.Budget.Unit != runtimeboundary.BudgetItems {
		return runtimeboundary.ContextEnvelope{}, fmt.Errorf("stele context supports item budgets only")
	}
	projectionIntent := req.ProjectionIntent
	if projectionIntent == "" {
		projectionIntent = "task_context"
	}
	p, err := c.BoundedProjection(ctx, ProjectionRequest{RunID: req.RunID, Query: req.Query, Budget: req.Budget.Limit, MemoryPath: req.MemoryPath, PathPrefix: req.PathPrefix, Role: req.Role, ProjectionIntent: projectionIntent, Scope: req.Scope, SessionID: req.SessionID, TurnID: req.TurnID, PolicyDigest: req.PolicyDigest, MaxBytes: req.MaxBytes, MaxReferences: req.MaxReferences})
	if err != nil {
		return runtimeboundary.ContextEnvelope{}, err
	}
	return p.Envelope, nil
}

var _ runtimeboundary.ContextAdapter = (*Client)(nil)

// PersistProjection stores the admitted, bounded envelope in Danny's
// application repository. The Stele continuity receipt remains metadata-only;
// the envelope is the restart projection owned by Danny.
func PersistProjection(ctx context.Context, repo storage.ContextProjectionRepository, projection BoundedProjection) (storage.ContextProjectionSnapshot, error) {
	if repo == nil || projection.Digest == "" || projection.SourceDigest == "" || projection.Envelope.Version == "" {
		return storage.ContextProjectionSnapshot{}, fmt.Errorf("invalid admitted Stele projection")
	}
	payload, err := json.Marshal(projection.Envelope)
	if err != nil {
		return storage.ContextProjectionSnapshot{}, err
	}
	return repo.Save(ctx, storage.ContextProjectionSnapshot{ID: projection.Digest, Scope: projection.Scope, SessionID: projection.SessionID, TurnID: projection.TurnID, BindingID: projection.BindingID, RunID: projection.RunID, ContractVersion: projection.Envelope.Version, Digest: projection.Digest, SourceDigest: projection.SourceDigest, ContinuityContractVersion: projection.Continuity.ContractVersion, ContinuityReference: projection.Continuity.Reference, ContinuityExpiresAt: projection.Continuity.ExpiresAt, Envelope: payload})
}

type providerContextResponse struct {
	Metadata  providerOperationMetadata `json:"metadata"`
	Result    providerContextResult     `json:"result"`
	Citations []ProjectionCitation      `json:"citations"`
	bodyBytes int
}

type providerOperationMetadata struct {
	RequestID      string `json:"request_id"`
	OperationID    string `json:"operation_id"`
	SchemaVersion  string `json:"schema_version"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

func (r *providerContextResponse) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, required := range []string{"metadata", "result", "citations"} {
		if _, exists := fields[required]; !exists {
			return fmt.Errorf("stele context response is missing %s", required)
		}
	}
	var resultFields map[string]json.RawMessage
	if err := json.Unmarshal(fields["result"], &resultFields); err != nil {
		return err
	}
	for _, required := range []string{"context_digest", "continuity", "response_mode", "disclosure", "budget_accounting", "profile", "recent_session", "recent_episodes", "relevant_summaries", "related_entities", "citations"} {
		if _, exists := resultFields[required]; !exists {
			return fmt.Errorf("stele context result is missing %s", required)
		}
	}
	type responseAlias providerContextResponse
	var decoded responseAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*r = providerContextResponse(decoded)
	r.bodyBytes = len(data)
	return nil
}

func (r providerContextResponse) validate(req ProjectionRequest) error {
	if r.Metadata.RequestID != req.RunID || strings.TrimSpace(r.Metadata.OperationID) == "" || r.Metadata.SchemaVersion != "provider-v1" {
		return fmt.Errorf("stele context response metadata is missing or mismatched")
	}
	if err := r.Result.validate(req); err != nil {
		return err
	}
	inner := make(map[string]struct{})
	for _, ref := range r.Result.references() {
		for _, citation := range ref.Citation {
			inner[citationIdentity(citation)] = struct{}{}
		}
	}
	outer := make(map[string]struct{}, len(r.Citations))
	for _, citation := range r.Citations {
		if !validProviderCitation(citation) {
			return fmt.Errorf("stele context outer citation provenance is invalid")
		}
		key := citationIdentity(citation)
		if _, exists := outer[key]; exists {
			return fmt.Errorf("stele context outer citations contain a duplicate")
		}
		outer[key] = struct{}{}
	}
	if len(inner) != len(outer) {
		return fmt.Errorf("stele context inner and outer citations disagree")
	}
	for key := range inner {
		if _, exists := outer[key]; !exists {
			return fmt.Errorf("stele context inner and outer citations disagree")
		}
	}
	usedBytes, ok := budgetInteger(r.Result.Budget.Usage, "response_bytes")
	if !ok || usedBytes != r.bodyBytes {
		return fmt.Errorf("stele context response byte accounting does not match body")
	}
	return nil
}

func validProviderCitation(citation ProjectionCitation) bool {
	return citation.SourceKind == "canonical_memory" && citation.Reference != "" && citation.Version != "" && citation.Availability == "available"
}

func citationIdentity(citation ProjectionCitation) string {
	return strings.Join([]string{citation.SourceKind, citation.Reference, citation.Version}, "\x00")
}

type providerContextResult struct {
	TurnID        string `json:"turn_id"`
	ResponseMode  string `json:"response_mode"`
	ContextDigest struct {
		ContractVersion            string `json:"contract_version"`
		Algorithm                  string `json:"algorithm"`
		CanonicalizationVersion    string `json:"canonicalization_version"`
		SelectionPolicyVersion     string `json:"selection_policy_version"`
		SelectionPolicyFingerprint string `json:"selection_policy_fingerprint"`
		Value                      string `json:"value"`
	} `json:"context_digest"`
	Continuity        ContinuityEvidence      `json:"continuity"`
	Disclosure        DisclosureEvidence      `json:"disclosure"`
	Budget            BudgetEvidence          `json:"budget_accounting"`
	Profile           []providerReferenceItem `json:"profile"`
	RecentSession     []providerReferenceItem `json:"recent_session"`
	RecentEpisodes    []providerReferenceItem `json:"recent_episodes"`
	RelevantSummaries []providerReferenceItem `json:"relevant_summaries"`
	RelatedEntities   []providerReferenceItem `json:"related_entities"`
}
type providerReferenceItem struct {
	Reference ProjectionReference  `json:"reference"`
	Citations []ProjectionCitation `json:"citations"`
}

func (r providerContextResult) references() []ProjectionReference {
	out := make([]ProjectionReference, 0)
	for _, items := range [][]providerReferenceItem{r.Profile, r.RecentSession, r.RecentEpisodes, r.RelevantSummaries, r.RelatedEntities} {
		for _, item := range items {
			item.Reference.Citation = append([]ProjectionCitation(nil), item.Citations...)
			out = append(out, item.Reference)
		}
	}
	return out
}
func (r providerContextResult) validate(req ProjectionRequest) error {
	if r.TurnID != req.TurnID {
		return fmt.Errorf("stele context turn binding mismatch")
	}
	if r.ResponseMode != "reference_only" {
		return fmt.Errorf("stele context response mode is unsupported")
	}
	if len(r.ContextDigest.Value) != 64 || r.ContextDigest.Algorithm != "sha-256" || (r.ContextDigest.ContractVersion != "context-turn-digest-v1" && r.ContextDigest.ContractVersion != "context-digest-v1") {
		return fmt.Errorf("stele context digest is missing or unsupported")
	}
	if req.TurnID != "" && r.ContextDigest.ContractVersion != "context-turn-digest-v1" {
		return fmt.Errorf("stele context turn-bound digest is missing")
	}
	if r.ContextDigest.CanonicalizationVersion != "context-turn-canonical-v1" && r.ContextDigest.CanonicalizationVersion != "context-canonical-v1" {
		return fmt.Errorf("stele context canonicalization evidence is missing")
	}
	if r.ContextDigest.SelectionPolicyVersion != "context-selection-v1" || !isHexDigest(r.ContextDigest.SelectionPolicyFingerprint) {
		return fmt.Errorf("stele context selection evidence is missing")
	}
	if _, err := hex.DecodeString(r.ContextDigest.Value); err != nil {
		return fmt.Errorf("stele context digest is malformed")
	}
	if r.Disclosure.Profile != "memory-reference-v1" || r.Disclosure.PolicyVersion != "context-disclosure-v1" || r.Disclosure.SourceMetadataVersion != "source-metadata-v1" {
		return fmt.Errorf("stele context disclosure evidence is missing")
	}
	if req.ProjectionIntent != "" && r.Disclosure.ProjectionIntent != req.ProjectionIntent {
		return fmt.Errorf("stele context disclosure intent drift")
	}
	if r.Budget.ContractVersion != "context-budget-v1" || r.Budget.PackingPolicyVersion != "context-packing-v1" || len(r.Budget.Effective) == 0 || len(r.Budget.Usage) == 0 {
		return fmt.Errorf("stele context budget evidence is missing")
	}
	if r.Budget.Selection.Outcome != "selected" && r.Budget.Selection.Outcome != "empty" && r.Budget.Selection.Outcome != "budget_exhausted" {
		return fmt.Errorf("stele context selection evidence is invalid")
	}
	for _, reason := range r.Budget.Selection.OmissionReasons {
		if reason != "item_limit" && reason != "content_bytes" && reason != "citations" && reason != "response_bytes" {
			return fmt.Errorf("stele context omission reason is invalid")
		}
	}
	for _, section := range r.Budget.Selection.UnsupportedSections {
		if section != "experience_insights" && section != "goal_context" && section != "diagnostics" && section != "feedback_diagnostics" {
			return fmt.Errorf("stele context unsupported section is invalid")
		}
	}
	requestedItems, ok := budgetInteger(r.Budget.Requested, "max_items")
	if !ok || requestedItems != req.Budget {
		return fmt.Errorf("stele context budget item unit mismatch")
	}
	requestedBytes, bytesOK := budgetInteger(r.Budget.Requested, "max_response_bytes")
	requestedCitations, citationsOK := budgetInteger(r.Budget.Requested, "max_citations")
	requestedContent, contentExists := r.Budget.Requested["max_content_bytes"]
	if !bytesOK || requestedBytes != req.MaxBytes || !citationsOK || requestedCitations != req.MaxReferences || !contentExists || requestedContent != nil {
		return fmt.Errorf("stele context requested byte or citation budget mismatch")
	}
	effectiveItems, ok := budgetInteger(r.Budget.Effective, "max_items")
	usageItems, usageOK := budgetInteger(r.Budget.Usage, "items")
	if !ok || !usageOK || effectiveItems < 1 || effectiveItems > req.Budget || usageItems < 0 || usageItems > effectiveItems || usageItems > req.Budget {
		return fmt.Errorf("stele context budget accounting is invalid")
	}
	effectiveBytes, hasEffectiveBytes := budgetInteger(r.Budget.Effective, "max_response_bytes")
	if !hasEffectiveBytes || effectiveBytes <= 0 {
		return fmt.Errorf("stele context byte budget evidence is missing")
	}
	if effectiveBytes > req.MaxBytes {
		return fmt.Errorf("stele context response byte budget exceeds request")
	}
	usedBytes, hasUsedBytes := budgetInteger(r.Budget.Usage, "response_bytes")
	if !hasUsedBytes {
		return fmt.Errorf("stele context response byte usage is missing")
	}
	if usedBytes > effectiveBytes || usedBytes > req.MaxBytes {
		return fmt.Errorf("stele context response byte budget usage exceeds request")
	}
	effectiveContent, hasEffectiveContent := budgetInteger(r.Budget.Effective, "max_content_bytes")
	usedContent, hasUsedContent := budgetInteger(r.Budget.Usage, "content_bytes")
	if !hasEffectiveContent || !hasUsedContent || usedContent > effectiveContent {
		return fmt.Errorf("stele context content byte accounting is invalid")
	}
	effectiveCitations, hasEffectiveCitations := budgetInteger(r.Budget.Effective, "max_citations")
	if !hasEffectiveCitations {
		return fmt.Errorf("stele context citation budget evidence is missing")
	}
	if effectiveCitations > req.MaxReferences {
		return fmt.Errorf("stele context citation budget exceeds request")
	}
	usedCitations, hasUsedCitations := budgetInteger(r.Budget.Usage, "citations")
	if !hasUsedCitations {
		return fmt.Errorf("stele context citation usage is missing")
	}
	if usedCitations > effectiveCitations || usedCitations > req.MaxReferences {
		return fmt.Errorf("stele context citations exceed request")
	}
	if r.Continuity.ContractVersion != "context-continuity-v1" || (r.Continuity.ReadSemantics != "fresh" && r.Continuity.ReadSemantics != "fresh_compare") {
		return fmt.Errorf("stele context continuity evidence is missing")
	}
	switch req.ContinuityMode {
	case "issue":
		if r.Continuity.ReadSemantics != "fresh" || r.Continuity.Outcome != "issued" || !validContinuityReference(r.Continuity.Reference) || !validContinuityExpiry(r.Continuity.ExpiresAt) {
			return fmt.Errorf("stele context continuity issue receipt is invalid")
		}
	case "compare":
		if r.Continuity.ReadSemantics != "fresh_compare" || (r.Continuity.Outcome != "unchanged" && r.Continuity.Outcome != "changed") || r.Continuity.Reference != req.ContinuityReference || !validContinuityExpiry(r.Continuity.ExpiresAt) {
			return fmt.Errorf("stele context continuity compare evidence is invalid")
		}
	default:
		if r.Continuity.ReadSemantics != "fresh" || r.Continuity.Outcome != "not_requested" || r.Continuity.Reference != "" || r.Continuity.ExpiresAt != "" {
			return fmt.Errorf("stele context continuity evidence is invalid")
		}
	}
	if req.ContinuityMode == "issue" && (!validContinuityReference(r.Continuity.Reference) || !validContinuityExpiry(r.Continuity.ExpiresAt)) {
		return fmt.Errorf("stele context continuity issue receipt is missing")
	}
	refs := r.references()
	if len(refs) != usageItems || len(refs) > req.Budget {
		return fmt.Errorf("stele context items exceed requested budget")
	}
	uniqueCitations := make(map[string]struct{})
	for _, ref := range refs {
		if len(ref.ID) == 0 || len(ref.ID) > 512 || len(ref.MemoryPath) == 0 || len(ref.MemoryPath) > 512 || !validPositiveVersion(ref.Version) || ref.Kind != "memory" || !validMemoryClass(ref.Class) || ref.Availability != "available" || ref.Source.Version != "source-metadata-v1" || (ref.Source.Trust != "untrusted" && ref.Source.Trust != "attested_data") || (ref.Source.Classification != "low" && ref.Source.Classification != "medium" && ref.Source.Classification != "high" && ref.Source.Classification != "restricted") || len(ref.Citation) == 0 {
			return fmt.Errorf("stele context reference provenance is invalid")
		}
		if !pathMatchesSelector(ref.MemoryPath, req.MemoryPath, req.PathPrefix) {
			return fmt.Errorf("stele context reference is outside requested path")
		}
		for _, citation := range ref.Citation {
			if !validProviderCitation(citation) || !validPositiveVersion(citation.Version) {
				return fmt.Errorf("stele context citation provenance is invalid")
			}
			uniqueCitations[strings.Join([]string{citation.SourceKind, citation.Reference, citation.Version}, "\x00")] = struct{}{}
		}
	}
	if len(uniqueCitations) > req.MaxReferences || len(uniqueCitations) != usedCitations {
		return fmt.Errorf("stele context citation accounting does not match returned references")
	}
	return nil
}

func pathMatchesSelector(referencePath, exactPath, prefix string) bool {
	referencePath = strings.Trim(referencePath, "/")
	if exactPath != "" && exactPath != "/" {
		return referencePath == strings.Trim(exactPath, "/")
	}
	if prefix != "" && prefix != "/" {
		prefix = strings.Trim(prefix, "/")
		return referencePath == prefix || strings.HasPrefix(referencePath, prefix+"/")
	}
	return true
}

func budgetInteger(values map[string]any, key string) (int, bool) {
	value, ok := values[key]
	if !ok {
		return 0, false
	}
	switch n := value.(type) {
	case float64:
		if n < 0 || n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil && i >= 0
	default:
		return 0, false
	}
}

func isHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validContinuityReference(reference string) bool {
	if len(reference) != 37 || !strings.HasPrefix(reference, "ctxc_") {
		return false
	}
	for _, ch := range reference[5:] {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

func validContinuityExpiry(value string) bool {
	parsed, err := time.Parse("2006-01-02T15:04:05Z", value)
	return err == nil && parsed.UTC().Format("2006-01-02T15:04:05Z") == value
}

func validPositiveVersion(value string) bool {
	if value == "" || value[0] == '0' {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func validMemoryClass(value string) bool {
	switch value {
	case "profile", "episodic", "procedural", "summary", "relation":
		return true
	default:
		return false
	}
}

func validTurnID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && !strings.ContainsRune("_.:-", r) {
			return false
		}
	}
	return true
}

func validatePathSelector(value, field string) error {
	value = strings.TrimSpace(value)
	if value == "" || value == "/" {
		return nil
	}
	if len(value) > 512 || strings.Contains(value, "\\") {
		return fmt.Errorf("stele %s is invalid", field)
	}
	value = strings.Trim(value, "/")
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." || len(segment) > 128 {
			return fmt.Errorf("stele %s is invalid", field)
		}
		for _, r := range segment {
			if unicode.IsControl(r) || strings.ContainsRune("*?[]", r) {
				return fmt.Errorf("stele %s is invalid", field)
			}
		}
		lower := strings.ToLower(segment)
		if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
			return fmt.Errorf("stele %s is invalid", field)
		}
	}
	return nil
}

func NewClient(baseURL string, httpClient *http.Client) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, nil
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{BaseURL: baseURL, HTTP: httpClient}, nil
}

func (c *Client) Version() string { return PinnedVersion }

// OpenAPIInventory returns the exact public service contract exposed by the
// pinned Stele module. The adapter never imports Stele internal packages.
func OpenAPIInventory() string { return openapi.SpecYAML() }

type RuntimeInitialization struct {
	Scope          map[string]string `json:"scope"`
	AgentID        string            `json:"agent_id"`
	SessionID      string            `json:"session_id"`
	ConversationID string            `json:"conversation_id,omitempty"`
}

type RuntimeBinding struct {
	BindingID string `json:"binding_id"`
	SessionID string `json:"session_id"`
}

// ProviderTurn is the public, server-issued turn identity that may be bound
// to a provider context read. Danny never invents this ID locally.
type ProviderTurn struct {
	ID string `json:"turn_id"`
}

func (c *Client) InitializeRuntime(ctx context.Context, in RuntimeInitialization) (RuntimeBinding, error) {
	var out RuntimeBinding
	if err := c.request(ctx, http.MethodPost, "/v1/provider/runtime", in, &out); err != nil {
		return RuntimeBinding{}, err
	}
	c.BindingID, c.SessionID = out.BindingID, out.SessionID
	c.Tenant, c.Project, c.Namespace = in.Scope["tenant"], in.Scope["project"], in.Scope["namespace"]
	if c.SessionID == "" {
		c.SessionID = in.SessionID
	}
	return out, nil
}

// CreateTurn obtains a server-authorized turn before a turn-bound context
// read. The returned ID is attribution only; it does not grant new authority.
func (c *Client) CreateTurn(ctx context.Context, requestID, query string, budget int) (ProviderTurn, error) {
	if c == nil || strings.TrimSpace(requestID) == "" || strings.TrimSpace(query) == "" || budget <= 0 || strings.TrimSpace(c.SessionID) == "" {
		return ProviderTurn{}, fmt.Errorf("stele provider turn request is invalid")
	}
	body := map[string]any{
		"metadata": map[string]any{"request_id": requestID, "operation_id": "context_turn", "schema_version": "provider-v1", "idempotency_key": requestID},
		"input":    map[string]any{"query": query, "context_budget": budget, "idempotency_key": requestID},
	}
	var response struct {
		Result ProviderTurn `json:"result"`
	}
	if err := c.request(ctx, http.MethodPost, "/v1/provider/turns", body, &response); err != nil {
		return ProviderTurn{}, err
	}
	if !validTurnID(response.Result.ID) {
		return ProviderTurn{}, fmt.Errorf("stele provider turn response is invalid")
	}
	return response.Result, nil
}

func (c *Client) Context(ctx context.Context, req runtimeprobe.ProbeRequest) (runtimeprobe.ProbeOutcome, error) {
	return c.operation(ctx, req, runtimeprobe.KindContext, "/v1/provider/context", map[string]any{"input": map[string]any{"query": req.Input, "budget": 2048}})
}

func (c *Client) Memory(ctx context.Context, req runtimeprobe.ProbeRequest) (runtimeprobe.ProbeOutcome, error) {
	if c == nil || strings.TrimSpace(c.BaseURL) == "" {
		return c.operation(ctx, req, runtimeprobe.KindMemory, "/v1/provider/events", map[string]any{"event_type": "runtimeprobe.memory", "content": req.Input})
	}
	if req.CorrelationID == "" || req.ParentID == "" {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("correlation and parent ids are required")
	}
	writeBody := map[string]any{"metadata": map[string]any{"request_id": req.CorrelationID, "operation_id": req.Capability, "schema_version": "provider-v1", "idempotency_key": req.CorrelationID}, "event_type": "runtimeprobe.memory", "content": req.Input}
	var writeResponse map[string]any
	writeErr := c.request(ctx, http.MethodPost, "/v1/provider/events", writeBody, &writeResponse)
	readBody := map[string]any{"metadata": map[string]any{"request_id": req.CorrelationID + ":retrieve", "operation_id": "retrieve", "schema_version": "provider-v1", "idempotency_key": req.CorrelationID + ":retrieve"}, "input": map[string]any{"query": req.Input, "top_k": 10}}
	var readResponse map[string]any
	readErr := c.request(ctx, http.MethodPost, "/v1/provider/retrieve", readBody, &readResponse)
	status := runtimeprobe.StatusPassed
	if writeErr != nil || readErr != nil {
		status = runtimeprobe.StatusFailedContract
	}
	now := time.Unix(0, 0).UTC()
	payload := map[string]string{"write_path": "/v1/provider/events", "retrieve_path": "/v1/provider/retrieve", "retrieve": "passed"}
	if id, ok := writeResponse["event_id"].(string); ok && id != "" {
		payload["event_id"] = id
	}
	if writeErr != nil {
		payload["write_error"] = writeErr.Error()
	}
	if readErr != nil {
		payload["retrieve"] = "failed"
		payload["retrieve_error"] = readErr.Error()
	}
	events := []runtimeprobe.Event{{Sequence: 1, ID: req.ParentID, Kind: runtimeprobe.KindParent, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now}, {Sequence: 2, ID: req.ParentID + ":memory", Kind: runtimeprobe.KindMemory, ParentID: req.ParentID, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(time.Nanosecond), Payload: payload}, {Sequence: 3, ID: req.ParentID + ":memory:terminal", Kind: runtimeprobe.KindTerminal, ParentID: req.ParentID + ":memory", CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(2 * time.Nanosecond)}}
	var err error
	if writeErr != nil {
		err = writeErr
	} else if readErr != nil {
		err = readErr
	}
	return runtimeprobe.ProbeOutcome{Events: events}, err
}

// References exercises the public provider event memory_path and exact-path
// retrieval contract for task and run references. Stele does not expose a
// separate reference endpoint; memory_path is the public scoped reference
// boundary and the returned canonical memory IDs prove persistence.
func (c *Client) References(ctx context.Context, req runtimeprobe.ProbeRequest) (runtimeprobe.ProbeOutcome, error) {
	if c == nil || strings.TrimSpace(c.BaseURL) == "" {
		return c.operation(ctx, req, runtimeprobe.KindMemory, "/v1/provider/events", map[string]any{"event_type": "runtimeprobe.reference", "content": req.Input})
	}
	if req.CorrelationID == "" || req.ParentID == "" {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("correlation and parent ids are required")
	}
	taskID := req.CorrelationID + ":task"
	runID := req.CorrelationID + ":run"
	taskValue := "task-reference:" + taskID
	runValue := "run-reference:" + runID
	taskPath := "runtimeprobe/tasks/" + taskID
	runPath := "runtimeprobe/runs/" + runID
	taskEventID, taskWriteErr := c.appendReference(ctx, req, "runtimeprobe.task_reference", taskPath, taskValue)
	runEventID, runWriteErr := c.appendReference(ctx, req, "runtimeprobe.run_reference", runPath, runValue)
	taskRetrieved, taskReadErr := c.retrieveReference(ctx, req, taskPath, taskValue)
	runRetrieved, runReadErr := c.retrieveReference(ctx, req, runPath, runValue)
	status := runtimeprobe.StatusPassed
	for _, err := range []error{taskWriteErr, runWriteErr, taskReadErr, runReadErr} {
		if err != nil {
			status = runtimeprobe.StatusFailedContract
			break
		}
	}
	now := time.Unix(0, 0).UTC()
	payload := map[string]string{
		"task_reference_id": taskEventID,
		"run_reference_id":  runEventID,
		"task_retrieved":    fmt.Sprint(taskRetrieved),
		"run_retrieved":     fmt.Sprint(runRetrieved),
		"task_path":         taskPath,
		"run_path":          runPath,
	}
	for key, err := range map[string]error{"task_write_error": taskWriteErr, "run_write_error": runWriteErr, "task_read_error": taskReadErr, "run_read_error": runReadErr} {
		if err != nil {
			payload[key] = err.Error()
		}
	}
	events := []runtimeprobe.Event{
		{Sequence: 1, ID: req.ParentID, Kind: runtimeprobe.KindParent, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now},
		{Sequence: 2, ID: req.ParentID + ":references", Kind: runtimeprobe.KindMemory, ParentID: req.ParentID, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(time.Nanosecond), Payload: payload},
		{Sequence: 3, ID: req.ParentID + ":references:terminal", Kind: runtimeprobe.KindTerminal, ParentID: req.ParentID + ":references", CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(2 * time.Nanosecond)},
	}
	var err error
	for _, candidate := range []error{taskWriteErr, runWriteErr, taskReadErr, runReadErr} {
		if candidate != nil {
			err = candidate
			break
		}
	}
	return runtimeprobe.ProbeOutcome{Events: events}, err
}

func (c *Client) appendReference(ctx context.Context, req runtimeprobe.ProbeRequest, eventType, memoryPath, content string) (string, error) {
	body := map[string]any{
		"metadata": map[string]any{"request_id": req.CorrelationID + ":" + eventType, "operation_id": eventType, "schema_version": "provider-v1", "idempotency_key": req.CorrelationID + ":" + eventType},
		// Stele's default governed extractor promotes conversation.message
		// events to canonical memories. Keep the task/run distinction in the
		// operation metadata while using that documented event contract.
		"event_type":      "conversation.message",
		"memory_path":     memoryPath,
		"content":         content,
		"source_metadata": runtimeReferenceSourceMetadata(),
	}
	var response map[string]any
	if err := c.request(ctx, http.MethodPost, "/v1/provider/events", body, &response); err != nil {
		return "", err
	}
	id, _ := response["event_id"].(string)
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("stele reference write returned no event_id")
	}
	return id, nil
}

func (c *Client) retrieveReference(ctx context.Context, req runtimeprobe.ProbeRequest, memoryPath, expectedContent string) (bool, error) {
	pathToken := referencePathToken(memoryPath)
	body := map[string]any{
		"metadata": map[string]any{"request_id": req.CorrelationID + ":retrieve:" + pathToken, "operation_id": "runtimeprobe.reference.retrieve", "schema_version": "provider-v1", "idempotency_key": req.CorrelationID + ":retrieve:" + pathToken},
		"input":    map[string]any{"query": expectedContent, "path": memoryPath, "top_k": 1},
	}
	var lastErr error
	for attempt := 0; attempt < 20; attempt++ {
		var response struct {
			Result struct {
				Matches []struct {
					MemoryPath string `json:"memory_path"`
				} `json:"matches"`
			} `json:"result"`
		}
		if err := c.request(ctx, http.MethodPost, "/v1/provider/retrieve", body, &response); err != nil {
			lastErr = err
		} else {
			for _, match := range response.Result.Matches {
				if match.MemoryPath == memoryPath {
					return true, nil
				}
			}
			lastErr = fmt.Errorf("stele reference %q was not returned by exact-path retrieve", memoryPath)
		}
		if attempt < 19 {
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
	return false, lastErr
}

// referencePathToken keeps provider operation metadata within Stele's token
// grammar while preserving the original memory path in the search selector.
// A digest also prevents path content from being copied into evidence or
// operation logs when this helper is reused for arbitrary references.
func referencePathToken(memoryPath string) string {
	digest := sha256.Sum256([]byte(memoryPath))
	return hex.EncodeToString(digest[:])
}

func (c *Client) RestartProjection(ctx context.Context, req runtimeprobe.ProbeRequest) (runtimeprobe.ProbeOutcome, error) {
	if c == nil {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("stele adapter is nil")
	}
	fresh := *c
	fresh.BindingID, fresh.SessionID = "", ""
	if fresh.BindingID == "" && fresh.APIKey != "" {
		_, err := fresh.InitializeRuntime(ctx, RuntimeInitialization{Scope: map[string]string{"tenant": c.Tenant, "project": c.Project, "namespace": c.Namespace}, AgentID: "runtimeprobe-restarted", SessionID: req.CorrelationID + ":restarted"})
		if err != nil {
			return runtimeprobe.ProbeOutcome{}, err
		}
	}
	return fresh.operation(ctx, req, runtimeprobe.KindContext, "/v1/provider/context", map[string]any{"input": map[string]any{"query": req.Input, "budget": 2048}})
}

func (c *Client) IdempotentMemory(ctx context.Context, req runtimeprobe.ProbeRequest) (runtimeprobe.ProbeOutcome, error) {
	if c == nil || strings.TrimSpace(c.BaseURL) == "" {
		return c.operation(ctx, req, runtimeprobe.KindMemory, "/v1/provider/events", map[string]any{"event_type": "runtimeprobe.memory", "content": req.Input})
	}
	if req.CorrelationID == "" || req.ParentID == "" {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("correlation and parent ids are required")
	}
	body := map[string]any{"metadata": map[string]any{"request_id": req.CorrelationID, "operation_id": req.Capability, "schema_version": "provider-v1", "idempotency_key": req.CorrelationID}, "event_type": "runtimeprobe.memory", "content": req.Input}
	var first, replay map[string]any
	errFirst := c.request(ctx, http.MethodPost, "/v1/provider/events", body, &first)
	errReplay := c.request(ctx, http.MethodPost, "/v1/provider/events", body, &replay)
	status := runtimeprobe.StatusPassed
	if errFirst != nil || errReplay != nil {
		status = runtimeprobe.StatusFailedContract
	}
	replayed, _ := replay["replayed"].(bool)
	if errFirst == nil && errReplay == nil && !replayed {
		status = runtimeprobe.StatusFailedContract
	}
	now := time.Unix(0, 0).UTC()
	payloadFirst := map[string]string{"path": "/v1/provider/events", "attempt": "first"}
	payloadReplay := map[string]string{"path": "/v1/provider/events", "attempt": "replay", "replayed": fmt.Sprint(replayed)}
	if id, ok := first["event_id"].(string); ok && id != "" {
		payloadFirst["event_id"] = id
	}
	if id, ok := replay["event_id"].(string); ok && id != "" {
		payloadReplay["event_id"] = id
	}
	if errFirst != nil {
		payloadFirst["error"] = errFirst.Error()
	}
	if errReplay != nil {
		payloadReplay["error"] = errReplay.Error()
	}
	events := []runtimeprobe.Event{{Sequence: 1, ID: req.ParentID, Kind: runtimeprobe.KindParent, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now}, {Sequence: 2, ID: req.ParentID + ":memory:first", Kind: runtimeprobe.KindMemory, ParentID: req.ParentID, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(time.Nanosecond), Payload: payloadFirst}, {Sequence: 3, ID: req.ParentID + ":memory:replay", Kind: runtimeprobe.KindMemory, ParentID: req.ParentID, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(2 * time.Nanosecond), Payload: payloadReplay}, {Sequence: 4, ID: req.ParentID + ":terminal", Kind: runtimeprobe.KindTerminal, ParentID: req.ParentID, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(3 * time.Nanosecond)}}
	var err error
	if errFirst != nil {
		err = errFirst
	} else if errReplay != nil {
		err = errReplay
	}
	return runtimeprobe.ProbeOutcome{Events: events}, err
}

func (c *Client) operation(ctx context.Context, req runtimeprobe.ProbeRequest, kind runtimeprobe.Kind, path string, input map[string]any) (runtimeprobe.ProbeOutcome, error) {
	if c == nil || strings.TrimSpace(c.BaseURL) == "" {
		return runtimeprobe.ProbeOutcome{Events: []runtimeprobe.Event{{Sequence: 1, ID: req.CorrelationID + ":terminal", Kind: runtimeprobe.KindTerminal, ParentID: req.ParentID, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: runtimeprobe.StatusSkippedUnconfigured, At: time.Unix(0, 0).UTC()}}}, nil
	}
	if req.CorrelationID == "" || req.ParentID == "" {
		return runtimeprobe.ProbeOutcome{}, fmt.Errorf("correlation and parent ids are required")
	}
	body := map[string]any{"metadata": map[string]any{"request_id": req.CorrelationID, "operation_id": req.Capability, "schema_version": "provider-v1", "idempotency_key": req.CorrelationID}}
	for k, v := range input {
		body[k] = v
	}
	var response map[string]any
	err := c.request(ctx, http.MethodPost, path, body, &response)
	status := runtimeprobe.StatusPassed
	payload := map[string]string{"path": path}
	if eventID, ok := response["event_id"].(string); ok && eventID != "" {
		payload["event_id"] = eventID
	}
	if replayed, ok := response["replayed"].(bool); ok {
		payload["replayed"] = fmt.Sprint(replayed)
	}
	if err != nil {
		status = runtimeprobe.StatusFailedContract
		payload["error"] = err.Error()
	}
	now := time.Unix(0, 0).UTC()
	events := []runtimeprobe.Event{{Sequence: 1, ID: req.ParentID, Kind: runtimeprobe.KindParent, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now}, {Sequence: 2, ID: req.ParentID + ":" + string(kind), Kind: kind, ParentID: req.ParentID, CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(time.Nanosecond), Payload: payload}, {Sequence: 3, ID: req.ParentID + ":" + string(kind) + ":terminal", Kind: runtimeprobe.KindTerminal, ParentID: req.ParentID + ":" + string(kind), CorrelationID: req.CorrelationID, Capability: req.Capability, Status: status, At: now.Add(2 * time.Nanosecond)}}
	return runtimeprobe.ProbeOutcome{Events: events}, err
}

func (c *Client) request(ctx context.Context, method, path string, body any, out any) error {
	if c == nil || strings.TrimSpace(c.BaseURL) == "" {
		return fmt.Errorf("stele endpoint is not configured")
	}
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return err
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, base.String(), reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.APIKey != "" {
		req.Header.Set("X-API-Key", c.APIKey)
	}
	if c.Tenant != "" {
		req.Header.Set("X-Stele-Tenant", c.Tenant)
	}
	if c.Project != "" {
		req.Header.Set("X-Stele-Project", c.Project)
	}
	if c.Namespace != "" {
		req.Header.Set("X-Stele-Namespace", c.Namespace)
	}
	if c.BindingID != "" {
		req.Header.Set("X-Stele-Runtime-Binding", c.BindingID)
	}
	if c.SessionID != "" {
		req.Header.Set("X-Stele-Runtime-Session", c.SessionID)
	}
	if object, ok := body.(map[string]any); ok {
		if metadata, ok := object["metadata"].(map[string]any); ok {
			if key, ok := metadata["idempotency_key"].(string); ok && key != "" {
				req.Header.Set("Idempotency-Key", key)
			}
		}
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusConflict && path == "/v1/provider/events" {
			return fmt.Errorf("%w: %s", ErrIdempotencyConflict, path)
		}
		return fmt.Errorf("stele %s returned %s", path, resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) requestLimited(ctx context.Context, method, path string, body any, out any, maxBytes int) error {
	if maxBytes <= 0 {
		return fmt.Errorf("stele response bound must be positive")
	}
	if c == nil || strings.TrimSpace(c.BaseURL) == "" {
		return fmt.Errorf("stele endpoint is not configured")
	}
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return err
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, base.String(), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("X-API-Key", c.APIKey)
	}
	if c.Tenant != "" {
		req.Header.Set("X-Stele-Tenant", c.Tenant)
	}
	if c.Project != "" {
		req.Header.Set("X-Stele-Project", c.Project)
	}
	if c.Namespace != "" {
		req.Header.Set("X-Stele-Namespace", c.Namespace)
	}
	if c.BindingID != "" {
		req.Header.Set("X-Stele-Runtime-Binding", c.BindingID)
	}
	if c.SessionID != "" {
		req.Header.Set("X-Stele-Runtime-Session", c.SessionID)
	}
	if object, ok := body.(map[string]any); ok {
		if metadata, ok := object["metadata"].(map[string]any); ok {
			if key, ok := metadata["idempotency_key"].(string); ok && key != "" {
				req.Header.Set("Idempotency-Key", key)
			}
		}
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusConflict && path == "/v1/provider/events" {
			return fmt.Errorf("%w: %s", ErrIdempotencyConflict, path)
		}
		return fmt.Errorf("stele %s returned %s", path, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
	if err != nil {
		return err
	}
	if len(data) > maxBytes {
		return fmt.Errorf("stele %s response exceeds %d bytes", path, maxBytes)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode stele %s response: %w", path, err)
	}
	return nil
}

var _ Adapter = (*Client)(nil)
