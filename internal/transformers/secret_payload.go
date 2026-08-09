package transformers

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Pure helpers for turning an AWS secret payload into 1Password fields. They
// live here rather than in internal/services/onepassword because the
// transformer pipeline needs them too, and transformers sit below services in
// the layering - a transformer importing a service would invert it.

// Stringify renders a decoded JSON value as 1Password would expect it as a
// field value. Used instead of a bare fmt.Sprint(v): a JSON `null` decodes
// to a Go nil interface, and fmt.Sprint(nil) renders that as the literal
// string "<nil>" - a bogus, non-empty secret value - rather than the empty
// string the original Ruby tool's `value.to_s` produced for the same input.
func Stringify(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// ParseFlatJSONObject decodes a JSON object's top-level fields, preserving
// their original order (encoding/json's map decoding does not - Go map
// iteration order is randomized on purpose). Returns ok=false if s isn't a
// JSON object.
func ParseFlatJSONObject(s string) (keys []string, values map[string]any, ok bool) {
	dec := json.NewDecoder(strings.NewReader(s))
	// Without this, a JSON number decodes to float64, which can silently
	// round or scientific-notation-ify a large integer (an account/client
	// ID, say) before stringify ever sees it. json.Number preserves the
	// original digit string exactly, and fmt.Sprint renders it verbatim
	// since it implements fmt.Stringer.
	dec.UseNumber()

	tok, err := dec.Token()
	if err != nil {
		return nil, nil, false
	}
	if delim, isDelim := tok.(json.Delim); !isDelim || delim != '{' {
		return nil, nil, false
	}

	values = map[string]any{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, nil, false
		}
		key, isString := keyTok.(string)
		if !isString {
			return nil, nil, false
		}

		var value any
		if err := dec.Decode(&value); err != nil {
			return nil, nil, false
		}

		keys = append(keys, key)
		values[key] = value
	}

	if _, err := dec.Token(); err != nil { // consume closing '}'
		return nil, nil, false
	}
	if dec.More() {
		return nil, nil, false // trailing content after the object
	}

	return keys, values, true
}
