// Package discoversamlcreds fetches SAML/OIDC material from a Keycloak
// realm, tolerating the realm being unreachable (a fresh environment may not
// have Keycloak set up yet).
package discoversamlcreds

import (
	"context"
	"fmt"

	"github.com/abradner/workflow/internal/domain"
)

// KeycloakClient is the subset of the Keycloak client this service needs.
type KeycloakClient interface {
	FetchRealmPublicKey(ctx context.Context, realmName string) (string, error)
	FetchSAMLDescriptor(ctx context.Context, realmName string) (string, error)
}

// ClientFactory builds a KeycloakClient for a given base URL - a fresh
// client per call, matching one Keycloak client per target environment.
type ClientFactory func(baseURL string) KeycloakClient

// Logger is the minimal logging interface this service accepts.
type Logger interface {
	Warn(msg string, keyvals ...any)
}

// Service fetches SAML credentials from Keycloak realms.
type Service struct {
	newClient ClientFactory
	logger    Logger
}

// New builds a Service. logger may be nil.
func New(newClient ClientFactory, logger Logger) *Service {
	return &Service{newClient: newClient, logger: logger}
}

// FetchFor returns SAML/OIDC credentials from realmName at baseURL, or nil
// if the realm couldn't be reached - failures here are non-fatal, the
// caller falls back gracefully (e.g. skipping public-key injection).
func (s *Service) FetchFor(ctx context.Context, realmName, baseURL string) *domain.SamlCredentials {
	client := s.newClient(baseURL)

	publicKey, err := client.FetchRealmPublicKey(ctx, realmName)
	if err != nil {
		s.warn(baseURL, err)
		return nil
	}

	ssoXML, err := client.FetchSAMLDescriptor(ctx, realmName)
	if err != nil {
		s.warn(baseURL, err)
		return nil
	}

	return &domain.SamlCredentials{PublicKey: publicKey, SSOXML: ssoXML}
}

func (s *Service) warn(baseURL string, err error) {
	if s.logger != nil {
		s.logger.Warn(fmt.Sprintf("Failed to fetch SAML credentials from %s (%s). Falling back gracefully.", baseURL, err))
	}
}
