package knowledge

import "context"

type PlanCommitter interface {
	Commit(context.Context, CompilationPlan) error
}
type CompilationService struct {
	compiler  Compiler
	committer PlanCommitter
}
type CompileResult struct {
	Plan      CompilationPlan
	Committed bool
}

func NewCompilationService(compiler Compiler, committer PlanCommitter) *CompilationService {
	return &CompilationService{compiler: compiler, committer: committer}
}

func (s *CompilationService) Compile(ctx context.Context, request CompileRequest) (CompileResult, error) {
	plan, err := s.compiler.Plan(ctx, request)
	if err != nil {
		return CompileResult{Plan: plan}, err
	}
	if err := s.Commit(ctx, plan); err != nil {
		return CompileResult{Plan: plan}, err
	}
	return CompileResult{Plan: plan, Committed: true}, nil
}

func (s *CompilationService) Commit(ctx context.Context, plan CompilationPlan) error {
	if s == nil || s.committer == nil {
		return ErrCompilationInvalid
	}
	if err := plan.ValidateError(); err != nil {
		return err
	}
	return s.committer.Commit(ctx, plan)
}
