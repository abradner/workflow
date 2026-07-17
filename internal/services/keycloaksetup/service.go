// Package keycloaksetup provisions a fresh Keycloak realm: the OIDC/SAML
// clients, groups, and seed users this project's environments need.
//
// It intentionally does NOT wait for Keycloak to become reachable - that
// polling loop lives in the Temporal workflow layer instead
// (internal/workflows/setupkeycloak.go), using workflow.Sleep so the wait is
// a durable timer rather than a blocking in-process sleep.
package keycloaksetup

import (
	"context"
	"encoding/base64"
	"fmt"
)

// RealmName and ClientID are fixed by convention for this project - the
// same realm/client every environment provisions.
const (
	RealmName = "neons"
	ClientID  = "Optus CFS"
)

// KeycloakClient is the subset of the Keycloak client this service needs.
type KeycloakClient interface {
	BaseURL() string
	Authenticate(ctx context.Context, username, password string) error
	CreateRealm(ctx context.Context, realmName string) error
	ImportClient(ctx context.Context, realmName string, payload map[string]any) error
	CreateGroup(ctx context.Context, realmName, groupName string) error
	CreateUser(ctx context.Context, realmName string, payload map[string]any) (string, error)
	GetUsers(ctx context.Context, realmName, username string) ([]map[string]any, error)
	GetGroups(ctx context.Context, realmName, search string) ([]map[string]any, error)
	AddUserToGroup(ctx context.Context, realmName, userID, groupID string) error
	FetchSAMLDescriptor(ctx context.Context, realmName string) (string, error)
}

// Logger is the minimal logging interface this service accepts.
type Logger interface {
	Info(msg string, keyvals ...any)
}

// Descriptors is the exported SAML SP descriptor, in both raw XML and
// base64 forms - the shapes each downstream consumer (Quarkus config vs.
// raw file) wants.
type Descriptors struct {
	XML string
	B64 string
}

// Service provisions Keycloak. It has no concept of "waiting for ready" -
// the caller must ensure Keycloak is reachable first.
type Service struct {
	client KeycloakClient
	logger Logger
}

// New builds a Service. logger may be nil.
func New(client KeycloakClient, logger Logger) *Service {
	return &Service{client: client, logger: logger}
}

// Setup authenticates, creates the realm and its clients/groups/users, and
// returns the exported SAML descriptor.
func (s *Service) Setup(ctx context.Context, adminUsername, adminPassword string) (Descriptors, error) {
	s.log("Authenticating as %s...", adminUsername)
	if err := s.client.Authenticate(ctx, adminUsername, adminPassword); err != nil {
		return Descriptors{}, err
	}

	s.log("Creating realm '%s'...", RealmName)
	if err := s.client.CreateRealm(ctx, RealmName); err != nil {
		return Descriptors{}, err
	}

	s.log("Importing client '%s'...", ClientID)
	if err := s.importClients(ctx); err != nil {
		return Descriptors{}, err
	}

	if err := s.setupGroups(ctx); err != nil {
		return Descriptors{}, err
	}
	if err := s.setupUsers(ctx); err != nil {
		return Descriptors{}, err
	}

	s.log("Exporting SAML descriptor...")
	descriptor, err := s.client.FetchSAMLDescriptor(ctx, RealmName)
	if err != nil {
		return Descriptors{}, err
	}

	return Descriptors{
		XML: descriptor,
		B64: base64.StdEncoding.EncodeToString([]byte(descriptor)),
	}, nil
}

func (s *Service) importClients(ctx context.Context) error {
	redirectURIs := []any{
		"http://localhost:8080/*",
		"http://host.docker.internal:8080/*",
		"https://*",
	}

	oidcPayload := map[string]any{
		"clientId":                  ClientID,
		"enabled":                   true,
		"protocol":                  "openid-connect",
		"directAccessGrantsEnabled": true,
		"publicClient":              false,
		"secret":                    "local_pmn_client_secret",
		"redirectUris":              redirectURIs,
		"webOrigins":                []any{"*"},
	}

	samlPayload := map[string]any{
		"clientId":     s.client.BaseURL() + "/realms/" + RealmName,
		"enabled":      true,
		"protocol":     "saml",
		"redirectUris": redirectURIs,
		"attributes": map[string]any{
			"saml.assertion.signature": "false",
			"saml.server.signature":    "true", // Quarkus expects the assertion or response to be signed
			"saml.client.signature":    "false",
			"saml.encrypt":             "false",
			"saml.authnstatement":      "true",
			"saml.force.post.binding":  "true",
		},
	}

	if err := s.client.ImportClient(ctx, RealmName, oidcPayload); err != nil {
		return err
	}
	return s.client.ImportClient(ctx, RealmName, samlPayload)
}

var provisionedGroups = []string{
	"CN=PMN_Admin_Access",
	"CN=PMN_Porting_Team_Access",
	"CN=PMN_ReadOnly_Access",
}

func (s *Service) setupGroups(ctx context.Context) error {
	for _, group := range provisionedGroups {
		s.log("Creating group '%s'...", group)
		if err := s.client.CreateGroup(ctx, RealmName, group); err != nil {
			return err
		}
	}
	return nil
}

type provisionedUser struct {
	Username  string
	Email     string
	FirstName string
	LastName  string
	Group     string
}

var provisionedUsers = []provisionedUser{
	{Username: "admin", Email: "admin@optus.com.au", FirstName: "The", LastName: "Admin", Group: "CN=PMN_Admin_Access"},
	{Username: "portingteam", Email: "portingteam@optus.com.au", FirstName: "porting", LastName: "team", Group: "CN=PMN_Porting_Team_Access"},
	{Username: "readonly", Email: "readonly@optus.com.au", FirstName: "read", LastName: "only", Group: "CN=PMN_ReadOnly_Access"},
}

func (s *Service) setupUsers(ctx context.Context) error {
	for _, u := range provisionedUsers {
		s.log("Creating user '%s'...", u.Username)

		payload := map[string]any{
			"username":  u.Username,
			"email":     u.Email,
			"firstName": u.FirstName,
			"lastName":  u.LastName,
			"enabled":   true,
			"credentials": []any{
				map[string]any{"type": "password", "value": u.Username, "temporary": false},
			},
		}

		userID, err := s.client.CreateUser(ctx, RealmName, payload)
		if err != nil {
			return err
		}

		// The user may already exist (e.g. re-running setup) - the CLI
		// wrapper reports no ID for that case, so look it up instead.
		if userID == "" {
			existing, err := s.client.GetUsers(ctx, RealmName, u.Username)
			if err != nil {
				return err
			}
			if len(existing) > 0 {
				if id, ok := existing[0]["id"].(string); ok {
					userID = id
				}
			}
		}

		if userID != "" {
			if err := s.assignUserToGroup(ctx, userID, u.Username, u.Group); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) assignUserToGroup(ctx context.Context, userID, username, groupName string) error {
	groups, err := s.client.GetGroups(ctx, RealmName, groupName)
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		return nil
	}

	groupID, _ := groups[0]["id"].(string)
	if groupID == "" {
		return nil
	}

	s.log("Adding user '%s' to group '%s'...", username, groupName)
	return s.client.AddUserToGroup(ctx, RealmName, userID, groupID)
}

func (s *Service) log(format string, args ...any) {
	if s.logger != nil {
		s.logger.Info(fmt.Sprintf(format, args...))
	}
}
