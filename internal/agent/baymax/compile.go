package baymax

import (
	"context"
	"fmt"

	"github.com/FelixSeptem/baymax/orchestration/teams"
	"github.com/FelixSeptem/danny/internal/context/assembly"
	"github.com/FelixSeptem/danny/internal/knowledge"
)

type CompileExecutor struct{ client *Client }

func NewCompileExecutor(client *Client) *CompileExecutor { return &CompileExecutor{client: client} }

func (e *CompileExecutor) Fanout(ctx context.Context, request knowledge.CompileFanoutRequest) ([]knowledge.CompileChildResult, error) {
	if e == nil || e.client == nil {
		return nil, ErrAuthorizedRequest
	}
	projection, err := assembly.NewBuilder(assembly.ProjectionLimits{MaxBytes: 4096}).Build(assembly.ProjectionRequest{Role: assembly.RoleSectionAnalyst, Prefix: "compile.v1", CapabilityDigest: request.CapabilityDigest, Task: "analyze independent sections", References: sectionReferences(request.SectionIDs)})
	if err != nil {
		return nil, err
	}
	descriptor, err := projectionDescriptor(projection, AuthorizedRequest{RunID: request.RunID, PlanDigest: "compile:" + request.RunID})
	if err != nil {
		return nil, err
	}
	results := make([]knowledge.CompileChildResult, 0, len(request.SectionIDs))
	for _, sectionID := range request.SectionIDs {
		runID := request.RunID + ":child:" + sectionID
		out, err := e.client.RunChildAuthorized(ctx, AuthorizedRequest{RunID: runID, ParentRunID: request.RunID, CorrelationID: request.RunID + ":" + sectionID, PlanVersion: "compile.v1", PlanDigest: "compile:" + request.RunID, CapabilityDigest: request.CapabilityDigest, ProjectionDigest: projection.Digest, Role: string(assembly.RoleSectionAnalyst), PerChildBudget: 1000, AggregationLimit: len(request.SectionIDs), Input: string(projection.Serialized) + "\nsection:" + sectionID, HostContextProjection: &descriptor, HostContextResolver: referenceProjectionResolver{}})
		status := out.Terminal.Status
		if status == "" {
			status = "failed"
		}
		child := knowledge.CompileChildResult{ID: sectionID, RunID: runID, ParentRunID: request.RunID, Status: status, Result: out.Terminal.Result, PlanVersion: "compile.v1", CapabilityDigest: request.CapabilityDigest, ProjectionDigest: projection.Digest, SourceIDs: []string{sectionID}}
		if err != nil {
			child.Error = err.Error()
		} else if out.Terminal.FailureClass != "" && status != "succeeded" {
			child.Error = out.Terminal.FailureClass
		}
		results = append(results, child)
	}
	return results, nil
}

func sectionReferences(ids []string) []assembly.Reference {
	refs := make([]assembly.Reference, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, assembly.Reference{ID: id, Kind: "source_section", Locator: id})
	}
	return refs
}

func (e *CompileExecutor) Aggregate(ctx context.Context, children []knowledge.CompileChildResult) error {
	if e == nil || e.client == nil {
		return ErrAuthorizedRequest
	}
	team, err := compileTeamRequest(children, teams.StrategyParallel)
	if err != nil {
		return err
	}
	_, err = e.client.RunTeamAuthorized(ctx, team)
	return err
}

func (e *CompileExecutor) Vote(ctx context.Context, children []knowledge.CompileChildResult) (knowledge.CompileVoteResult, error) {
	if e == nil || e.client == nil {
		return knowledge.CompileVoteResult{}, ErrAuthorizedRequest
	}
	team, err := compileTeamRequest(children, teams.StrategyVote)
	if err != nil {
		return knowledge.CompileVoteResult{}, err
	}
	result, err := e.client.RunTeamAuthorized(ctx, team)
	if err != nil {
		return knowledge.CompileVoteResult{}, err
	}
	evidence := make([]string, 0, len(result.Members))
	for _, member := range result.Members {
		evidence = append(evidence, member.RunID)
	}
	return knowledge.CompileVoteResult{Approved: result.WinnerVote == "approve", EvidenceIDs: evidence, Reason: result.WinnerVote}, nil
}

func compileTeamRequest(children []knowledge.CompileChildResult, strategy teams.Strategy) (TeamRequest, error) {
	if len(children) == 0 || len(children) > 8 {
		return TeamRequest{}, ErrAuthorizedRequest
	}
	parent := children[0].ParentRunID
	planVersion, capability, projection := children[0].PlanVersion, children[0].CapabilityDigest, children[0].ProjectionDigest
	if parent == "" || planVersion == "" || capability == "" || projection == "" {
		return TeamRequest{}, ErrAuthorizedRequest
	}
	members := make([]TeamMember, 0, len(children))
	for _, child := range children {
		if child.ID == "" || child.RunID == "" || child.ParentRunID != parent || child.Status != "succeeded" || child.PlanVersion != planVersion || child.CapabilityDigest != capability || child.ProjectionDigest != projection {
			return TeamRequest{}, fmt.Errorf("child %q cannot participate in compilation team", child.ID)
		}
		input := "review section " + child.ID + " result: " + child.Result
		if strategy == teams.StrategyVote {
			input = "Return exactly approve or reject after reviewing section " + child.ID + " result: " + child.Result
		}
		members = append(members, TeamMember{ID: child.ID, Role: "worker", Input: input})
	}
	name := "parallel"
	if strategy == teams.StrategyVote {
		name = "vote"
	}
	return TeamRequest{RunID: parent, TeamID: parent + ":compile:" + name, CorrelationID: parent + ":compile:" + name, PlanVersion: planVersion, PlanDigest: "compile:" + parent, CapabilityDigest: capability, ProjectionDigest: projection, WriteScopeDigest: "none", EffectNamespace: parent + ":compile-effects", Strategy: strategy, VoteTieBreak: teams.VoteTieBreakFirstTaskID, Members: members}, nil
}
