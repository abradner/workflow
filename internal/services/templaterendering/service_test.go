package templaterendering_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/internal/services/templaterendering"
)

func TestFlattenHash(t *testing.T) {
	t.Run("flattens a nested map with dot-separated keys", func(t *testing.T) {
		input := map[string]any{
			"cluster": map[string]any{"id": "abc123", "secret": "xyz789"},
			"certs":   map[string]any{"os": map[string]any{"crt": "CERT_DATA", "key": "KEY_DATA"}},
		}

		assert.Equal(t, map[string]any{
			"cluster.id":     "abc123",
			"cluster.secret": "xyz789",
			"certs.os.crt":   "CERT_DATA",
			"certs.os.key":   "KEY_DATA",
		}, templaterendering.FlattenHash(input))
	})

	t.Run("handles a flat map unchanged", func(t *testing.T) {
		assert.Equal(t, map[string]any{"simple": "value"}, templaterendering.FlattenHash(map[string]any{"simple": "value"}))
	})

	t.Run("handles an empty map", func(t *testing.T) {
		assert.Equal(t, map[string]any{}, templaterendering.FlattenHash(map[string]any{}))
	})
}

func TestExtractPlaceholders(t *testing.T) {
	t.Run("finds all unique placeholder keys, sorted", func(t *testing.T) {
		template := "token: {{ trustdinfo.token }}\nca:\n    crt: {{ certs.os.crt }}\n    key: {{ certs.os.key }}\nid: {{ cluster.id }}\n"
		assert.Equal(t, []string{"certs.os.crt", "certs.os.key", "cluster.id", "trustdinfo.token"}, templaterendering.ExtractPlaceholders(template))
	})

	t.Run("deduplicates repeated placeholders", func(t *testing.T) {
		template := "crt: {{ certs.os.crt }}\nca: {{ certs.os.crt }}"
		assert.Equal(t, []string{"certs.os.crt"}, templaterendering.ExtractPlaceholders(template))
	})

	t.Run("returns empty for content without placeholders", func(t *testing.T) {
		assert.Empty(t, templaterendering.ExtractPlaceholders("no placeholders here"))
	})

	t.Run("handles varied whitespace inside braces", func(t *testing.T) {
		template := "a: {{key.one}} b: {{  key.two  }}"
		assert.Equal(t, []string{"key.one", "key.two"}, templaterendering.ExtractPlaceholders(template))
	})
}

func TestMissingKeys(t *testing.T) {
	secrets := map[string]any{"cluster.id": "abc", "cluster.secret": "xyz"}

	t.Run("returns empty when all keys are present", func(t *testing.T) {
		assert.Empty(t, templaterendering.MissingKeys("{{ cluster.id }} {{ cluster.secret }}", secrets))
	})

	t.Run("returns missing keys", func(t *testing.T) {
		assert.Equal(t, []string{"certs.os.crt"}, templaterendering.MissingKeys("{{ cluster.id }} {{ certs.os.crt }}", secrets))
	})
}

func TestRender(t *testing.T) {
	secrets := map[string]any{
		"cluster.id":       "R10sHpCQ==",
		"certs.os.crt":     "LS0tLS1CRUdJ",
		"trustdinfo.token": "ao3hmv.rn45d0",
	}

	t.Run("replaces all placeholders with secret values", func(t *testing.T) {
		template := "id: {{ cluster.id }}\nca:\n    crt: {{ certs.os.crt }}\ntoken: {{ trustdinfo.token }}\n"
		result, err := templaterendering.Render(template, secrets)
		require.NoError(t, err)
		assert.Equal(t, "id: R10sHpCQ==\nca:\n    crt: LS0tLS1CRUdJ\ntoken: ao3hmv.rn45d0\n", result)
	})

	t.Run("preserves surrounding content", func(t *testing.T) {
		result, err := templaterendering.Render("token: {{ trustdinfo.token }} # The machine token", secrets)
		require.NoError(t, err)
		assert.Equal(t, "token: ao3hmv.rn45d0 # The machine token", result)
	})

	t.Run("errors naming every unresolved placeholder", func(t *testing.T) {
		_, err := templaterendering.Render("{{ cluster.id }} {{ unknown.key }}", secrets)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown.key")
	})

	t.Run("handles templates with no placeholders", func(t *testing.T) {
		result, err := templaterendering.Render("static: content", secrets)
		require.NoError(t, err)
		assert.Equal(t, "static: content", result)
	})
}
