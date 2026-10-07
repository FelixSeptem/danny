package knowledge

import (
	"context"
	"errors"
	"sync"

	agentpolicy "github.com/FelixSeptem/danny/internal/agent/knowledge"
	"github.com/google/uuid"
)

var ErrPlanNotAdmitted = errors.New("knowledge: compilation plan was not admitted")

type CompilationAdmission struct {
	mu     sync.Mutex
	plans  map[string]CompilationPlan
	Invoke func(context.Context, CompilationPlan) error
}

func NewCompilationAdmission() *CompilationAdmission {
	return &CompilationAdmission{plans: map[string]CompilationPlan{}}
}

func (a *CompilationAdmission) Register(registry *agentpolicy.ToolRegistry) error {
	return registry.Register(agentpolicy.ToolDefinition{
		Name: "commit_compilation", Policy: agentpolicy.PolicyWrite, WriteScope: agentpolicy.WriteScopeUser,
		Invoke: func(ctx context.Context, input map[string]any) error {
			token, _ := input["admission_token"].(string)
			if token == "" {
				return ErrPlanNotAdmitted
			}
			a.mu.Lock()
			plan, ok := a.plans[token]
			a.mu.Unlock()
			if !ok {
				return ErrPlanNotAdmitted
			}
			if a.Invoke == nil {
				return nil
			}
			return a.Invoke(ctx, plan)
		},
	})
}

func (a *CompilationAdmission) Admit(plan CompilationPlan) string {
	if plan.ValidateError() != nil {
		return ""
	}
	token := uuid.NewString()
	a.mu.Lock()
	a.plans[token] = plan
	a.mu.Unlock()
	return token
}

func (a *CompilationAdmission) Revoke(token string) {
	a.mu.Lock()
	delete(a.plans, token)
	a.mu.Unlock()
}
