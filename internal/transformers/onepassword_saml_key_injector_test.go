package transformers_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/internal/domain"
	"github.com/abradner/workflow/internal/transformers"
)

type fakeLogger struct {
	infos []string
}

func (f *fakeLogger) Info(msg string, _ ...any) { f.infos = append(f.infos, msg) }

func strptr(s string) *string { return &s }

func TestOnePasswordSamlKeyInjector_MapsEnvironmentNames(t *testing.T) {
	mapper := transformers.OnePasswordSamlKeyInjector{SourceEnv: "dev4", TargetEnv: "dev5"}

	result := mapper.Call([]domain.ExtractedSecret{
		{Name: "dev4/pmn-config", String: strptr("conn=db.dev4.com")},
	})

	require.Len(t, result, 1)
	assert.Equal(t, "dev5/pmn-config", result[0].Name)
	require.NotNil(t, result[0].String)
	assert.Equal(t, "conn=db.dev5.com", *result[0].String)
}

func TestOnePasswordSamlKeyInjector_InjectsFreshPublicKey(t *testing.T) {
	logger := &fakeLogger{}
	mapper := transformers.OnePasswordSamlKeyInjector{
		SourceEnv:   "dev4",
		TargetEnv:   "dev5",
		KCPublicKey: "fresh_key",
		Logger:      logger,
	}

	result := mapper.Call([]domain.ExtractedSecret{
		{Name: "dev4/pmn-ui-api-config", String: strptr(`{"mp.jwt.verify.publickey":"stale"}`)},
	})

	require.Len(t, result, 1)
	require.NotNil(t, result[0].String)

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(*result[0].String), &payload))
	assert.Equal(t, "fresh_key", payload["mp.jwt.verify.publickey"])

	require.Len(t, logger.infos, 1)
	assert.Contains(t, logger.infos[0], "Injected fresh")
}

// TestOnePasswordSamlKeyInjector_PreservesLargeNumericFieldsWhileInjecting is
// the regression test for the unmarshal/marshal round trip in
// injectPublicKey: without json.Decoder.UseNumber, a large integer sharing a
// JSON object with "mp.jwt.verify.publickey" would decode to float64 and
// come back out rounded/in scientific notation, corrupting a field this
// transformer was never meant to touch.
func TestOnePasswordSamlKeyInjector_PreservesLargeNumericFieldsWhileInjecting(t *testing.T) {
	mapper := transformers.OnePasswordSamlKeyInjector{
		SourceEnv:   "dev4",
		TargetEnv:   "dev5",
		KCPublicKey: "fresh_key",
	}

	result := mapper.Call([]domain.ExtractedSecret{
		{Name: "dev4/pmn-ui-api-config", String: strptr(`{"mp.jwt.verify.publickey":"stale","clientId":123456789012345678}`)},
	})

	require.Len(t, result, 1)
	require.NotNil(t, result[0].String)
	assert.Contains(t, *result[0].String, `"clientId":123456789012345678`, "large integer must survive the round trip exactly, not as float64 rounding/scientific notation")

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(*result[0].String), &payload))
	assert.Equal(t, "fresh_key", payload["mp.jwt.verify.publickey"])
}

func TestOnePasswordSamlKeyInjector_LeavesNonMatchingJSONAlone(t *testing.T) {
	mapper := transformers.OnePasswordSamlKeyInjector{SourceEnv: "dev4", TargetEnv: "dev5", KCPublicKey: "fresh_key"}

	result := mapper.Call([]domain.ExtractedSecret{
		{Name: "dev4/other", String: strptr(`{"unrelated":"field"}`)},
		{Name: "dev4/binary-only", Binary: strptr("base64stuff")},
	})

	require.Len(t, result, 2)
	assert.JSONEq(t, `{"unrelated":"field"}`, *result[0].String)
	assert.Nil(t, result[1].String)
	require.NotNil(t, result[1].Binary)
	assert.Equal(t, "base64stuff", *result[1].Binary)
}
