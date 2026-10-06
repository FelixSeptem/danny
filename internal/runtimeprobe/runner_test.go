package runtimeprobe_test

import (
	"context"
	"testing"

	"github.com/FelixSeptem/baymax/core/types"
	"github.com/FelixSeptem/baymax/integration/fakes"
	"github.com/FelixSeptem/danny/internal/agent/baymax"
	"github.com/FelixSeptem/danny/internal/runtimeprobe"
)

func TestOfflineRunnerUsesFakeBaymaxAndExplicitlySkipsUnconfiguredStele(t *testing.T) {
	model := fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "ok"}}, {Response: types.ModelResponse{FinalAnswer: "ok"}}})
	adapter, err := baymax.New(model)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := (runtimeprobe.OfflineRunner{Baymax: adapter}).Run(context.Background(), "bounded input")
	if err != nil {
		t.Fatal(err)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatal(err)
	}
	var skipped bool
	for _, event := range bundle.Events {
		if event.Capability == "context" && event.Status == runtimeprobe.StatusSkippedUnconfigured {
			skipped = true
		}
	}
	if !skipped {
		t.Fatal("missing explicit unconfigured Stele result")
	}
}
