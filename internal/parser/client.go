package parser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Client interface {
	Submit(context.Context, Request) (Job, error)
	Status(context.Context, string) (Job, error)
	Result(context.Context, string) (Result, error)
	Cancel(context.Context, string) error
}

type HTTPClient struct {
	BaseURL string
	Client  *http.Client
}

func (c HTTPClient) httpClient() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c HTTPClient) Submit(ctx context.Context, request Request) (Job, error) {
	if err := request.Validate(); err != nil {
		return Job{}, err
	}
	var job Job
	if err := c.doJSON(ctx, http.MethodPost, "/v1/parser/jobs", request, &job); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (c HTTPClient) Status(ctx context.Context, id string) (Job, error) {
	var job Job
	if err := c.doJSON(ctx, http.MethodGet, "/v1/parser/jobs/"+escapeID(id), nil, &job); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (c HTTPClient) Result(ctx context.Context, id string) (Result, error) {
	var result Result
	if err := c.doJSON(ctx, http.MethodGet, "/v1/parser/jobs/"+escapeID(id)+"/result", nil, &result); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (c HTTPClient) Cancel(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodPost, "/v1/parser/jobs/"+escapeID(id)+"/cancel", struct{}{}, nil)
}

func (c HTTPClient) doJSON(ctx context.Context, method, path string, input, output any) error {
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		return fmt.Errorf("parser: HTTP base URL is required")
	}
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("parser: encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return fmt.Errorf("parser: create HTTP request: %w", err)
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("parser: HTTP request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure Failure
		_ = json.NewDecoder(response.Body).Decode(&failure)
		if failure.Code != "" {
			return fmt.Errorf("parser: HTTP %s: %s", failure.Code, failure.Message)
		}
		return fmt.Errorf("parser: HTTP status %s", response.Status)
	}
	if output == nil {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(output); err != nil {
		return fmt.Errorf("parser: decode HTTP response: %w", err)
	}
	return nil
}

type CLIClient struct {
	Command     string
	StagingRoot string
	WorkDir     string
}

func (c CLIClient) Submit(ctx context.Context, request Request) (Job, error) {
	if err := request.Validate(); err != nil {
		return Job{}, err
	}
	requestPath, resultPath, err := c.writeRequest(request)
	if err != nil {
		return Job{}, err
	}
	defer func() { _ = os.Remove(requestPath) }()
	defer func() { _ = os.Remove(resultPath) }()
	cmd := exec.CommandContext(ctx, c.command(), "--request", requestPath, "--result", resultPath, "--staging-root", c.StagingRoot)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return Job{}, fmt.Errorf("parser CLI: %w: %s", err, strings.TrimSpace(string(output)))
	}
	data, err := os.ReadFile(resultPath)
	if err != nil {
		return Job{}, fmt.Errorf("parser CLI result: %w", err)
	}
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return Job{}, fmt.Errorf("parser CLI decode result: %w", err)
	}
	return job, nil
}

func (c CLIClient) Status(context.Context, string) (Job, error) {
	return Job{}, errors.New("parser CLI does not provide remote status; use the durable application record")
}

func (c CLIClient) Result(context.Context, string) (Result, error) {
	return Result{}, errors.New("parser CLI does not provide remote result lookup; use Submit result")
}

func (c CLIClient) Cancel(context.Context, string) error {
	return errors.New("parser CLI cancellation is controlled by the process context")
}

func (c CLIClient) command() string {
	if strings.TrimSpace(c.Command) != "" {
		return c.Command
	}
	return "danny-parser"
}

func (c CLIClient) writeRequest(request Request) (string, string, error) {
	dir := c.WorkDir
	if dir == "" {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	requestFile, err := os.CreateTemp(dir, "danny-parser-request-*.json")
	if err != nil {
		return "", "", err
	}
	resultFile, err := os.CreateTemp(dir, "danny-parser-result-*.json")
	if err != nil {
		_ = requestFile.Close()
		_ = os.Remove(requestFile.Name())
		return "", "", err
	}
	encoded, err := json.Marshal(request)
	if err == nil {
		_, err = requestFile.Write(encoded)
	}
	if closeErr := requestFile.Close(); err == nil {
		err = closeErr
	}
	if closeErr := resultFile.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(requestFile.Name())
		_ = os.Remove(resultFile.Name())
		return "", "", err
	}
	return filepath.Clean(requestFile.Name()), filepath.Clean(resultFile.Name()), nil
}

func escapeID(id string) string {
	return strings.NewReplacer("/", "%2F", "?", "%3F", "#", "%23").Replace(id)
}
