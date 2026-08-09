package onepassword

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// NewFieldID mints an ID for a field the vault has not seen.
//
// It lives in services rather than domain because it reads the OS entropy
// pool: I/O, and nondeterministic. internal/domain is documented as free of
// both, and that guarantee is load-bearing rather than decorative -
// transformers call into the domain model and are documented as safe to call
// from workflow code, where a nondeterministic call breaks Temporal replay.
//
// The CLI preserves supplied field IDs verbatim, so whatever this returns
// becomes that field's stable identity for every subsequent run.
// A read failure panics rather than degrading. Returning "" would be the
// tempting alternative, and it is the wrong one for the same reason
// domain.UpsertField refuses to default a nil generator: StaleFieldIDs skips
// fields with an empty ID, so every field minted after such a failure becomes
// permanently invisible to stale tracking, silently disabling prune-1p.
//
// crypto/rand.Read does not fail on a healthy system; if it does, the machine
// has a problem worth stopping for, not working around.
func NewFieldID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("1Password field ID generation failed, refusing to mint unusable IDs: %v", err))
	}
	return hex.EncodeToString(b)
}
