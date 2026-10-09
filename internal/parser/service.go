package parser

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"time"
)

type Service struct {
	Client      Client
	Staging     StagingStore
	PollEvery   time.Duration
	PollTimeout time.Duration
}

func (s Service) Parse(ctx context.Context, filename string, source io.Reader) (Result, error) {
	content, err := io.ReadAll(source)
	if err != nil {
		return Result{}, fmt.Errorf("parser read source: %w", err)
	}
	objectID, err := NewObjectID()
	if err != nil {
		return Result{}, err
	}
	if _, err := s.Staging.Put(objectID, bytes.NewReader(content)); err != nil {
		return Result{}, err
	}
	defer func() { _ = s.Staging.Remove(objectID) }()
	hash := sha256.Sum256(content)
	contentHash := fmt.Sprintf("%x", hash[:])
	request := NewRequest(objectID, "parse:"+contentHash, objectID, "source:"+contentHash[:24], contentHash, filename)
	job, err := s.Client.Submit(ctx, request)
	if err != nil {
		return Result{}, err
	}
	if job.Result != nil && job.Status == StatusSucceeded {
		return *job.Result, nil
	}
	if _, ok := s.Client.(CLIClient); ok {
		if job.Status != StatusSucceeded {
			return Result{}, fmt.Errorf("parser: terminal status %s", job.Status)
		}
	}
	return s.wait(ctx, job.JobID)
}

func (s Service) wait(ctx context.Context, jobID string) (Result, error) {
	interval := s.PollEvery
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	timeout := s.PollTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		job, err := s.Client.Status(waitCtx, jobID)
		if err != nil {
			return Result{}, err
		}
		if job.Status.Terminal() {
			if job.Status != StatusSucceeded {
				if job.Failure != nil {
					return Result{}, fmt.Errorf("parser: %s: %s", job.Failure.Code, job.Failure.Message)
				}
				return Result{}, fmt.Errorf("parser: terminal status %s", job.Status)
			}
			return s.Client.Result(waitCtx, jobID)
		}
		select {
		case <-waitCtx.Done():
			return Result{}, waitCtx.Err()
		case <-time.After(interval):
		}
	}
}
