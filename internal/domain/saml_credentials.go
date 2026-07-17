// Package domain holds small, framework-free value types shared across the
// workflow engine: data with behavior, but no I/O.
package domain

import "strings"

// SamlCredentials is the SAML/OIDC material discovered from a Keycloak realm.
type SamlCredentials struct {
	PublicKey string
	SSOXML    string
}

// PEMPublicKey formats the raw base64 public key into the 64-column PEM block
// that Quarkus SmallRye JWT config expects. Returns "" if there's no key.
func (c SamlCredentials) PEMPublicKey() string {
	if c.PublicKey == "" {
		return ""
	}

	lines := make([]string, 0, len(c.PublicKey)/64+3)
	lines = append(lines, "-----BEGIN PUBLIC KEY-----")
	for i := 0; i < len(c.PublicKey); i += 64 {
		end := min(i+64, len(c.PublicKey))
		lines = append(lines, c.PublicKey[i:end])
	}
	lines = append(lines, "-----END PUBLIC KEY-----")

	return strings.Join(lines, "\n")
}
