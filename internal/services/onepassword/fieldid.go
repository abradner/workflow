package onepassword

import (
	"crypto/rand"
	"encoding/hex"
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
func NewFieldID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand does not fail in practice. If it ever does, an empty ID
		// is safer than a predictable one: the CLI assigns its own.
		return ""
	}
	return hex.EncodeToString(b)
}
