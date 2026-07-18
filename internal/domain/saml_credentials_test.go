package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abradner/workflow/internal/domain"
)

func TestSamlCredentials_PEMPublicKey(t *testing.T) {
	t.Run("formats base64 into 64-character PEM blocks", func(t *testing.T) {
		rawKey := strings.Repeat("A", 64) + strings.Repeat("B", 64) + "CC"
		creds := domain.SamlCredentials{PublicKey: rawKey, SSOXML: "<xml/>"}

		expected := strings.Join([]string{
			"-----BEGIN PUBLIC KEY-----",
			strings.Repeat("A", 64),
			strings.Repeat("B", 64),
			"CC",
			"-----END PUBLIC KEY-----",
		}, "\n")

		assert.Equal(t, expected, creds.PEMPublicKey())
	})

	t.Run("returns empty string when the public key is missing", func(t *testing.T) {
		creds := domain.SamlCredentials{PublicKey: "", SSOXML: "<xml/>"}
		assert.Empty(t, creds.PEMPublicKey())
	})
}
