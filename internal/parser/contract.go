package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

const ContractVersion = "v1"

var (
	ErrInvalidRequest     = errors.New("parser: invalid request")
	ErrUnsupportedVersion = errors.New("parser: unsupported contract version")
	ErrUnsupportedFormat  = errors.New("parser: unsupported format")
	ErrNotFound           = errors.New("parser: job not found")
	ErrConflict           = errors.New("parser: idempotency conflict")
)

type Status string

const (
	StatusAccepted       Status = "accepted"
	StatusRunning        Status = "running"
	StatusSucceeded      Status = "succeeded"
	StatusUnsupported    Status = "unsupported"
	StatusResourceLimit  Status = "resource_limited"
	StatusTimeout        Status = "timeout"
	StatusProviderFailed Status = "provider_failed"
	StatusCancelled      Status = "cancelled"
	StatusFailed         Status = "failed"
)

func (s Status) Terminal() bool {
	switch s {
	case StatusSucceeded, StatusUnsupported, StatusResourceLimit, StatusTimeout, StatusProviderFailed, StatusCancelled, StatusFailed:
		return true
	default:
		return false
	}
}

type ProviderMode string

const (
	ProviderLocal          ProviderMode = "local"
	ProviderPageIndexCloud ProviderMode = "pageindex_cloud"
)

type Policy struct {
	PDFPageThreshold    int   `json:"pdf_page_threshold,omitempty"`
	AllowPageIndexCloud bool  `json:"allow_pageindex_cloud,omitempty"`
	TimeoutSeconds      int   `json:"timeout_seconds,omitempty"`
	MaxBytes            int64 `json:"max_bytes,omitempty"`
	MaxPages            int   `json:"max_pages,omitempty"`
}

type Request struct {
	ContractVersion   string `json:"contract_version"`
	JobID             string `json:"job_id"`
	IdempotencyKey    string `json:"idempotency_key"`
	StagingObjectID   string `json:"staging_object_id"`
	SourceFingerprint string `json:"source_fingerprint"`
	ContentHash       string `json:"content_hash"`
	Filename          string `json:"filename"`
	FormatHint        string `json:"format_hint,omitempty"`
	Policy            Policy `json:"policy,omitempty"`
}

func NewRequest(jobID, idempotencyKey, stagingObjectID, sourceFingerprint, contentHash, filename string) Request {
	return Request{ContractVersion: ContractVersion, JobID: jobID, IdempotencyKey: idempotencyKey, StagingObjectID: stagingObjectID, SourceFingerprint: sourceFingerprint, ContentHash: contentHash, Filename: filename}
}

type ImageReference struct {
	Path string `json:"path"`
	Alt  string `json:"alt,omitempty"`
}

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Failure struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type Result struct {
	JobID             string           `json:"job_id"`
	ContractVersion   string           `json:"contract_version"`
	SourceFingerprint string           `json:"source_fingerprint"`
	ContentHash       string           `json:"content_hash"`
	Filename          string           `json:"filename"`
	DetectedFormat    string           `json:"detected_format"`
	ParserVersion     string           `json:"parser_version"`
	ProviderMode      ProviderMode     `json:"provider_mode"`
	CanonicalMarkdown string           `json:"canonical_markdown"`
	Pages             int              `json:"pages,omitempty"`
	Sections          []Section        `json:"sections,omitempty"`
	Images            []ImageReference `json:"images,omitempty"`
	Warnings          []Warning        `json:"warnings,omitempty"`
	UntrustedSource   bool             `json:"untrusted_source"`
	Failure           *Failure         `json:"failure,omitempty"`
}

type Section struct {
	Title     string `json:"title"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

type Job struct {
	Request
	Status        Status       `json:"status"`
	AttemptCount  int          `json:"attempt_count"`
	Result        *Result      `json:"result,omitempty"`
	Failure       *Failure     `json:"failure,omitempty"`
	ParserVersion string       `json:"parser_version,omitempty"`
	ProviderMode  ProviderMode `json:"provider_mode,omitempty"`
	CleanupState  string       `json:"cleanup_state,omitempty"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
}

func (r Request) Validate() error {
	if r.ContractVersion == "" {
		r.ContractVersion = ContractVersion
	}
	if r.ContractVersion != ContractVersion {
		return ErrUnsupportedVersion
	}
	if strings.TrimSpace(r.JobID) == "" || strings.TrimSpace(r.IdempotencyKey) == "" || strings.TrimSpace(r.StagingObjectID) == "" || strings.TrimSpace(r.SourceFingerprint) == "" || strings.TrimSpace(r.ContentHash) == "" || strings.TrimSpace(r.Filename) == "" {
		return ErrInvalidRequest
	}
	if strings.ContainsAny(r.StagingObjectID, `/\\`) || strings.Contains(r.StagingObjectID, "..") {
		return ErrInvalidRequest
	}
	if r.Policy.PDFPageThreshold < 0 || r.Policy.MaxBytes < 0 || r.Policy.MaxPages < 0 || r.Policy.TimeoutSeconds < 0 {
		return ErrInvalidRequest
	}
	return nil
}

func HashBytes(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
