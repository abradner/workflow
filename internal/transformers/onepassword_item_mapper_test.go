package transformers_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/internal/domain"
	"github.com/abradner/workflow/internal/transformers"
)

func strPtr(s string) *string { return &s }

func fieldsOf(t *testing.T, item *domain.OnePasswordItem) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, raw := range item.Payload()["fields"].([]any) {
		f := raw.(map[string]any)
		section, _ := f["section"].(map[string]any)
		out[section["id"].(string)+"/"+f["label"].(string)] = f["value"].(string)
	}
	return out
}

// The property that lets the deduplication pass be deleted: the section ID is
// computed from the already-remapped name, so it lands on its final value and
// matches the hydrated field directly.
func TestOnePasswordItemMapper_RemapsNameBeforeSanitizing(t *testing.T) {
	existing := map[string]any{
		"id": "abc", "title": "k8s-pmn-dev4", "category": "SECURE_NOTE",
		"updated_at": "2026-07-01T00:00:00Z",
		"sections":   []any{map[string]any{"id": "dev4_pmn_keycloak-dev4_pmn_keycloak", "label": "x"}},
		"fields": []any{map[string]any{
			"id": "vault-field-1", "section": map[string]any{"id": "dev4_pmn_keycloak-dev4_pmn_keycloak"},
			"label": "username", "value": "dev4_pmn_keycloak", "type": "CONCEALED",
		}},
	}
	item := domain.WrapOnePasswordItem(existing)

	transformers.OnePasswordItemMapper{SourceEnv: "dev3", TargetEnv: "dev4"}.Call(item,
		[]domain.ExtractedSecret{{
			Name:   "dev/dev3_pmn_keycloak/dev3_pmn_keycloak",
			String: strPtr(`{"username":"dev3_pmn_keycloak"}`),
		}})

	fields := item.Payload()["fields"].([]any)
	require.Len(t, fields, 1, "must update the hydrated field, not collide with it")

	f := fields[0].(map[string]any)
	assert.Equal(t, "vault-field-1", f["id"], "the vault's field ID must survive")
	assert.Equal(t, "dev4_pmn_keycloak", f["value"], "value remapped onto the target env")
	assert.Empty(t, item.StaleFieldIDs(), "the field was written, so it is not stale")
}

func TestOnePasswordItemMapper_SpreadsJSONObjectsIntoFields(t *testing.T) {
	item := domain.NewOnePasswordItem("k8s-pmn-dev4", "SECURE_NOTE")

	transformers.OnePasswordItemMapper{SourceEnv: "dev3", TargetEnv: "dev4"}.Call(item,
		[]domain.ExtractedSecret{{
			Name:   "dev3/pmn/config",
			String: strPtr(`{"username":"dev3_user","password":"p@ss","port":5432}`),
		}})

	got := fieldsOf(t, item)
	assert.Equal(t, "dev4_user", got["pmn-config/username"])
	assert.Equal(t, "p@ss", got["pmn-config/password"])
	assert.Equal(t, "5432", got["pmn-config/port"], "an integer must not become 5432.0")
}

func TestOnePasswordItemMapper_OpaqueStringBecomesPassword(t *testing.T) {
	item := domain.NewOnePasswordItem("k8s-pmn-dev4", "SECURE_NOTE")

	transformers.OnePasswordItemMapper{SourceEnv: "dev3", TargetEnv: "dev4"}.Call(item,
		[]domain.ExtractedSecret{{Name: "dev3/pmn/keystore", String: strPtr("not json at all")}})

	assert.Equal(t, "not json at all", fieldsOf(t, item)["pmn-keystore/password"])
}

// A base64 blob has no environment names in it, and a substring replacement
// inside encoded bytes would corrupt it.
func TestOnePasswordItemMapper_DoesNotRemapBinaryPayloads(t *testing.T) {
	item := domain.NewOnePasswordItem("k8s-pmn-dev4", "SECURE_NOTE")
	blob := "ZGV2MyBpbnNpZGUgYmFzZTY0"

	transformers.OnePasswordItemMapper{SourceEnv: "dev3", TargetEnv: "dev4"}.Call(item,
		[]domain.ExtractedSecret{{Name: "dev3/pmn/keystore", Binary: &blob}})

	assert.Equal(t, blob, fieldsOf(t, item)["pmn-keystore/password"])
}

// Re-running against an already-migrated item changes nothing: the remap is
// source->target, and a target value contains no source string to rewrite.
func TestOnePasswordItemMapper_IsIdempotent(t *testing.T) {
	item := domain.NewOnePasswordItem("k8s-pmn-dev4", "SECURE_NOTE")
	mapper := transformers.OnePasswordItemMapper{SourceEnv: "dev3", TargetEnv: "dev4"}
	secrets := []domain.ExtractedSecret{{
		Name: "dev3/pmn/config", String: strPtr(`{"username":"dev3_user"}`),
	}}

	mapper.Call(item, secrets)
	first := fieldsOf(t, item)
	mapper.Call(item, secrets)

	assert.Equal(t, first, fieldsOf(t, item))
	assert.Len(t, item.Payload()["fields"].([]any), 1)
}
