package onepassword_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/internal/domain"
	"github.com/abradner/workflow/internal/services/onepassword"
)

type fakeOpClient struct {
	gotItem map[string]any
}

func (f *fakeOpClient) CreateItem(_ context.Context, item map[string]any) (string, error) {
	f.gotItem = item
	return "ok", nil
}

func strptr(s string) *string { return &s }

func TestIngestVaultItem_BuildsSectionsAndFieldsFromExtractedSecrets(t *testing.T) {
	client := &fakeOpClient{}
	svc := onepassword.New("wtf", client)

	secrets := []domain.ExtractedSecret{
		{Name: "dev3/wtf/config", String: strptr(`{"foo":"bar","baz":"qux"}`)},
		{Name: "dev3/wtf-ext/keystore", Binary: strptr("base64EncodedString")},
		{Name: "dev3/wtf-raw/secret", String: strptr("raw_string_password")},
	}

	_, err := svc.IngestVaultItem(context.Background(), "dev4", secrets)
	require.NoError(t, err)

	require.NotNil(t, client.gotItem)
	assert.Equal(t, "k8s-wtf-dev4", client.gotItem["title"])
	assert.Equal(t, "SECURE_NOTE", client.gotItem["category"])

	sections := client.gotItem["sections"].([]map[string]any)
	assert.Equal(t, []map[string]any{
		{"id": "wtf-config", "label": "wtf-config"},
		{"id": "wtf-ext-keystore", "label": "wtf-ext-keystore"},
		{"id": "wtf-raw-secret", "label": "wtf-raw-secret"},
	}, sections)

	fields := client.gotItem["fields"].([]map[string]any)
	require.Len(t, fields, 4)

	// JSON object fields are spread out in their original key order (not
	// Go's randomized map order) - see parseFlatJSONObject.
	assert.Equal(t, concealedField("wtf-config", "foo", "bar"), fields[0])
	assert.Equal(t, concealedField("wtf-config", "baz", "qux"), fields[1])
	// Binary data maps to a single "password" field.
	assert.Equal(t, concealedField("wtf-ext-keystore", "password", "base64EncodedString"), fields[2])
	// Raw non-JSON string maps to a single "password" field.
	assert.Equal(t, concealedField("wtf-raw-secret", "password", "raw_string_password"), fields[3])
}

// TestIngestVaultItem_NullJSONFieldBecomesEmptyString is the regression test
// for a JSON `null` value decoding to a Go nil interface and then rendering
// as the literal string "<nil>" via a bare fmt.Sprint - instead of the empty
// string the original Ruby tool's `value.to_s` produced for the same input.
func TestIngestVaultItem_NullJSONFieldBecomesEmptyString(t *testing.T) {
	client := &fakeOpClient{}
	svc := onepassword.New("wtf", client)

	secrets := []domain.ExtractedSecret{
		{Name: "dev3/wtf/config", String: strptr(`{"foo":null}`)},
	}

	_, err := svc.IngestVaultItem(context.Background(), "dev4", secrets)
	require.NoError(t, err)

	fields := client.gotItem["fields"].([]map[string]any)
	require.Len(t, fields, 1)
	assert.Equal(t, concealedField("wtf-config", "foo", ""), fields[0])
}

func TestIngestVaultItem_EmptySecretsMarshalFieldsAsEmptyArray(t *testing.T) {
	client := &fakeOpClient{}
	svc := onepassword.New("wtf", client)

	_, err := svc.IngestVaultItem(context.Background(), "dev4", nil)
	require.NoError(t, err)

	// A nil Go slice marshals to JSON `null`, which the 1Password item-create
	// API can reject for a "fields" array - it must always be `[]`.
	b, err := json.Marshal(client.gotItem["fields"])
	require.NoError(t, err)
	assert.JSONEq(t, `[]`, string(b))
}

func concealedField(sectionID, label, value string) map[string]any {
	return map[string]any{
		"section": map[string]any{"id": sectionID},
		"label":   label,
		"value":   value,
		"type":    "CONCEALED",
	}
}
