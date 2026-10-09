package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/FelixSeptem/danny/internal/parser"
)

var (
	ErrSourcePathRequired = errors.New("knowledge: source path is required")
	ErrSourceEmpty        = errors.New("knowledge: source content is empty")
	ErrInvalidSourceRange = errors.New("knowledge: invalid source range")
)

type SourceInput struct {
	Path              string           `json:"path"`
	Content           string           `json:"content"`
	Format            string           `json:"format,omitempty"`
	ParserVersion     string           `json:"parser_version,omitempty"`
	ProviderMode      string           `json:"provider_mode,omitempty"`
	SourceFingerprint string           `json:"source_fingerprint,omitempty"`
	ParserStatus      string           `json:"parser_status,omitempty"`
	ParserFailure     string           `json:"parser_failure,omitempty"`
	Warnings          []parser.Warning `json:"warnings,omitempty"`
	UntrustedSource   bool             `json:"untrusted_source,omitempty"`
}

type SourceRange struct {
	StartLine int `json:"start_line"`
	EndLine   int `json:"end_line"`
}

type Section struct {
	ID          string      `json:"id"`
	SourceID    string      `json:"source_id"`
	Title       string      `json:"title"`
	Level       int         `json:"level"`
	HeadingPath []string    `json:"heading_path"`
	ParentID    string      `json:"parent_id,omitempty"`
	Range       SourceRange `json:"range"`
	Content     string      `json:"content,omitempty"`
}

type SourceMetadata struct {
	SourceID          string           `json:"source_id"`
	Path              string           `json:"path"`
	ContentHash       string           `json:"content_hash"`
	Content           []byte           `json:"content,omitempty"`
	Sections          []Section        `json:"sections,omitempty"`
	Format            string           `json:"format,omitempty"`
	ParserVersion     string           `json:"parser_version,omitempty"`
	ProviderMode      string           `json:"provider_mode,omitempty"`
	SourceFingerprint string           `json:"source_fingerprint,omitempty"`
	ParserStatus      string           `json:"parser_status,omitempty"`
	ParserFailure     string           `json:"parser_failure,omitempty"`
	Warnings          []parser.Warning `json:"warnings,omitempty"`
	UntrustedSource   bool             `json:"untrusted_source"`
}

type SourceDecisionKind string

const (
	SourceAdded     SourceDecisionKind = "added"
	SourceChanged   SourceDecisionKind = "changed"
	SourceUnchanged SourceDecisionKind = "unchanged"
	SourceRemoved   SourceDecisionKind = "removed"
)

type SourceDecision struct {
	Kind       SourceDecisionKind `json:"kind"`
	Source     SourceMetadata     `json:"source,omitempty"`
	PreviousID string             `json:"previous_id,omitempty"`
}

var headingPattern = regexp.MustCompile(`^(#{1,6})[ \t]+(.+?)\s*$`)

