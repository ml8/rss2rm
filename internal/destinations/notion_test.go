package destinations

import (
	"context"
	"rss2rm/internal/service"
	"strings"
	"testing"
	"time"
)

func TestNotionDestination_Init(t *testing.T) {
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
			},
			wantErr: false,
		},
		{
			name: "valid config with parent page",
			config: map[string]string{
				"client_id":      "test-client-id",
				"client_secret":  "test-client-secret",
				"parent_page_id": "abc123",
			},
			wantErr: false,
		},
		{
			name: "valid config with tokens",
			config: map[string]string{
				"client_id":     "test-client-id",
				"client_secret": "test-client-secret",
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
			},
			wantErr: true,
			errMsg:  "client_id is required",
		},
		{
			name: "missing client_secret",
			config: map[string]string{
				"client_id": "test-client-id",
			},
			wantErr: true,
			errMsg:  "client_secret is required",
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
			d := &NotionDestination{}
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

func TestNewNotionDestination(t *testing.T) {
	expiry := time.Now().Add(time.Hour)
	config := map[string]string{
		"client_id":      "test-client-id",
		"client_secret":  "test-client-secret",
		"parent_page_id": "page-123",
		"access_token":   "test-access",
		"refresh_token":  "test-refresh",
		"token_expiry":   expiry.Format(time.RFC3339),
	}

	d := NewNotionDestination(config)

	if d.ClientID != "test-client-id" {
		t.Errorf("ClientID = %q, want %q", d.ClientID, "test-client-id")
	}
	if d.ClientSecret != "test-client-secret" {
		t.Errorf("ClientSecret = %q, want %q", d.ClientSecret, "test-client-secret")
	}
	if d.ParentPageID != "page-123" {
		t.Errorf("ParentPageID = %q, want %q", d.ParentPageID, "page-123")
	}
	if d.AccessToken != "test-access" {
		t.Errorf("AccessToken = %q, want %q", d.AccessToken, "test-access")
	}
	if d.RefreshToken != "test-refresh" {
		t.Errorf("RefreshToken = %q, want %q", d.RefreshToken, "test-refresh")
	}
}

func TestNotionDestination_Type(t *testing.T) {
	d := &NotionDestination{}
	if d.Type() != "notion" {
		t.Errorf("Type() = %q, want %q", d.Type(), "notion")
	}
}

func TestNotionDestination_HasValidToken(t *testing.T) {
	tests := []struct {
		name string
		dest *NotionDestination
		want bool
	}{
		{
			name: "no tokens",
			dest: &NotionDestination{},
			want: false,
		},
		{
			name: "only access token is sufficient for Notion",
			dest: &NotionDestination{
				AccessToken: "token",
			},
			want: true,
		},
		{
			name: "both tokens",
			dest: &NotionDestination{
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

func TestNotionDestination_NeedsAuth(t *testing.T) {
	tests := []struct {
		name string
		dest *NotionDestination
		want bool
	}{
		{
			name: "needs auth when no tokens",
			dest: &NotionDestination{},
			want: true,
		},
		{
			name: "no auth needed when has access token",
			dest: &NotionDestination{
				AccessToken: "access",
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

func TestNotionDestination_GetAuthURL(t *testing.T) {
	d := &NotionDestination{
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
		"response_type=code",
		"owner=user",
	}

	for _, part := range requiredParts {
		if !strings.Contains(url, part) {
			t.Errorf("URL missing %q: %s", part, url)
		}
	}

	// Should start with Notion auth URL
	if !strings.HasPrefix(url, "https://api.notion.com/v1/oauth/authorize") {
		t.Errorf("URL should start with Notion auth endpoint: %s", url)
	}
}

func TestNotionDestination_SetParentPageID(t *testing.T) {
	d := &NotionDestination{}

	d.SetParentPageID("new-page-id")

	if d.ParentPageID != "new-page-id" {
		t.Errorf("ParentPageID = %q, want %q", d.ParentPageID, "new-page-id")
	}
	if !d.configChanged {
		t.Error("configChanged should be true after SetParentPageID")
	}
}

func TestNotionDestination_GetUpdatedConfig(t *testing.T) {
	t.Run("returns nil when config unchanged", func(t *testing.T) {
		d := &NotionDestination{
			ClientID:     "id",
			ClientSecret: "secret",
		}

		config := d.GetUpdatedConfig()
		if config != nil {
			t.Errorf("GetUpdatedConfig() = %v, want nil", config)
		}
	})

	t.Run("returns config when changed", func(t *testing.T) {
		d := &NotionDestination{
			ClientID:      "id",
			ClientSecret:  "secret",
			AccessToken:   "access",
			RefreshToken:  "refresh",
			ParentPageID:  "page-id",
			TokenExpiry:   time.Now().Add(time.Hour),
			configChanged: true,
		}

		config := d.GetUpdatedConfig()
		if config == nil {
			t.Fatal("GetUpdatedConfig() returned nil, want config")
		}

		if config["access_token"] != "access" {
			t.Errorf("access_token = %q, want %q", config["access_token"], "access")
		}
		if config["parent_page_id"] != "page-id" {
			t.Errorf("parent_page_id = %q, want %q", config["parent_page_id"], "page-id")
		}
	})
}

func TestNotionDestination_Upload_RequiresAuth(t *testing.T) {
	d := &NotionDestination{
		ClientID:     "id",
		ClientSecret: "secret",
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

func TestNotionDestination_TestConnection_RequiresAuth(t *testing.T) {
	d := &NotionDestination{
		ClientID:     "id",
		ClientSecret: "secret",
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

func TestNotionDestination_BuildPageRequest(t *testing.T) {
	d := &NotionDestination{
		ParentPageID: "parent-page-123",
	}

	pageData := d.buildPageRequest("Test Article", "My Feed")

	// Check that parent is set correctly
	parent, ok := pageData["parent"].(map[string]string)
	if !ok {
		t.Fatal("parent should be a map[string]string")
	}
	if parent["page_id"] != "parent-page-123" {
		t.Errorf("parent page_id = %q, want %q", parent["page_id"], "parent-page-123")
	}

	// Check that properties contain title
	props, ok := pageData["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("properties should be a map")
	}
	title, ok := props["title"].(map[string]interface{})
	if !ok {
		t.Fatal("title property should be a map")
	}
	titleArray, ok := title["title"].([]map[string]interface{})
	if !ok {
		t.Fatal("title.title should be an array")
	}
	if len(titleArray) == 0 {
		t.Fatal("title array should not be empty")
	}
	textMap, ok := titleArray[0]["text"].(map[string]string)
	if !ok {
		t.Fatal("text should be a map")
	}
	if textMap["content"] != "Test Article" {
		t.Errorf("title content = %q, want %q", textMap["content"], "Test Article")
	}

	// Check that children blocks exist
	children, ok := pageData["children"].([]map[string]interface{})
	if !ok {
		t.Fatal("children should be an array")
	}
	if len(children) < 2 {
		t.Error("should have at least 2 content blocks")
	}
}

func TestNotionDestination_InterfaceCompliance(t *testing.T) {
	// Compile-time interface compliance checks
	var _ service.Destination = &NotionDestination{}
	var _ service.ConfigUpdater = &NotionDestination{}
	var _ service.OAuthDestination = &NotionDestination{}
}
