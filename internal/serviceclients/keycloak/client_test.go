package keycloak_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/internal/serviceclients/keycloak"
)

func TestReady(t *testing.T) {
	t.Run("true when the well-known config is reachable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/realms/master/.well-known/openid-configuration", r.URL.Path)
			w.Write([]byte("{}"))
		}))
		defer server.Close()

		client := keycloak.New(server.URL)
		assert.True(t, client.Ready(context.Background()))
	})

	t.Run("false when unreachable", func(t *testing.T) {
		client := keycloak.New("http://127.0.0.1:1")
		assert.False(t, client.Ready(context.Background()))
	})
}

func TestFetchRealmPublicKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/realms/neons", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]string{"public_key": "abcxyz"})
	}))
	defer server.Close()

	client := keycloak.New(server.URL)
	key, err := client.FetchRealmPublicKey(context.Background(), "neons")
	require.NoError(t, err)
	assert.Equal(t, "abcxyz", key)
}

func TestFetchSAMLDescriptor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/realms/neons/protocol/saml/descriptor", r.URL.Path)
		w.Write([]byte("<xml></xml>"))
	}))
	defer server.Close()

	client := keycloak.New(server.URL)
	xml, err := client.FetchSAMLDescriptor(context.Background(), "neons")
	require.NoError(t, err)
	assert.Equal(t, "<xml></xml>", xml)
}

func TestAuthenticate_StoresTokenForSubsequentRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/realms/master/protocol/openid-connect/token":
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "admin-cli", r.FormValue("client_id"))
			assert.Equal(t, "admin", r.FormValue("username"))
			json.NewEncoder(w).Encode(map[string]string{"access_token": "tok123"})
		case "/admin/realms/neons/groups":
			assert.Equal(t, "Bearer tok123", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := keycloak.New(server.URL)
	require.NoError(t, client.Authenticate(context.Background(), "admin", "pass"))
	require.NoError(t, client.CreateGroup(context.Background(), "neons", "CN=Admins"))
}

func TestGetUsers_RequiresAuthenticationFirst(t *testing.T) {
	client := keycloak.New("http://example.invalid")
	_, err := client.GetUsers(context.Background(), "neons", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestCreateUser_ParsesIDFromLocationHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/realms/master/protocol/openid-connect/token":
			json.NewEncoder(w).Encode(map[string]string{"access_token": "tok"})
		case "/admin/realms/neons/users":
			w.Header().Set("Location", "https://kc/admin/realms/neons/users/user-abc-123")
			w.WriteHeader(http.StatusCreated)
		}
	}))
	defer server.Close()

	client := keycloak.New(server.URL)
	require.NoError(t, client.Authenticate(context.Background(), "admin", "pass"))

	id, err := client.CreateUser(context.Background(), "neons", map[string]any{"username": "admin"})
	require.NoError(t, err)
	assert.Equal(t, "user-abc-123", id)
}

func TestCreateRealm_TreatsConflictAsSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/realms/master/protocol/openid-connect/token":
			json.NewEncoder(w).Encode(map[string]string{"access_token": "tok"})
		case "/admin/realms":
			w.WriteHeader(http.StatusConflict)
		}
	}))
	defer server.Close()

	client := keycloak.New(server.URL)
	require.NoError(t, client.Authenticate(context.Background(), "admin", "pass"))
	require.NoError(t, client.CreateRealm(context.Background(), "neons"))
}
