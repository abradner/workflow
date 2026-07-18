package discoversamlcreds_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/internal/services/discoversamlcreds"
)

type fakeKeycloakClient struct {
	publicKey    string
	publicKeyErr error
	ssoXML       string
	ssoXMLErr    error
}

func (f *fakeKeycloakClient) FetchRealmPublicKey(context.Context, string) (string, error) {
	return f.publicKey, f.publicKeyErr
}

func (f *fakeKeycloakClient) FetchSAMLDescriptor(context.Context, string) (string, error) {
	return f.ssoXML, f.ssoXMLErr
}

type fakeWarnLogger struct {
	warnings []string
}

func (f *fakeWarnLogger) Warn(msg string, _ ...any) { f.warnings = append(f.warnings, msg) }

func TestFetchFor_ReturnsCredentialsOnSuccess(t *testing.T) {
	fake := &fakeKeycloakClient{publicKey: "PUBKEY", ssoXML: "<xml/>"}
	svc := discoversamlcreds.New(func(string) discoversamlcreds.KeycloakClient { return fake }, nil)

	result := svc.FetchFor(context.Background(), "neons", "http://test")
	require.NotNil(t, result)
	assert.Equal(t, "PUBKEY", result.PublicKey)
	assert.Equal(t, "<xml/>", result.SSOXML)
}

func TestFetchFor_FallsBackGracefullyOnFailure(t *testing.T) {
	fake := &fakeKeycloakClient{publicKeyErr: errors.New("conn refused")}
	logger := &fakeWarnLogger{}
	svc := discoversamlcreds.New(func(string) discoversamlcreds.KeycloakClient { return fake }, logger)

	result := svc.FetchFor(context.Background(), "neons", "http://test")
	assert.Nil(t, result)
	require.Len(t, logger.warnings, 1)
	assert.Contains(t, logger.warnings[0], "Falling back gracefully")
}
