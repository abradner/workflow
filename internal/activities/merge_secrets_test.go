package activities

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abradner/workflow/internal/domain"
)

// In-package so mergeSecrets can be tested directly. Driving it through the
// activity would need a fake that answers the same secret name differently
// depending on which pass asked - stateful, order-dependent, and testing the
// fake more than the merge.
func TestMergeSecrets(t *testing.T) {
	str := func(s string) *string { return &s }
	named := func(name, value string) domain.ExtractedSecret {
		return domain.ExtractedSecret{Name: name, String: str(value)}
	}
	names := func(got []domain.ExtractedSecret) []string {
		out := make([]string, len(got))
		for i, s := range got {
			out[i] = s.Name
		}
		return out
	}

	t.Run("preserves order and appends new entries", func(t *testing.T) {
		got := mergeSecrets(
			[]domain.ExtractedSecret{named("a", "1"), named("b", "2")},
			[]domain.ExtractedSecret{named("z", "9")},
		)
		assert.Equal(t, []string{"a", "b", "z"}, names(got),
			"a map-based merge would satisfy every other assertion here and reshuffle this one")
	})

	t.Run("an explicitly named secret wins a collision", func(t *testing.T) {
		got := mergeSecrets(
			[]domain.ExtractedSecret{named("a", "filtered"), named("b", "filtered")},
			[]domain.ExtractedSecret{named("b", "exact")},
		)
		assert.Equal(t, []string{"a", "b"}, names(got), "an override replaces in place, it does not append")
		assert.Equal(t, "exact", *got[1].String)
	})

	t.Run("override does not disturb position", func(t *testing.T) {
		got := mergeSecrets(
			[]domain.ExtractedSecret{named("a", "1"), named("b", "2"), named("c", "3")},
			[]domain.ExtractedSecret{named("a", "new")},
		)
		assert.Equal(t, []string{"a", "b", "c"}, names(got), "a must stay first, not move to the end")
		assert.Equal(t, "new", *got[0].String)
	})

	t.Run("empty override is a no-op", func(t *testing.T) {
		base := []domain.ExtractedSecret{named("a", "1")}
		assert.Equal(t, []string{"a"}, names(mergeSecrets(base, nil)))
	})
}
