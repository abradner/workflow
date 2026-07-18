package transformers

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/abradner/workflow/internal/domain"
)

// Logger is the minimal logging interface this transformer accepts. It's
// satisfied both by go.temporal.io/sdk/log.Logger (workflow.GetLogger(ctx),
// when this runs inline in a workflow) and by any stand-in used in tests.
type Logger interface {
	Info(msg string, keyvals ...any)
}

// OnePasswordSamlKeyInjector remaps AWS secret names/values extracted for
// SourceEnv onto TargetEnv, and - if a fresh Keycloak public key is
// available - patches it into any JSON secret payload that carries an
// "mp.jwt.verify.publickey" field.
type OnePasswordSamlKeyInjector struct {
	SourceEnv   string
	TargetEnv   string
	KCPublicKey string // "" means no fresh key to inject
	Logger      Logger // nil is fine, logging is best-effort
}

// Call maps every extracted secret from SourceEnv to TargetEnv.
func (t OnePasswordSamlKeyInjector) Call(secrets []domain.ExtractedSecret) []domain.ExtractedSecret {
	out := make([]domain.ExtractedSecret, len(secrets))

	for i, secret := range secrets {
		mappedString := t.remapString(secret.String)

		if t.KCPublicKey != "" && mappedString != nil {
			if injected, ok := t.injectPublicKey(*mappedString); ok {
				mappedString = &injected
				if t.Logger != nil {
					t.Logger.Info(fmt.Sprintf("Injected fresh Keycloak public key into %s", secret.Name))
				}
			}
		}

		out[i] = domain.ExtractedSecret{
			Name:   strings.ReplaceAll(secret.Name, t.SourceEnv, t.TargetEnv),
			String: mappedString,
			Binary: secret.Binary,
		}
	}

	return out
}

func (t OnePasswordSamlKeyInjector) remapString(s *string) *string {
	if s == nil {
		return nil
	}
	mapped := strings.ReplaceAll(*s, t.SourceEnv, t.TargetEnv)
	return &mapped
}

// injectPublicKey patches mappedString's "mp.jwt.verify.publickey" field if
// mappedString parses as a JSON object carrying that key. Any other shape
// (not JSON, not an object, no such key) is left untouched - ok is false.
func (t OnePasswordSamlKeyInjector) injectPublicKey(mappedString string) (string, bool) {
	var payload map[string]any
	// Decoded with UseNumber rather than plain json.Unmarshal: without it,
	// any other numeric field in this payload (an account/client ID, say)
	// decodes to float64 and comes back out of json.Marshal below rounded
	// or in scientific notation - corrupting a field this transformer was
	// never even meant to touch, just because it happened to share a JSON
	// object with the public key.
	dec := json.NewDecoder(strings.NewReader(mappedString))
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil {
		return "", false
	}
	if _, hasKey := payload["mp.jwt.verify.publickey"]; !hasKey {
		return "", false
	}

	payload["mp.jwt.verify.publickey"] = t.KCPublicKey
	b, err := json.Marshal(payload)
	if err != nil {
		return "", false
	}
	return string(b), true
}
