package transformers

import (
	"strings"

	"github.com/abradner/workflow/internal/domain"
)

// OnePasswordItemMapper folds extracted AWS secrets into a 1Password item,
// remapping them from the source environment onto the target as it goes.
//
// The remap happens on the secret *name* before it is sanitized into a section
// ID, which is the whole reason this type is simpler than the Ruby it replaces.
// That version mapped source-named sections into a target-named item and then
// renamed them afterwards, which made every section collide with its own
// hydrated counterpart and needed a deduplication pass to clean up. Remapping
// first means the section ID lands on its final value immediately, UpsertField
// matches the existing field directly, and the vault's field ID is preserved
// without any of that machinery.
type OnePasswordItemMapper struct {
	SourceEnv string
	TargetEnv string
	Logger    Logger // nil is fine, logging is best-effort

	// NewFieldID mints IDs for fields the vault has not seen. Injected rather
	// than generated here: minting one reads the OS entropy pool, and this
	// package is documented as pure so that workflow code can call it directly
	// without breaking Temporal replay.
	NewFieldID func() string
}

// Call upserts every secret into item.
func (t OnePasswordItemMapper) Call(item *domain.OnePasswordItem, secrets []domain.ExtractedSecret) {
	if item == nil {
		return
	}

	newFieldID := t.NewFieldID
	if newFieldID == nil {
		// An empty ID is what the domain model already falls back to, and the
		// CLI assigns its own. Better than panicking on a nil func in a
		// transformer that is otherwise total.
		newFieldID = func() string { return "" }
	}

	for _, secret := range secrets {
		sectionID := sanitizeSectionID(t.remap(secret.Name))

		switch {
		case secret.String != nil:
			value := t.remap(*secret.String)
			if keys, values, ok := ParseFlatJSONObject(value); ok {
				for _, k := range keys {
					item.UpsertField(sectionID, k, Stringify(values[k]), "CONCEALED", newFieldID)
				}
				continue
			}
			item.UpsertField(sectionID, "password", value, "CONCEALED", newFieldID)
		case secret.Binary != nil:
			// Not remapped: a base64 blob has no environment names in it, and
			// a substring replacement inside encoded bytes would corrupt it.
			item.UpsertField(sectionID, "password", *secret.Binary, "CONCEALED", newFieldID)
		}
	}
}

// remap rewrites the source environment to the target.
//
// Measured against a real 19-secret dev3 extract: every secret name contains
// the environment, and 8 of 47 field values do - all of them usernames like
// "dev3_pmn_keycloak", every one of which needs it. No secret-like value
// contained it.
func (t OnePasswordItemMapper) remap(s string) string {
	if t.SourceEnv == "" || t.SourceEnv == t.TargetEnv {
		return s
	}
	out := strings.ReplaceAll(s, t.SourceEnv, t.TargetEnv)
	if out != s && t.Logger != nil {
		// Logged without the value: this is secret material, and the point is
		// that a surprising rewrite is auditable, not that it is readable.
		t.Logger.Info("Remapped environment string in extracted secret",
			"from", t.SourceEnv, "to", t.TargetEnv)
	}
	return out
}

// sanitizeSectionID drops the leading environment segment from an AWS secret
// name and joins the rest with hyphens: "dev4/wtf/config" -> "wtf-config".
func sanitizeSectionID(awsName string) string {
	parts := strings.Split(awsName, "/")
	if len(parts) > 1 {
		parts = parts[1:]
	}
	return strings.Join(parts, "-")
}