func NormalizeSource(input SourceInput) (SourceMetadata, error) {
	path := filepath.ToSlash(filepath.Clean(strings.TrimSpace(input.Path)))
	if path == "." || path == "" || path == ".." || strings.HasPrefix(path, "../") {
		return SourceMetadata{}, ErrSourcePathRequired
	}
	content := strings.ReplaceAll(strings.ReplaceAll(input.Content, "\r\n", "\n"), "\r", "\n")
	if strings.TrimSpace(content) == "" {
		return SourceMetadata{}, ErrSourceEmpty
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	hash := sha256.Sum256([]byte(content))
	contentHash := hex.EncodeToString(hash[:])
	sourceID := "source-" + contentHash[:24]
	sections := parseSections(sourceID, content)
	return SourceMetadata{SourceID: sourceID, Path: path, ContentHash: contentHash, Content: []byte(content), Sections: sections, Format: "md", ParserVersion: "danny-native", ProviderMode: "local", UntrustedSource: true}, nil
}

func NormalizeParsedSource(path string, result parser.Result) (SourceMetadata, error) {
	metadata, err := NormalizeSource(SourceInput{Path: path, Content: result.CanonicalMarkdown, Format: result.DetectedFormat, ParserVersion: result.ParserVersion, ProviderMode: string(result.ProviderMode), SourceFingerprint: result.SourceFingerprint, Warnings: result.Warnings, UntrustedSource: result.UntrustedSource})
	if err != nil {
		return SourceMetadata{}, err
	}
	metadata.ContentHash = result.ContentHash
	metadata.SourceFingerprint = result.SourceFingerprint
	metadata.Format = result.DetectedFormat
	metadata.ParserVersion = result.ParserVersion
	metadata.ProviderMode = string(result.ProviderMode)
	metadata.Warnings = append([]parser.Warning(nil), result.Warnings...)
	metadata.UntrustedSource = true
	return metadata, nil
}

func parseSections(sourceID, content string) []Section {
	lines := strings.Split(content, "\n")
	type marker struct {
		title       string
		level, line int
		path        []string
	}
	markers := make([]marker, 0)
	stack := make([]marker, 0, 6)
	for i, line := range lines {
		match := headingPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		level := len(match[1])
		for len(stack) > 0 && stack[len(stack)-1].level >= level {
			stack = stack[:len(stack)-1]
		}
		path := make([]string, 0, len(stack)+1)
		for _, parent := range stack {
			path = append(path, parent.title)
		}
		path = append(path, strings.TrimSpace(match[2]))
		current := marker{title: strings.TrimSpace(match[2]), level: level, line: i + 1, path: path}
		markers = append(markers, current)
		stack = append(stack, current)
	}
	sections := make([]Section, 0, len(markers))
	ids := make([]string, len(markers))
	pathCounts := map[string]int{}
	for i, current := range markers {
		key := strings.Join(current.path, "\x1f")
		pathCounts[key]++
		ids[i] = sectionID(sourceID, append(current.path, fmt.Sprint(pathCounts[key])), current.line)
	}
	for i, current := range markers {
		end := len(lines)
		for j := i + 1; j < len(markers); j++ {
			if markers[j].level <= current.level {
				end = markers[j].line - 1
				break
			}
		}
		parentID := ""
		for j := i - 1; j >= 0; j-- {
			if markers[j].level < current.level {
				parentID = ids[j]
				break
			}
		}
		id := ids[i]
		bodyStart := current.line
		bodyEnd := end
		if bodyStart < 1 {
			bodyStart = 1
		}
		if bodyEnd < bodyStart {
			bodyEnd = bodyStart
		}
		body := strings.Join(lines[bodyStart:bodyEnd], "\n")
		sections = append(sections, Section{ID: id, SourceID: sourceID, Title: current.title, Level: current.level, HeadingPath: current.path, ParentID: parentID, Range: SourceRange{StartLine: current.line, EndLine: end}, Content: strings.TrimSpace(body)})
	}
	return sections
}

func sectionID(sourceID string, path []string, line int) string {
	sum := sha256.Sum256([]byte(sourceID + "\x00" + strings.Join(path, "\x1f") + fmt.Sprint(line)))
	return "section-" + hex.EncodeToString(sum[:])[:24]
}

func PlanScan(previous map[string]SourceMetadata, inputs []SourceInput, explicitScope bool) ([]SourceDecision, error) {
	current := make(map[string]SourceMetadata, len(inputs))
	for _, input := range inputs {
		metadata, err := NormalizeSource(input)
		if err != nil {
			return nil, err
		}
		if _, exists := current[metadata.Path]; exists {
			return nil, fmt.Errorf("duplicate source path %q", metadata.Path)
		}
		current[metadata.Path] = metadata
	}
	paths := make([]string, 0, len(current)+len(previous))
	seen := map[string]struct{}{}
	for path := range current {
		paths = append(paths, path)
		seen[path] = struct{}{}
	}
	if explicitScope {
		for path := range previous {
			if _, ok := seen[path]; !ok {
				paths = append(paths, path)
			}
		}
	}
	// lexical ordering makes the plan and its effect key deterministic.
	for i := 1; i < len(paths); i++ {
		for j := i; j > 0 && paths[j] < paths[j-1]; j-- {
			paths[j], paths[j-1] = paths[j-1], paths[j]
		}
	}
	decisions := make([]SourceDecision, 0, len(paths))
	for _, path := range paths {
		metadata, present := current[path]
		old, hadOld := previous[path]
		switch {
		case !present && hadOld:
			decisions = append(decisions, SourceDecision{Kind: SourceRemoved, Source: old, PreviousID: old.SourceID})
		case present && !hadOld:
			decisions = append(decisions, SourceDecision{Kind: SourceAdded, Source: metadata})
		case metadata.ContentHash == old.ContentHash:
			decisions = append(decisions, SourceDecision{Kind: SourceUnchanged, Source: metadata, PreviousID: old.SourceID})
		default:
			decisions = append(decisions, SourceDecision{Kind: SourceChanged, Source: metadata, PreviousID: old.SourceID})
		}
	}
	return decisions, nil
}
