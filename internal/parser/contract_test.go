package parser

import (
	"errors"
	"testing"
)

func TestRequestRejectsUnsupportedVersionAndHostPaths(t *testing.T) {
	base := Request{ContractVersion: ContractVersion, JobID: "job-1", IdempotencyKey: "key-1", StagingObjectID: "object-1", SourceFingerprint: "source-1", ContentHash: "hash-1", Filename: "note.md"}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	base.ContractVersion = "v2"
	if !errors.Is(base.Validate(), ErrUnsupportedVersion) {
		t.Fatalf("version error = %v", base.Validate())
	}
	base.ContractVersion = ContractVersion
	base.StagingObjectID = `../../etc/passwd`
	if !errors.Is(base.Validate(), ErrInvalidRequest) {
		t.Fatalf("path request error = %v", base.Validate())
	}
}

func TestStatusTerminal(t *testing.T) {
	if !StatusSucceeded.Terminal() || StatusRunning.Terminal() {
		t.Fatal("terminal status classification is incorrect")
	}
}
