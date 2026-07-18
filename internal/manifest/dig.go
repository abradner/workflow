package manifest

// Dig walks nested maps by key, in the spirit of Ruby's Hash#dig: it returns
// nil as soon as any intermediate value is missing or isn't a map, instead of
// panicking. It only handles string-keyed maps; the handful of call sites
// that also need to index into an array mid-chain do that explicitly.
func Dig(doc map[string]any, keys ...string) any {
	var cur any = doc
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok || m == nil {
			return nil
		}
		cur = m[k]
	}
	return cur
}

// DigMap is Dig, coerced to map[string]any (nil if the result isn't one).
func DigMap(doc map[string]any, keys ...string) map[string]any {
	m, _ := Dig(doc, keys...).(map[string]any)
	return m
}

// DigString is Dig, coerced to string ("" if the result isn't one).
func DigString(doc map[string]any, keys ...string) string {
	s, _ := Dig(doc, keys...).(string)
	return s
}

// DigSlice is Dig, coerced to []any (nil if the result isn't one).
func DigSlice(doc map[string]any, keys ...string) []any {
	s, _ := Dig(doc, keys...).([]any)
	return s
}

// MutateYAML normalizes the "single doc vs. document stream" split that
// Kustomize files have: docs is either a map[string]any (one document) or a
// []any (a multi-document stream, or a JSON-patch array). fn is applied to
// every document either way, and the result comes back in the same shape it
// went in. Anything else (e.g. raw file content as a string) passes through
// untouched. Mirrors Ruby's Transformers::Base#mutate_yaml.
func MutateYAML(docs any, fn func(any) any) any {
	switch v := docs.(type) {
	case map[string]any:
		return fn(v)
	case []any:
		out := make([]any, len(v))
		for i, d := range v {
			out[i] = fn(d)
		}
		return out
	default:
		return docs
	}
}
