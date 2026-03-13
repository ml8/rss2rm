package destinations

import (
	"context"
	"rss2rm/internal/service"
	"strings"
	"testing"
	"time"
)

func TestDropboxDestination_Init(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]string
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config without tokens",
			config: map[string]string{
				"app_key":    "test-app-key",
				"app_secret": "test-app-secret",
			},
			wantErr: false,
		},
		{
			name: "valid config with folder path",
			config: map[string]string{
				"app_key":     "test-app-key",
				"app_secret":  "test-app-secret",
				"folder_path": "/my-folder",
			},
			wantErr: false,
		},
		{
			name: "valid config with tokens",
			config: map[string]string{
				"app_key":       "test-app-key",
				"app_secret":    "test-app-secret",
				"access_token":  "token",
				"refresh_token": "refresh",
				"token_expiry":  time.Now().Format(time.RFC3339),
			},
			wantErr: false,
		},
		{
			name: "missing app_key",
			config: map[string]string{
				"app_secret": "test-app-secret",
			},
			wantErr: true,
			errMsg:  "app_key is required",
		},
		{
			name: "missing app_secret",
			config: map[string]string{
				"app_key": "test-app-key",
			},
			wantErr: true,
			errMsg:  "app_secret is required",
		},
		{
			name:    "empty config",
			config:  map[string]string{},
			wantErr: true,
			errMsg:  "app_key is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &DropboxDestination{}
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

func TestDropboxDestination_Init_FolderPathNormalization(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty defaults to /rss2rm",
			input:    "",
			expected: "/rss2rm",
		},
		{
			name:     "path without leading slash gets one",
			input:    "my-folder",
			expected: "/my-folder",
		},
		{
			name:     "path with leading slash preserved",
			input:    "/my-folder",
			expected: "/my-folder",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &DropboxDestination{}
			config, err := d.Init(context.Background(), map[string]string{
				"app_key":     "key",
				"app_secret":  "secret",
				"folder_path": tt.input,
			})
			if err != nil {
				t.Fatalf("Init() error = %v", err)
			}

			if config["folder_path"] != tt.expected {
				t.Errorf("folder_path = %q, want %q", config["folder_path"], tt.expected)
			}
		})
	}
}

func TestNewDropboxDestination(t *testing.T) {
	expiry := time.Now().Add(time.Hour)
	config := map[string]string{
		"app_key":       "test-app-key",
		"app_secret":    "test-app-secret",
		"folder_path":   "/my-folder",
		"access_token":  "test-access",
		"refresh_token": "test-refresh",
		"token_expiry":  expiry.Format(time.RFC3339),
	}

	d := NewDropboxDestination(config)

	if d.AppKey != "test-app-key" {
		t.Errorf("AppKey = %q, want %q", d.AppKey, "test-app-key")
	}
	if d.AppSecret != "test-app-secret" {
		t.Errorf("AppSecret = %q, want %q", d.AppSecret, "test-app-secret")
	}
	if d.FolderPath != "/my-folder" {
		t.Errorf("FolderPath = %q, want %q", d.FolderPath, "/my-folder")
	}
	if d.AccessToken != "test-access" {
		t.Errorf("AccessToken = %q, want %q", d.AccessToken, "test-access")
	}
	if d.RefreshToken != "test-refresh" {
		t.Errorf("RefreshToken = %q, want %q", d.RefreshToken, "test-refresh")
	}
}

func TestDropboxDestination_Type(t *testing.T) {
	d := &DropboxDestination{}
	if d.Type() != "dropbox" {
		t.Errorf("Type() = %q, want %q", d.Type(), "dropbox")
	}
}

func TestDropboxDestination_HasValidToken(t *testing.T) {
	tests := []struct {
		name string
		dest *DropboxDestination
		want bool
	}{
		{
			name: "no tokens",
			dest: &DropboxDestination{},
			want: false,
		},
		{
			name: "only access token",
			dest: &DropboxDestination{
				AccessToken: "token",
			},
			want: false,
		},
		{
			name: "only refresh token",
			dest: &DropboxDestination{
				RefreshToken: "token",
			},
			want: false,
		},
		{
			name: "both tokens",
			dest: &DropboxDestination{
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

func TestDropboxDestination_NeedsAuth(t *testing.T) {
	tests := []struct {
		name string
		dest *DropboxDestination
		want bool
	}{
		{
			name: "needs auth when no tokens",
			dest: &DropboxDestination{},
			want: true,
		},
		{
			name: "no auth needed when has tokens",
			dest: &DropboxDestination{
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

func TestDropboxDestination_GetAuthURL(t *testing.T) {
	d := &DropboxDestination{
		AppKey:    "test-app-key",
		AppSecret: "test-app-secret",
	}

	url := d.GetAuthURL("http://localhost:8080/callback", "state-123")

	if url == "" {
		t.Error("GetAuthURL() returned empty string")
	}

	// Should contain required OAuth parameters
	requiredParts := []string{
		"client_id=test-app-key",
		"state=state-123",
		"response_type=code",
		"token_access_type=offline",
	}

	for _, part := range requiredParts {
		if !strings.Contains(url, part) {
			t.Errorf("URL missing %q: %s", part, url)
		}
	}

	// Should start with Dropbox auth URL
	if !strings.HasPrefix(url, "https://www.dropbox.com/oauth2/authorize") {
		t.Errorf("URL should start with Dropbox auth endpoint: %s", url)
	}
}

func TestDropboxDestination_GetUpdatedConfig(t *testing.T) {
	t.Run("returns nil when config unchanged", func(t *testing.T) {
		d := &DropboxDestination{
			AppKey:     "key",
			AppSecret:  "secret",
			FolderPath: "/test",
		}

		config := d.GetUpdatedConfig()
		if config != nil {
			t.Errorf("GetUpdatedConfig() = %v, want nil", config)
		}
	})

	t.Run("returns config when changed", func(t *testing.T) {
		d := &DropboxDestination{
			AppKey:        "key",
			AppSecret:     "secret",
			FolderPath:    "/test",
			configChanged: true,
			AccessToken:   "access",
			RefreshToken:  "refresh",
			TokenExpiry:   time.Now().Add(time.Hour),
		}

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
	})
}

func TestDropboxDestination_Upload_RequiresAuth(t *testing.T) {
	d := &DropboxDestination{
		AppKey:     "key",
		AppSecret:  "secret",
		FolderPath: "/test",
		// No tokens set
	}

	_, err := d.Upload(context.Background(), "/tmp/test.pdf", "target")
	if err == nil {
		t.Error("Upload() should fail without auth")
	}

	if !strings.Contains(err.Error(), "not authorized") {
		t.Errorf("error should mention authorization: %v", err)
	}
}

func TestDropboxDestination_TestConnection_RequiresAuth(t *testing.T) {
	d := &DropboxDestination{
		AppKey:     "key",
		AppSecret:  "secret",
		FolderPath: "/test",
		// No tokens set
	}

	err := d.TestConnection(context.Background())
	if err == nil {
		t.Error("TestConnection() should fail without auth")
	}

	if !strings.Contains(err.Error(), "not authorized") {
		t.Errorf("error should mention authorization: %v", err)
	}
}

func TestDropboxDestination_InterfaceCompliance(t *testing.T) {
	// Compile-time interface compliance checks
	var _ service.Destination = &DropboxDestination{}
	var _ service.ConfigUpdater = &DropboxDestination{}
	var _ service.OAuthDestination = &DropboxDestination{}
}
