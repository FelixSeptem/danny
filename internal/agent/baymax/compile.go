package baymax

import (
	"context"
	"fmt"

	"github.com/FelixSeptem/baymax/orchestration/teams"
	"github.com/FelixSeptem/danny/internal/context/assembly"
	"github.com/FelixSeptem/danny/internal/knowledge"
	"github.com/FelixSeptem/danny/internal/runtimeprobe"
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
	if _, err := e.client.ContextAssembly(ctx, runtimeprobe.ProbeRequest{CorrelationID: request.RunID, Input: "compile sections", Capability: request.CapabilityDigest}); err != nil {
		return nil, err
	}
	results := make([]knowledge.CompileChildResult, 0, len(request.SectionIDs))
	for _, sectionID := range request.SectionIDs {
		out, err := e.client.Child(ctx, runtimeprobe.ProbeRequest{CorrelationID: request.RunID + ":" + sectionID, ParentID: request.RunID, Input: string(projection.Serialized) + "\nsection:" + sectionID, Capability: request.CapabilityDigest})
		status := "succeeded"
		if err != nil || len(out.Events) == 0 || out.Events[len(out.Events)-1].Status != runtimeprobe.StatusPassed {
			status = "failed"
		}
		child := knowledge.CompileChildResult{ID: sectionID, Status: status, SourceIDs: []string{sectionID}}
		if err != nil {
			child.Error = err.Error()
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
	engine := teams.New()
	tasks := make([]teams.Task, 0, len(children))
	for _, child := range children {
		current := child
		tasks = append(tasks, teams.Task{TaskID: current.ID, AgentID: current.ID, Role: teams.RoleWorker, Target: teams.TaskTargetLocal, Runner: teams.TaskRunnerFunc(func(context.Context, teams.Task) (teams.TaskResult, error) {
			if current.Status != "succeeded" {
				return teams.TaskResult{}, fmt.Errorf("child %s failed", current.ID)
			}
			return teams.TaskResult{Output: current.ID}, nil
		})})
	}
	_, err := engine.Run(ctx, teams.Plan{RunID: "compile", TeamID: "compile:parallel", Strategy: teams.StrategyParallel, Tasks: tasks})
	return err
}

func (e *CompileExecutor) Vote(ctx context.Context, children []knowledge.CompileChildResult) (knowledge.CompileVoteResult, error) {
	engine := teams.New()
	tasks := make([]teams.Task, 0, len(children))
	for _, child := range children {
		current := child
		tasks = append(tasks, teams.Task{TaskID: current.ID, AgentID: current.ID, Role: teams.RoleWorker, Target: teams.TaskTargetLocal, Runner: teams.TaskRunnerFunc(func(context.Context, teams.Task) (teams.TaskResult, error) {
			return teams.TaskResult{Vote: "approve", Output: current.ID}, nil
		})})
	}
	result, err := engine.Run(ctx, teams.Plan{RunID: "compile", TeamID: "compile:vote", Strategy: teams.StrategyVote, VoteTieBreak: teams.VoteTieBreakFirstTaskID, Tasks: tasks})
	if err != nil {
		return knowledge.CompileVoteResult{}, err
	}
	return knowledge.CompileVoteResult{Approved: result.WinnerVote != "reject"}, nil
}
