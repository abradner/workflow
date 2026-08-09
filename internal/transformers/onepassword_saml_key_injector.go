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

// OnePasswordSamlKeyInjector patches a freshly-fetched Keycloak public key
// into any JSON secret payload carrying an "mp.jwt.verify.publickey" field.
//
// It used to remap SourceEnv onto TargetEnv as well. That job now belongs to
// OnePasswordItemMapper, which has to do it anyway to compute the section ID -
// doing it in both places meant the same substitution ran twice, and put the
// remap in a type whose name says nothing about it.
//
// Note what it does NOT do when no key is available: it leaves the payload
// alone, which means the value read from the *source* environment survives
// into the target's vault item. That is a stale key wearing the right label,
// not an absent one, and it is why sync-1p's ordering matters - see
// docs/OPERATIONS.md.
type OnePasswordSamlKeyInjector struct {
	KCPublicKey string // "" means no fresh key to inject
	Logger      Logger // nil is fine, logging is best-effort
}

// Call returns secrets with the public key patched into any payload carrying
// the field. Everything else is passed through untouched.
func (t OnePasswordSamlKeyInjector) Call(secrets []domain.ExtractedSecret) []domain.ExtractedSecret {
	out := make([]domain.ExtractedSecret, len(secrets))

	for i, secret := range secrets {
		out[i] = secret

		if t.KCPublicKey == "" || secret.String == nil {
			continue
		}
		if injected, ok := t.injectPublicKey(*secret.String); ok {
			out[i].String = &injected
			if t.Logger != nil {
				t.Logger.Info(fmt.Sprintf("Injected fresh Keycloak public key into %s", secret.Name))
			}
		}
	}

	return out
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
