// Package onepassword reads, amends and writes back the "one Secure Note per
// environment" vault item the workflow engine provisions from extracted AWS
// secrets.
//
// The read half is what makes this an upsert rather than a blind create. An
// earlier version built a fresh item and created it on every run, which
// replaced the vault item wholesale each time: any field added or corrected by
// hand was destroyed, and every field's identity churned, breaking anything
// that referenced one by ID.
package onepassword

import (
	"context"
	"fmt"

	"github.com/abradner/workflow/internal/domain"
)

// Client is the subset of the 1Password CLI wrapper this service needs.
type Client interface {
	GetItem(ctx context.Context, ref, vault string) (map[string]any, error)
	CreateItem(ctx context.Context, item map[string]any, vault string) (string, error)
	EditItem(ctx context.Context, itemID string, item map[string]any, vault string) (string, error)
}

// Service reads and writes one environment's Secure Note.
type Service struct {
	client      Client
	projectName string
	vaultName   string
}

// New builds a Service for the given project, working in vaultName.
func New(projectName, vaultName string, client Client) *Service {
	return &Service{client: client, projectName: projectName, vaultName: vaultName}
}

// ItemTitle is the Secure Note title for env.
func (s *Service) ItemTitle(env string) string {
	return fmt.Sprintf("k8s-%s-%s", s.projectName, env)
}

// Load returns env's existing item, or a new empty one if the vault has none.
//
// The distinction between "no item yet" and "the lookup failed" is load-bearing
// and comes from the client: a miss is (nil, nil), never an error. Treating a
// failed lookup as a miss would create a *second* item alongside the first.
func (s *Service) Load(ctx context.Context, env string) (*domain.OnePasswordItem, error) {
	title := s.ItemTitle(env)

	raw, err := s.client.GetItem(ctx, title, s.vaultName)
	if err != nil {
		return nil, fmt.Errorf("reading 1Password item %s: %w", title, err)
	}
	if raw == nil {
		return domain.NewOnePasswordItem(title, "SECURE_NOTE"), nil
	}
	return domain.WrapOnePasswordItem(raw), nil
}

// CommitResult describes what a Commit did, in terms safe to return to
// workflow code: counts and flags, never field names or values.
type CommitResult struct {
	// Created is true when the item did not previously exist.
	Created bool

	// StaleFields counts fields the vault holds that this run did not write.
	// Reported, never acted on - see Commit's Prune option.
	StaleFields int

	// FieldsPruned counts fields actually removed. Zero unless Prune was set.
	FieldsPruned int

	// UnknownKeys names top-level item keys this codebase does not model. They
	// are written back untouched; this exists so schema drift is noticed. Key
	// names only, and 1Password's own vocabulary - not secret material.
	UnknownKeys []string
}

// CommitOptions controls what Commit does beyond writing.
type CommitOptions struct {
	// Prune removes stale fields instead of merely counting them. Off by
	// default and never inferred: this tool does not delete what it did not
	// just write, and a stale field is frequently something a human put there
	// on purpose.
	Prune bool
}

// Commit writes item back to the vault, creating it if it does not exist yet.
//
// Every field the item carries is sent, including ones this run did not touch
// and keys this codebase does not model. That is not thoroughness, it is
// required: `op item edit` replaces rather than merges, so a field omitted from
// the payload is deleted from the vault. domain.OnePasswordItem holds the
// vault's own structure precisely so nothing can be dropped on the way out.
func (s *Service) Commit(ctx context.Context, item *domain.OnePasswordItem, opts CommitOptions) (CommitResult, error) {
	if item == nil {
		return CommitResult{}, fmt.Errorf("refusing to commit a nil 1Password item")
	}

	stale := item.StaleFieldIDs()
	result := CommitResult{
		Created:     item.IsNew(),
		StaleFields: len(stale),
		UnknownKeys: item.UnknownTopLevelKeys(),
	}

	if opts.Prune && len(stale) > 0 {
		result.FieldsPruned = item.DropFields(stale)
	}

	if item.IsNew() {
		if _, err := s.client.CreateItem(ctx, item.Payload(), s.vaultName); err != nil {
			return CommitResult{}, fmt.Errorf("creating 1Password item %s: %w", item.Title(), err)
		}
		return result, nil
	}

	if _, err := s.client.EditItem(ctx, item.ID(), item.Payload(), s.vaultName); err != nil {
		return CommitResult{}, fmt.Errorf("updating 1Password item %s: %w", item.Title(), err)
	}
	return result, nil
}
