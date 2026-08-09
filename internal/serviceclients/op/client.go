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

// CreateItem pipes item's JSON encoding to `op item create -`, into vault.
//
// vault must be non-empty. Without --vault the CLI silently files the item in
// the account's *personal* vault - no error, no warning - which is how this
// tool spent its early life writing per-environment Secure Notes somewhere
// only the operator who ran it could read. Callers validate before getting
// here; this is the backstop.
//
// The category is deliberately not passed as a flag: it travels in the piped
// template, and `op` rejects being given it in both places. See
// docs/OP_CLI_NOTES.md.
func (c *Client) CreateItem(ctx context.Context, item map[string]any, vault string) (string, error) {
	if vault == "" {
		return "", fmt.Errorf("refusing to create a 1Password item with no vault: it would land in the personal vault")
	}

	payload, err := json.Marshal(item)
	if err != nil {
		return "", fmt.Errorf("encoding 1Password item: %w", err)
	}

	stdout, stderr, err := c.runner.Run(ctx, "op", []string{"item", "create", "--vault", vault, "-"}, payload)
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

// GetItem returns the item titled or identified by ref, exactly as
// `op item get --format json` reports it, or nil if no such item exists.
//
// A miss is not an error. `op` distinguishes them by exit status - not-found
// is exit 1 with a message on stderr, never empty output on exit 0 - and
// sync-1p's whole upsert flow depends on telling "no item yet, create one"
// apart from "the CLI failed". Callers get (nil, nil) for the former.
//
// vault may be empty when ref is an item ID, which is globally unique. It
// should not be when ref is a title: titles are only unique within a vault,
// and resolving one across an account is chance, not design.
func (c *Client) GetItem(ctx context.Context, ref, vault string) (map[string]any, error) {
	args := []string{"item", "get", ref, "--format", "json"}
	if vault != "" {
		args = append(args, "--vault", vault)
	}

	stdout, stderr, err := c.runner.Run(ctx, "op", args, nil)
	if err != nil {
		if isNotFound(stderr) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read 1P item %s: %s", ref, strings.TrimSpace(stderr))
	}

	var item map[string]any
	// UseNumber so a large numeric field survives the round trip verbatim
	// rather than going through float64 - the same trap documented on
	// onepassword.parseFlatJSONObject, and considerably worse here because
	// whatever we decode is written straight back to the vault.
	dec := json.NewDecoder(strings.NewReader(stdout))
	dec.UseNumber()
	if err := dec.Decode(&item); err != nil {
		return nil, fmt.Errorf("decoding 1Password item %s: %w", ref, err)
	}
	return item, nil
}

// EditItem writes item back, replacing the stored item wholesale.
//
// Two things make this sharper than it looks, both verified against op 2.35.0
// and recorded in docs/OP_CLI_NOTES.md:
//
//   - The payload must be a *round-tripped* item - the structure GetItem
//     returned, sent back whole. A hand-assembled subset is rejected outright:
//     the observed failure was "Item updatedAt must be > 1970-01-01" for a
//     payload carrying only title/category/sections/fields. Exactly which
//     fields the validator requires was NOT established - only that omitting
//     the metadata fails - so the fake checks for id and updated_at, the two
//     the failure named. Send the whole thing back and the question does not
//     arise, which is why the domain model wraps what the CLI gave it rather
//     than rebuilding a payload from modelled fields.
//   - The write is REPLACE, not merge. Any field absent from the payload is
//     deleted from the vault. Preserving a field you are not modifying means
//     sending it back verbatim; there is no passive option.
func (c *Client) EditItem(ctx context.Context, itemID string, item map[string]any, vault string) (string, error) {
	if itemID == "" {
		return "", fmt.Errorf("refusing to edit a 1Password item with no ID")
	}

	payload, err := json.Marshal(item)
	if err != nil {
		return "", fmt.Errorf("encoding 1Password item: %w", err)
	}

	args := []string{"item", "edit", itemID}
	if vault != "" {
		args = append(args, "--vault", vault)
	}

	stdout, stderr, err := c.runner.Run(ctx, "op", args, payload)
	if err != nil {
		return "", fmt.Errorf("failed to edit 1P item %s: %s", itemID, strings.TrimSpace(stderr))
	}
	return strings.TrimSpace(stdout), nil
}

// isNotFound distinguishes "no such item" from a real failure. `op` has no
// distinct exit code for it, so the message is all there is to go on.
func isNotFound(stderr string) bool {
	s := strings.ToLower(stderr)
	return strings.Contains(s, "isn't an item") || strings.Contains(s, "no item matches")
}
