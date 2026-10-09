package parser

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type StagingStore struct {
	Root string
}

func (s StagingStore) Put(objectID string, source io.Reader) (string, error) {
	if strings.TrimSpace(objectID) == "" || strings.ContainsAny(objectID, `/\\`) || strings.Contains(objectID, "..") {
		return "", ErrInvalidRequest
	}
	root, err := filepath.Abs(s.Root)
	if err != nil {
		return "", fmt.Errorf("parser staging root: %w", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("create parser staging root: %w", err)
	}
	destination := filepath.Join(root, objectID)
	temporary, err := os.CreateTemp(root, ".staging-*")
	if err != nil {
		return "", fmt.Errorf("create parser staging file: %w", err)
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if _, err := io.Copy(temporary, source); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("write parser staging file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close parser staging file: %w", err)
	}
	if err := os.Chmod(temporaryName, 0o600); err != nil {
		return "", fmt.Errorf("protect parser staging file: %w", err)
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return "", fmt.Errorf("publish parser staging object: %w", err)
	}
	return objectID, nil
}

func NewObjectID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate parser staging object id: %w", err)
	}
	return fmt.Sprintf("obj-%x", bytes[:]), nil
}

func (s StagingStore) Remove(objectID string) error {
	if strings.TrimSpace(objectID) == "" || strings.ContainsAny(objectID, `/\\`) || strings.Contains(objectID, "..") {
		return ErrInvalidRequest
	}
	return os.Remove(filepath.Join(s.Root, objectID))
}
