package destinations

import (
	"context"
	"rss2rm/internal/service"
	"testing"
	"time"
)

func TestGmailDestination_Init(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]string
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config without tokens",
			config: map[string]string{
				"client_id":     "test-client-id",
				"client_secret": "test-client-secret",
				"to_email":      "test@example.com",
			},
			wantErr: false,
		},
		{
			name: "valid config with tokens",
			config: map[string]string{
				"client_id":     "test-client-id",
				"client_secret": "test-client-secret",
				"to_email":      "test@example.com",
				"access_token":  "token",
				"refresh_token": "refresh",
				"token_expiry":  time.Now().Format(time.RFC3339),
			},
			wantErr: false,
		},
		{
			name: "missing client_id",
			config: map[string]string{
				"client_secret": "test-client-secret",
				"to_email":      "test@example.com",
			},
			wantErr: true,
			errMsg:  "client_id is required",
		},
		{
			name: "missing client_secret",
			config: map[string]string{
				"client_id": "test-client-id",
				"to_email":  "test@example.com",
			},
			wantErr: true,
			errMsg:  "client_secret is required",
		},
		{
			name: "missing to_email",
			config: map[string]string{
				"client_id":     "test-client-id",
				"client_secret": "test-client-secret",
			},
			wantErr: true,
			errMsg:  "to_email is required",
		},
		{
			name:    "empty config",
			config:  map[string]string{},
			wantErr: true,
			errMsg:  "client_id is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &GmailDestination{}
			_, err := d.Init(context.Background(), tt.config)

			if (err != nil) != tt.wantErr {
				t.Errorf("Init() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && tt.errMsg != "" {
				if err.Error() != tt.errMsg {
					t.Errorf("Init() error = %q, want %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

func TestNewGmailDestination(t *testing.T) {
	expiry := time.Now().Add(time.Hour)
	config := map[string]string{
		"client_id":     "test-client-id",
		"client_secret": "test-client-secret",
		"to_email":      "test@example.com",
		"access_token":  "test-access",
		"refresh_token": "test-refresh",
		"token_expiry":  expiry.Format(time.RFC3339),
	}

	d := NewGmailDestination(config)

	if d.ClientID != "test-client-id" {
		t.Errorf("ClientID = %q, want %q", d.ClientID, "test-client-id")
	}
	if d.ClientSecret != "test-client-secret" {
		t.Errorf("ClientSecret = %q, want %q", d.ClientSecret, "test-client-secret")
	}
	if d.ToEmail != "test@example.com" {
		t.Errorf("ToEmail = %q, want %q", d.ToEmail, "test@example.com")
	}
	if d.AccessToken != "test-access" {
		t.Errorf("AccessToken = %q, want %q", d.AccessToken, "test-access")
	}
	if d.RefreshToken != "test-refresh" {
		t.Errorf("RefreshToken = %q, want %q", d.RefreshToken, "test-refresh")
	}
}

func TestGmailDestination_Type(t *testing.T) {
	d := &GmailDestination{}
	if d.Type() != "gmail" {
		t.Errorf("Type() = %q, want %q", d.Type(), "gmail")
	}
}

func TestGmailDestination_HasValidToken(t *testing.T) {
	tests := []struct {
		name string
		dest *GmailDestination
		want bool
	}{
		{
			name: "no tokens",
			dest: &GmailDestination{},
			want: false,
		},
		{
			name: "only access token",
			dest: &GmailDestination{
				AccessToken: "token",
			},
			want: false,
		},
		{
			name: "only refresh token",
			dest: &GmailDestination{
				RefreshToken: "token",
			},
			want: false,
		},
		{
			name: "both tokens",
			dest: &GmailDestination{
				AccessToken:  "access",
				RefreshToken: "refresh",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dest.HasValidToken()
			if got != tt.want {
				t.Errorf("HasValidToken() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGmailDestination_NeedsAuth(t *testing.T) {
	tests := []struct {
		name string
		dest *GmailDestination
		want bool
	}{
		{
			name: "needs auth when no tokens",
			dest: &GmailDestination{},
			want: true,
		},
		{
			name: "no auth needed when has tokens",
			dest: &GmailDestination{
				AccessToken:  "access",
				RefreshToken: "refresh",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dest.NeedsAuth()
			if got != tt.want {
				t.Errorf("NeedsAuth() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGmailDestination_GetAuthURL(t *testing.T) {
	d := &GmailDestination{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
	}

	url := d.GetAuthURL("http://localhost:8080/callback", "state-123")

	if url == "" {
		t.Error("GetAuthURL() returned empty string")
	}

	// Should contain required OAuth parameters
	requiredParts := []string{
		"client_id=test-client-id",
		"state=state-123",
	}

	for _, part := range requiredParts {
		if !containsSubstring(url, part) {
			t.Errorf("URL missing %q: %s", part, url)
		}
	}
}

func TestGmailDestination_SetTokens(t *testing.T) {
	d := &GmailDestination{}
	expiry := time.Now().Add(time.Hour)

	d.SetTokens("new-access", "new-refresh", expiry)

	if d.AccessToken != "new-access" {
		t.Errorf("AccessToken = %q, want %q", d.AccessToken, "new-access")
	}
	if d.RefreshToken != "new-refresh" {
		t.Errorf("RefreshToken = %q, want %q", d.RefreshToken, "new-refresh")
	}
	if !d.TokenExpiry.Equal(expiry) {
		t.Errorf("TokenExpiry = %v, want %v", d.TokenExpiry, expiry)
	}
	if !d.configChanged {
		t.Error("configChanged should be true after SetTokens")
	}
}

func TestGmailDestination_GetUpdatedConfig(t *testing.T) {
	t.Run("returns nil when config unchanged", func(t *testing.T) {
		d := &GmailDestination{
			ClientID:     "id",
			ClientSecret: "secret",
			ToEmail:      "test@example.com",
		}

		config := d.GetUpdatedConfig()
		if config != nil {
			t.Errorf("GetUpdatedConfig() = %v, want nil", config)
		}
	})

	t.Run("returns config when changed", func(t *testing.T) {
		d := &GmailDestination{
			ClientID:     "id",
			ClientSecret: "secret",
			ToEmail:      "test@example.com",
		}

		expiry := time.Now().Add(time.Hour)
		d.SetTokens("access", "refresh", expiry)

		config := d.GetUpdatedConfig()
		if config == nil {
			t.Fatal("GetUpdatedConfig() returned nil, want config")
		}

		if config["access_token"] != "access" {
			t.Errorf("access_token = %q, want %q", config["access_token"], "access")
		}
		if config["refresh_token"] != "refresh" {
			t.Errorf("refresh_token = %q, want %q", config["refresh_token"], "refresh")
		}
		if config["client_id"] != "id" {
			t.Errorf("client_id = %q, want %q", config["client_id"], "id")
		}
	})
}

func TestGmailDestination_Upload_RequiresAuth(t *testing.T) {
	d := &GmailDestination{
		ClientID:     "id",
		ClientSecret: "secret",
		ToEmail:      "test@example.com",
		// No tokens set
	}

	_, err := d.Upload(context.Background(), "/tmp/test.pdf", "target")
	if err == nil {
		t.Error("Upload() should fail without auth")
	}

	if !containsSubstring(err.Error(), "not authorized") {
		t.Errorf("error should mention authorization: %v", err)
	}
}

func TestGmailDestination_TestConnection_RequiresAuth(t *testing.T) {
	d := &GmailDestination{
		ClientID:     "id",
		ClientSecret: "secret",
		ToEmail:      "test@example.com",
		// No tokens set
	}

	err := d.TestConnection(context.Background())
	if err == nil {
		t.Error("TestConnection() should fail without auth")
	}

	if !containsSubstring(err.Error(), "not authorized") {
		t.Errorf("error should mention authorization: %v", err)
	}
}

func TestGmailDestination_InterfaceCompliance(t *testing.T) {
	// Compile-time interface compliance checks
	var _ service.Destination = &GmailDestination{}
	var _ service.ConfigUpdater = &GmailDestination{}
	var _ service.OAuthDestination = &GmailDestination{}
}

// helper function
func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
