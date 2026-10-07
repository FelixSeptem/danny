package runtimeprobe

import "context"

type ProbeRequest struct {
	CorrelationID string
	ParentID      string
	Input         string
	Capability    string
}

type ProbeOutcome struct {
	Events []Event
}

type BaymaxAdapter interface {
	Version() string
	Run(context.Context, ProbeRequest) (ProbeOutcome, error)
	Stream(context.Context, ProbeRequest) (ProbeOutcome, error)
	Child(context.Context, ProbeRequest) (ProbeOutcome, error)
	TeamParallel(context.Context, ProbeRequest) (ProbeOutcome, error)
	TeamVote(context.Context, ProbeRequest) (ProbeOutcome, error)
	Mailbox(context.Context, ProbeRequest) (ProbeOutcome, error)
	Scheduler(context.Context, ProbeRequest) (ProbeOutcome, error)
	ContextAssembly(context.Context, ProbeRequest) (ProbeOutcome, error)
}

type SteleAdapter interface {
	Version() string
	Context(context.Context, ProbeRequest) (ProbeOutcome, error)
	Memory(context.Context, ProbeRequest) (ProbeOutcome, error)
	References(context.Context, ProbeRequest) (ProbeOutcome, error)
	Compilation(context.Context, ProbeRequest) (ProbeOutcome, error)
	RestartProjection(context.Context, ProbeRequest) (ProbeOutcome, error)
	IdempotentMemory(context.Context, ProbeRequest) (ProbeOutcome, error)
}
