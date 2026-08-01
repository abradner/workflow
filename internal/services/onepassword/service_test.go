package onepassword_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/internal/domain"
	"github.com/abradner/workflow/internal/services/onepassword"
)

// fakeOpClient records what the service asked the CLI to do.
type fakeOpClient struct {
	existing map[string]any
	getErr   error

	gotVault    string
	created     map[string]any
	editedID    string
	editedItem  map[string]any
	createCalls int
	editCalls   int
}

func (f *fakeOpClient) GetItem(_ context.Context, _, vault string) (map[string]any, error) {
	f.gotVault = vault
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.existing, nil
}

func (f *fakeOpClient) CreateItem(_ context.Context, item map[string]any, vault string) (string, error) {
	f.gotVault, f.created, f.createCalls = vault, item, f.createCalls+1
	return "new-id", nil
}

func (f *fakeOpClient) EditItem(_ context.Context, id string, item map[string]any, vault string) (string, error) {
	f.gotVault, f.editedID, f.editedItem, f.editCalls = vault, id, item, f.editCalls+1
	return "edited", nil
}

func vaultItem() map[string]any {
	return map[string]any{
		"id": "abc123", "title": "k8s-wtf-dev4", "category": "SECURE_NOTE",
		"updated_at": "2026-07-01T00:00:00Z", "version": 3,
		"sections": []any{map[string]any{"id": "cfg", "label": "cfg"}},
		"fields": []any{
			map[string]any{"id": "f-1", "section": map[string]any{"id": "cfg"}, "label": "username", "value": "old", "type": "CONCEALED"},
			map[string]any{"id": "f-2", "section": map[string]any{"id": "cfg"}, "label": "hand-added", "value": "keep", "type": "CONCEALED"},
		},
	}
}

func TestLoad_ReturnsANewItemWhenTheVaultHasNone(t *testing.T) {
	client := &fakeOpClient{existing: nil}
	svc := onepassword.New("wtf", "Tooling", client)

	item, err := svc.Load(context.Background(), "dev4")
	require.NoError(t, err)

	assert.True(t, item.IsNew())
	assert.Equal(t, "k8s-wtf-dev4", item.Title())
	assert.Equal(t, "Tooling", client.gotVault)
}

// A failed lookup must not read as "no item yet": doing so would create a
// second item alongside the one already there.
func TestLoad_PropagatesLookupFailures(t *testing.T) {
	client := &fakeOpClient{getErr: errors.New("authorization timeout")}
	svc := onepassword.New("wtf", "Tooling", client)

	_, err := svc.Load(context.Background(), "dev4")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authorization timeout")
}

func TestCommit_CreatesWhenTheItemIsNew(t *testing.T) {
	client := &fakeOpClient{}
	svc := onepassword.New("wtf", "Tooling", client)

	item := domain.NewOnePasswordItem("k8s-wtf-dev4", "SECURE_NOTE")
	item.UpsertField("cfg", "username", "v", "CONCEALED")

	result, err := svc.Commit(context.Background(), item, onepassword.CommitOptions{})
	require.NoError(t, err)

	assert.True(t, result.Created)
	assert.Equal(t, 1, client.createCalls)
	assert.Equal(t, 0, client.editCalls)
	assert.Equal(t, "Tooling", client.gotVault)
}

func TestCommit_EditsWhenTheItemExists(t *testing.T) {
	client := &fakeOpClient{existing: vaultItem()}
	svc := onepassword.New("wtf", "Tooling", client)

	item, err := svc.Load(context.Background(), "dev4")
	require.NoError(t, err)
	item.UpsertField("cfg", "username", "new", "CONCEALED")

	result, err := svc.Commit(context.Background(), item, onepassword.CommitOptions{})
	require.NoError(t, err)

	assert.False(t, result.Created)
	assert.Equal(t, 1, client.editCalls)
	assert.Equal(t, "abc123", client.editedID)
	// op item edit validates these; a payload without them is rejected.
	assert.Equal(t, "abc123", client.editedItem["id"])
	assert.Contains(t, client.editedItem, "updated_at")
}

// The default. A field the vault holds that this run did not write is counted
// and sent back untouched -- under REPLACE semantics, omitting it would delete
// it, and it is frequently something a human put there on purpose.
func TestCommit_PreservesStaleFieldsAndCountsThem(t *testing.T) {
	client := &fakeOpClient{existing: vaultItem()}
	svc := onepassword.New("wtf", "Tooling", client)

	item, err := svc.Load(context.Background(), "dev4")
	require.NoError(t, err)
	item.UpsertField("cfg", "username", "new", "CONCEALED")

	result, err := svc.Commit(context.Background(), item, onepassword.CommitOptions{})
	require.NoError(t, err)

	assert.Equal(t, 1, result.StaleFields, "hand-added was not written this run")
	assert.Equal(t, 0, result.FieldsPruned)

	fields := client.editedItem["fields"].([]any)
	require.Len(t, fields, 2, "the stale field must still be in the payload")
}

func TestCommit_PrunesOnlyWhenAsked(t *testing.T) {
	client := &fakeOpClient{existing: vaultItem()}
	svc := onepassword.New("wtf", "Tooling", client)

	item, err := svc.Load(context.Background(), "dev4")
	require.NoError(t, err)
	item.UpsertField("cfg", "username", "new", "CONCEALED")

	result, err := svc.Commit(context.Background(), item, onepassword.CommitOptions{Prune: true})
	require.NoError(t, err)

	assert.Equal(t, 1, result.StaleFields)
	assert.Equal(t, 1, result.FieldsPruned)

	fields := client.editedItem["fields"].([]any)
	require.Len(t, fields, 1)
	assert.Equal(t, "f-1", fields[0].(map[string]any)["id"])
}

func TestCommit_ReportsUnknownTopLevelKeys(t *testing.T) {
	raw := vaultItem()
	raw["favorite"] = true
	client := &fakeOpClient{existing: raw}
	svc := onepassword.New("wtf", "Tooling", client)

	item, err := svc.Load(context.Background(), "dev4")
	require.NoError(t, err)

	result, err := svc.Commit(context.Background(), item, onepassword.CommitOptions{})
	require.NoError(t, err)

	assert.Equal(t, []string{"favorite"}, result.UnknownKeys)
	assert.Equal(t, true, client.editedItem["favorite"], "and it is written back untouched")
}

func TestCommit_RefusesNil(t *testing.T) {
	svc := onepassword.New("wtf", "Tooling", &fakeOpClient{})

	_, err := svc.Commit(context.Background(), nil, onepassword.CommitOptions{})
	require.Error(t, err)
}
