// Package filesystem is the one place raw disk I/O happens: listing
// directories and reading/writing files. Everything here is a Temporal
// activity target - see internal/activities. YAML text rendering itself is
// pure and lives in internal/manifest instead.
package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/abradner/workflow/internal/manifest"
)

// Service wraps the filesystem operations the workflow engine needs.
type Service struct{}

// New returns a ready-to-use Service.
func New() *Service { return &Service{} }

// ListDirectories returns every directory under basePath matching pattern
// (a glob, e.g. "wtf-*"), sorted for repeatable output.
func (s *Service) ListDirectories(basePath, pattern string) ([]string, error) {
	if !s.DirectoryExists(basePath) {
		return nil, fmt.Errorf("source directory %s does not exist", basePath)
	}

	matches, err := filepath.Glob(filepath.Join(basePath, pattern))
	if err != nil {
		return nil, err
	}

	var dirs []string
	for _, m := range matches {
		if info, err := os.Stat(m); err == nil && info.IsDir() {
			dirs = append(dirs, m)
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

// Glob returns every path matching pattern, sorted.
func (s *Service) Glob(pattern string) ([]string, error) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// PathEntries lists every direct child of path, sorted.
func (s *Service) PathEntries(path string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(path, "*"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// BaseFilename returns the last path segment.
func (s *Service) BaseFilename(path string) string {
	return filepath.Base(path)
}

// DirectoryExists reports whether path exists and is a directory.
func (s *Service) DirectoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// CreateDirectory makes path and any missing parents.
func (s *Service) CreateDirectory(path string) error {
	return os.MkdirAll(path, 0o755)
}

// ReadFile returns a file's raw content.
func (s *Service) ReadFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

// WriteFile writes content to path, creating/truncating it.
func (s *Service) WriteFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// IsYAML reports whether path looks like a YAML file by extension.
func (s *Service) IsYAML(path string) bool {
	return manifest.IsYAMLPath(path)
}

// ReadYAML parses a YAML file into a generic document tree: map[string]any
// for a mapping, []any for a sequence, or a scalar type.
func (s *Service) ReadYAML(path string) (any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var doc any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return doc, nil
}
