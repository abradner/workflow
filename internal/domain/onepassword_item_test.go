package domain_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/internal/domain"
)

// The item a real `op item get --format json` returns, including the keys the
// model does not interpret but must not lose.
// testFieldID is deterministic: these tests care about which field got the ID,
// never what it is.
var testFieldIDCounter int

func testFieldID() string {
	testFieldIDCounter++
	return fmt.Sprintf("generated-%d", testFieldIDCounter)
}

func vaultItem() map[string]any {
	return map[string]any{
		"id": "abc123", "title": "k8s-wtf-dev4", "category": "SECURE_NOTE",
		"vault":      map[string]any{"name": "Tooling"},
		"version":    3,
		"created_at": "2026-01-01T00:00:00Z",
		"updated_at": "2026-07-01T00:00:00Z",
		"sections":   []any{map[string]any{"id": "cfg", "label": "cfg"}},
		"fields": []any{
			map[string]any{"id": "f-1", "section": map[string]any{"id": "cfg"}, "label": "username", "value": "old", "type": "CONCEALED"},
			map[string]any{"id": "f-2", "section": map[string]any{"id": "cfg"}, "label": "legacy", "value": "keep", "type": "CONCEALED"},
		},
	}
}

// The property everything else rests on: keys the model does not understand
// survive to the payload. Under op item edit's REPLACE semantics, dropping one
// deletes it from the vault.
func TestOnePasswordItem_PreservesUnmodelledKeys(t *testing.T) {
	raw := vaultItem()
	raw["favorite"] = true
	raw["urls"] = []any{map[string]any{"href": "https://example.com"}}

	item := domain.WrapOnePasswordItem(raw)
	item.UpsertField("cfg", "username", "new", "CONCEALED", testFieldID)

	payload := item.Payload()
	assert.Equal(t, true, payload["favorite"], "an unmodelled key must reach the vault untouched")
	assert.Contains(t, payload, "urls")
	// And the ones op item edit validates.
	assert.Equal(t, "abc123", payload["id"])
	assert.Equal(t, "2026-07-01T00:00:00Z", payload["updated_at"])
	assert.Equal(t, 3, payload["version"])
}

func TestOnePasswordItem_ReportsUnknownKeys(t *testing.T) {
	raw := vaultItem()
	item := domain.WrapOnePasswordItem(raw)
	assert.Empty(t, item.UnknownTopLevelKeys(), "a stock item must not trip the warning")

	raw["favorite"] = true
	raw["autofill_behaviour"] = "never"
	assert.Equal(t, []string{"autofill_behaviour", "favorite"},
		domain.WrapOnePasswordItem(raw).UnknownTopLevelKeys())
}

// Updating an existing field keeps the vault's own field ID. That identity is
// the entire reason for reading the item before writing it.
func TestOnePasswordItem_UpsertPreservesVaultFieldID(t *testing.T) {
	item := domain.WrapOnePasswordItem(vaultItem())

	item.UpsertField("cfg", "username", "new", "CONCEALED", testFieldID)

	fields := item.Payload()["fields"].([]any)
	require.Len(t, fields, 2, "updating must not append a duplicate")
	f := fields[0].(map[string]any)
	assert.Equal(t, "f-1", f["id"])
	assert.Equal(t, "new", f["value"])
}

func TestOnePasswordItem_UpsertAddsNewFieldAndSection(t *testing.T) {
	item := domain.WrapOnePasswordItem(vaultItem())

	item.UpsertField("newsec", "api_key", "secret", "CONCEALED", testFieldID)

	fields := item.Payload()["fields"].([]any)
	require.Len(t, fields, 3)
	added := fields[2].(map[string]any)
	assert.NotEmpty(t, added["id"], "a new field needs an ID to be stable next run")
	assert.Equal(t, "newsec", added["section"].(map[string]any)["id"])

	sections := item.Payload()["sections"].([]any)
	assert.Len(t, sections, 2, "the section must be created alongside the field")
}

// Stale means "the vault has it and this run did not write it" - the basis for
// the prune workflow, and for the warning sync-1p emits without acting on it.
func TestOnePasswordItem_StaleFieldIDs(t *testing.T) {
	item := domain.WrapOnePasswordItem(vaultItem())

	item.UpsertField("cfg", "username", "new", "CONCEALED", testFieldID)

	assert.Equal(t, []string{"f-2"}, item.StaleFieldIDs(),
		"f-1 was written this run; f-2 was not")
}

func TestOnePasswordItem_DropFields(t *testing.T) {
	item := domain.WrapOnePasswordItem(vaultItem())

	removed := item.DropFields([]string{"f-2"})

	assert.Equal(t, 1, removed)
	fields := item.Payload()["fields"].([]any)
	require.Len(t, fields, 1)
	assert.Equal(t, "f-1", fields[0].(map[string]any)["id"])
}

func TestOnePasswordItem_NewItemIsNewAndHasNoID(t *testing.T) {
	item := domain.NewOnePasswordItem("k8s-wtf-dev5", "SECURE_NOTE")

	assert.True(t, item.IsNew())
	assert.Empty(t, item.ID())
	assert.Equal(t, "k8s-wtf-dev5", item.Title())

	item.UpsertField("cfg", "username", "v", "CONCEALED", testFieldID)
	assert.Empty(t, item.StaleFieldIDs(), "a fresh item has nothing stale")
}

func TestWrapOnePasswordItem_NilIsNil(t *testing.T) {
	assert.Nil(t, domain.WrapOnePasswordItem(nil))
}
