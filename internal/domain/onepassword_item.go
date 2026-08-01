package domain

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
)

// knownItemKeys are the top-level keys `op item get --format json` returns.
// Anything outside this set is preserved untouched and reported by
// UnknownTopLevelKeys - see the type doc.
var knownItemKeys = map[string]bool{
	"id": true, "title": true, "category": true, "vault": true,
	"version": true, "created_at": true, "updated_at": true,
	"sections": true, "fields": true,
}

// OnePasswordItem is a 1Password item being built or amended.
//
// It wraps the decoded item *exactly as the CLI returned it* and exposes a
// typed API over that map, rather than modelling the item as a struct and
// re-serializing it. That is a correctness requirement, not a style choice, for
// two compounding reasons:
//
//   - `op item edit` demands a round-tripped payload. It validates id and
//     updated_at and rejects a hand-assembled subset outright. Rebuilding from
//     modelled fields produces something the CLI refuses.
//   - The write is REPLACE. A key dropped on the way out is *deleted from the
//     vault*. So a model that reconstructs the payload turns "we didn't model
//     that field" into silent data loss in someone's secret store.
//
// Wrapping makes both unreachable: unmodelled keys are never touched, so they
// cannot be dropped. UnknownTopLevelKeys reports anything 1Password returned
// that this type does not understand - it should never fire, which is exactly
// what makes it worth listening to when it does.
type OnePasswordItem struct {
	raw map[string]any

	// touched records field IDs written this run, so StaleFieldIDs can report
	// what the vault holds that the current extract no longer produces.
	touched map[string]bool
}

// NewOnePasswordItem builds an item that does not exist in the vault yet.
func NewOnePasswordItem(title, category string) *OnePasswordItem {
	return &OnePasswordItem{
		raw: map[string]any{
			"title":    title,
			"category": category,
			"sections": []any{},
			"fields":   []any{},
		},
		touched: map[string]bool{},
	}
}

// WrapOnePasswordItem adopts an item as returned by the CLI. The map is taken
// by reference and mutated in place; callers should not keep using it.
func WrapOnePasswordItem(raw map[string]any) *OnePasswordItem {
	if raw == nil {
		return nil
	}
	if _, ok := raw["sections"]; !ok {
		raw["sections"] = []any{}
	}
	if _, ok := raw["fields"]; !ok {
		raw["fields"] = []any{}
	}
	return &OnePasswordItem{raw: raw, touched: map[string]bool{}}
}

// ID is the vault's identifier, empty for an item that does not exist yet.
func (i *OnePasswordItem) ID() string { s, _ := i.raw["id"].(string); return s }

// Title is the item's title.
func (i *OnePasswordItem) Title() string { s, _ := i.raw["title"].(string); return s }

// IsNew reports whether this item still needs creating rather than editing.
func (i *OnePasswordItem) IsNew() bool { return i.ID() == "" }

// Payload is the structure to hand to the CLI, including every key this type
// does not model.
func (i *OnePasswordItem) Payload() map[string]any { return i.raw }

// UnknownTopLevelKeys lists keys the CLI returned that this type does not
// model. They are preserved regardless; this exists so schema drift is
// noticed rather than silently carried.
func (i *OnePasswordItem) UnknownTopLevelKeys() []string {
	var unknown []string
	for k := range i.raw {
		if !knownItemKeys[k] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	return unknown
}

// UpsertField sets a field by (section, label), creating the section and the
// field if needed, and preserving the vault's own field ID when one exists.
//
// Preserving that ID is the point of the whole upsert: it is what makes a
// field a stable thing across runs rather than a new field that happens to
// have the same name.
func (i *OnePasswordItem) UpsertField(sectionID, label, value, fieldType string) {
	i.ensureSection(sectionID)

	fields := i.fields()
	for _, f := range fields {
		if fieldSectionID(f) != sectionID || asString(f["label"]) != label {
			continue
		}
		f["value"] = value
		f["type"] = fieldType
		if id := asString(f["id"]); id != "" {
			i.touched[id] = true
		}
		return
	}

	id := newFieldID()
	i.raw["fields"] = append(i.rawFields(), map[string]any{
		"id":      id,
		"section": map[string]any{"id": sectionID},
		"label":   label,
		"value":   value,
		"type":    fieldType,
	})
	i.touched[id] = true
}

// StaleFieldIDs are fields the vault holds that this run did not write. They
// are candidates for removal, never removed implicitly: this tool does not
// delete what it did not just write.
func (i *OnePasswordItem) StaleFieldIDs() []string {
	var stale []string
	for _, f := range i.fields() {
		if id := asString(f["id"]); id != "" && !i.touched[id] {
			stale = append(stale, id)
		}
	}
	sort.Strings(stale)
	return stale
}

// DropFields removes the given field IDs. Only the prune workflow calls this.
func (i *OnePasswordItem) DropFields(ids []string) int {
	drop := make(map[string]bool, len(ids))
	for _, id := range ids {
		drop[id] = true
	}

	kept := make([]any, 0, len(i.rawFields()))
	removed := 0
	for _, raw := range i.rawFields() {
		f, ok := raw.(map[string]any)
		if ok && drop[asString(f["id"])] {
			removed++
			continue
		}
		kept = append(kept, raw)
	}
	i.raw["fields"] = kept
	return removed
}

func (i *OnePasswordItem) ensureSection(sectionID string) {
	for _, raw := range i.rawSections() {
		if s, ok := raw.(map[string]any); ok && asString(s["id"]) == sectionID {
			return
		}
	}
	i.raw["sections"] = append(i.rawSections(), map[string]any{
		"id": sectionID, "label": sectionID,
	})
}

func (i *OnePasswordItem) rawFields() []any   { s, _ := i.raw["fields"].([]any); return s }
func (i *OnePasswordItem) rawSections() []any { s, _ := i.raw["sections"].([]any); return s }

// fields returns the field maps, skipping any element that is not one.
func (i *OnePasswordItem) fields() []map[string]any {
	out := make([]map[string]any, 0, len(i.rawFields()))
	for _, raw := range i.rawFields() {
		if f, ok := raw.(map[string]any); ok {
			out = append(out, f)
		}
	}
	return out
}

func fieldSectionID(f map[string]any) string {
	section, ok := f["section"].(map[string]any)
	if !ok {
		return ""
	}
	return asString(section["id"])
}

func asString(v any) string { s, _ := v.(string); return s }

// newFieldID mints an ID for a field the vault has not seen. The CLI preserves
// supplied field IDs verbatim, so this becomes the field's stable identity for
// every subsequent run.
func newFieldID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand does not fail in practice; if it ever does, an empty ID
		// is safer than a predictable one - the CLI will assign its own.
		return ""
	}
	return hex.EncodeToString(b)
}
