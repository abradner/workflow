// Package workspaceextractor loads an app's base/ and source-overlay/ files
// off disk into an in-memory manifest.Workspace, ready for the pure
// transformer pipeline to work on.
package workspaceextractor

import (
	"path/filepath"
	"strings"

	"github.com/abradner/workflow/internal/manifest"
	"github.com/abradner/workflow/internal/services/filesystem"
)

// FS is the subset of filesystem.Service the extractor depends on.
type FS interface {
	DirectoryExists(path string) bool
	PathEntries(path string) ([]string, error)
	IsYAML(path string) bool
	ReadYAML(path string) (any, error)
	ReadFile(path string) (string, error)
}

// Extractor loads an app's manifests off disk into a Workspace.
type Extractor struct {
	SourceDir  string
	SourceEnv  string
	TargetEnvs []string
	FS         FS
}

// New builds an Extractor. fs defaults to a real filesystem.Service if nil.
func New(sourceDir, sourceEnv string, targetEnvs []string, fs FS) *Extractor {
	if fs == nil {
		fs = filesystem.New()
	}
	return &Extractor{SourceDir: sourceDir, SourceEnv: sourceEnv, TargetEnvs: targetEnvs, FS: fs}
}

// Extract loads base/ and overlay/<source_env>/ for appName into a fresh Workspace.
func (e *Extractor) Extract(appName string) (*manifest.Workspace, error) {
	ws := manifest.New(appName, e.SourceEnv, e.TargetEnvs)

	basePath := filepath.Join(e.SourceDir, appName, "base")
	if err := e.loadPath(ws, basePath, "base", basePath); err != nil {
		return nil, err
	}

	overlayPath := filepath.Join(e.SourceDir, appName, "overlay", e.SourceEnv)
	if err := e.loadPath(ws, overlayPath, "overlay/"+e.SourceEnv, overlayPath); err != nil {
		return nil, err
	}

	return ws, nil
}

func (e *Extractor) loadPath(ws *manifest.Workspace, fsPath, virtualPrefix, baseFsPath string) error {
	if !e.FS.DirectoryExists(fsPath) {
		return nil
	}

	entries, err := e.FS.PathEntries(fsPath)
	if err != nil {
		return err
	}

	for _, srcFile := range entries {
		if e.FS.DirectoryExists(srcFile) {
			if err := e.loadPath(ws, srcFile, virtualPrefix, baseFsPath); err != nil {
				return err
			}
			continue
		}

		relativeFilename := strings.TrimPrefix(srcFile, baseFsPath+"/")
		virtualPath := virtualPrefix + "/" + relativeFilename

		if e.FS.IsYAML(relativeFilename) {
			content, err := e.FS.ReadYAML(srcFile)
			if err != nil {
				return err
			}
			ws.Manifests[virtualPath] = content
		} else {
			content, err := e.FS.ReadFile(srcFile)
			if err != nil {
				return err
			}
			ws.Manifests[virtualPath] = content
		}
	}

	return nil
}
