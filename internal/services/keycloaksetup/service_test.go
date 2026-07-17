package keycloaksetup_test

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/internal/services/keycloaksetup"
)

type fakeKeycloakClient struct {
	baseURL string

	authenticateCalls    int
	createRealmCalls     int
	importClientPayloads []map[string]any
	createGroupCalls     int
	createUserCalls      int
	createUserReturn     func(callIndex int) string
	getUsersCalls        int
	getGroupsCalls       int
	addUserToGroupCalls  int
	fetchDescriptorCalls int
}

func (f *fakeKeycloakClient) BaseURL() string { return f.baseURL }

func (f *fakeKeycloakClient) Authenticate(context.Context, string, string) error {
	f.authenticateCalls++
	return nil
}

func (f *fakeKeycloakClient) CreateRealm(context.Context, string) error {
	f.createRealmCalls++
	return nil
}

func (f *fakeKeycloakClient) ImportClient(_ context.Context, _ string, payload map[string]any) error {
	f.importClientPayloads = append(f.importClientPayloads, payload)
	return nil
}

func (f *fakeKeycloakClient) CreateGroup(context.Context, string, string) error {
	f.createGroupCalls++
	return nil
}

func (f *fakeKeycloakClient) CreateUser(context.Context, string, map[string]any) (string, error) {
	idx := f.createUserCalls
	f.createUserCalls++
	if f.createUserReturn != nil {
		return f.createUserReturn(idx), nil
	}
	return "fake_user_id", nil
}

func (f *fakeKeycloakClient) GetUsers(context.Context, string, string) ([]map[string]any, error) {
	f.getUsersCalls++
	return []map[string]any{{"id": "looked-up-user-id"}}, nil
}

func (f *fakeKeycloakClient) GetGroups(context.Context, string, string) ([]map[string]any, error) {
	f.getGroupsCalls++
	return []map[string]any{{"id": "fake_group_id"}}, nil
}

func (f *fakeKeycloakClient) AddUserToGroup(context.Context, string, string, string) error {
	f.addUserToGroupCalls++
	return nil
}

func (f *fakeKeycloakClient) FetchSAMLDescriptor(context.Context, string) (string, error) {
	f.fetchDescriptorCalls++
	return "<xml/>", nil
}

func TestSetup_OrchestratesRealmClientGroupAndUserCreation(t *testing.T) {
	client := &fakeKeycloakClient{baseURL: "http://keycloak.example"}
	svc := keycloaksetup.New(client, nil)

	result, err := svc.Setup(context.Background(), "admin", "pass")
	require.NoError(t, err)

	assert.Equal(t, 1, client.authenticateCalls)
	assert.Equal(t, 1, client.createRealmCalls)

	require.Len(t, client.importClientPayloads, 2)
	assert.Equal(t, "openid-connect", client.importClientPayloads[0]["protocol"])
	assert.Equal(t, "saml", client.importClientPayloads[1]["protocol"])

	assert.Equal(t, 3, client.createGroupCalls)
	assert.Equal(t, 3, client.createUserCalls)
	assert.Equal(t, 3, client.getGroupsCalls)
	assert.Equal(t, 3, client.addUserToGroupCalls)
	assert.Equal(t, 0, client.getUsersCalls, "should not need the existing-user lookup when CreateUser succeeds")

	assert.Equal(t, 1, client.fetchDescriptorCalls)
	assert.Equal(t, "<xml/>", result.XML)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("<xml/>")), result.B64)
}

func TestSetup_FallsBackToLookupWhenUserAlreadyExists(t *testing.T) {
	client := &fakeKeycloakClient{
		baseURL:          "http://keycloak.example",
		createUserReturn: func(int) string { return "" }, // simulate "already exists"
	}
	svc := keycloaksetup.New(client, nil)

	_, err := svc.Setup(context.Background(), "admin", "pass")
	require.NoError(t, err)

	assert.Equal(t, 3, client.getUsersCalls)
	assert.Equal(t, 3, client.addUserToGroupCalls, "should still assign the looked-up user to its group")
}
