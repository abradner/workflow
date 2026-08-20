// Package configload is the platform's environment-driven configuration
// harness: a consumer defines its own Config struct with `env:` tags
// (caarlos0/env) and calls Load, which reads a .env file first if one
// exists (missing is fine) and then parses the process environment.
//
// The platform convention is that config loads on the WORKER, not the CLI
// client - expose the consumer's Load inside a LoadConfig activity that
// every workflow calls as its first step, so Config always reflects the
// machine the worker actually runs on. Credential fields must be blanked
// in that activity's result: activity results land in Temporal's durable,
// plaintext event history.
package configload

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// Load reads a T from the environment, loading a .env file first if one
// exists. The .env file is resolved against the process's current working
// directory - which, per the platform convention above, is the WORKER's
// working directory (in a container, wherever its entrypoint left cwd), not
// wherever the CLI was invoked. Missing is fine; a present-but-elsewhere
// .env silently isn't loaded, so prefer real environment variables in
// deployed workers. A .env that exists but cannot be read or parsed is an
// error - silently falling back to the ambient environment would be the
// hardest misconfiguration to diagnose.
func Load[T any]() (*T, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("loading .env: %w", err)
	}

	cfg := new(T)
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	return cfg, nil
}

// ExpandPath resolves a leading ~ (bare "~" or "~/...", NOT "~user/...")
// and relative segments to an absolute path, mirroring Ruby's
// File.expand_path - for config fields that name directories. An empty
// path resolves to the working directory: env's `required` means set, not
// non-empty, so a `SOURCE_DIR=` line yields the worker's cwd here rather
// than an error - callers wanting stricter semantics must validate
// non-emptiness themselves.
func ExpandPath(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	return filepath.Abs(path)
}
