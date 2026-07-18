// Package op wraps the 1Password CLI (`op`). The CLI remains the practical
// choice here even in the Go port: 1Password's official Go SDK targets
// read-only Secrets Automation, not creating arbitrary multi-section Secure
// Note items the way this tool needs.
package op

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Runner executes a command and captures its output - swappable in tests so
// they don't need a real `op` binary on PATH.
type Runner interface {
	Run(ctx context.Context, name string, args []string, stdin []byte) (stdout, stderr string, err error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args []string, stdin []byte) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// Client wraps 1Password CLI operations.
type Client struct {
	runner Runner
}

// New builds a Client that shells out to the real `op` binary.
func New() *Client { return &Client{runner: execRunner{}} }

// NewWithRunner builds a Client around a custom Runner - used in tests.
func NewWithRunner(r Runner) *Client { return &Client{runner: r} }

// CreateItem pipes item's JSON encoding to `op item create -`.
func (c *Client) CreateItem(ctx context.Context, item map[string]any) (string, error) {
	payload, err := json.Marshal(item)
	if err != nil {
		return "", fmt.Errorf("encoding 1Password item: %w", err)
	}

	stdout, stderr, err := c.runner.Run(ctx, "op", []string{"item", "create", "-"}, payload)
	if err != nil {
		return "", fmt.Errorf("failed to create 1P item: %s", strings.TrimSpace(stderr))
	}
	return strings.TrimSpace(stdout), nil
}

// ReadNote returns the notesPlain field of a Secure Note item.
func (c *Client) ReadNote(ctx context.Context, itemID string) (string, error) {
	stdout, stderr, err := c.runner.Run(ctx, "op", []string{"item", "get", itemID, "--fields", "notesPlain"}, nil)
	if err != nil {
		return "", fmt.Errorf("failed to read 1P item %s: %s", itemID, strings.TrimSpace(stderr))
	}

	content := strings.TrimSpace(stdout)
	// op sometimes wraps output in double quotes with escaped newlines.
	if len(content) >= 2 && strings.HasPrefix(content, `"`) && strings.HasSuffix(content, `"`) {
		content = strings.ReplaceAll(content[1:len(content)-1], `\n`, "\n")
	}
	return content, nil
}
