package destinations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"rss2rm/internal/service"
	"strings"
	"time"
)

// Dropbox OAuth2 endpoints
const (
	dropboxAuthURL   = "https://www.dropbox.com/oauth2/authorize"
	dropboxTokenURL  = "https://api.dropboxapi.com/oauth2/token"
	dropboxUploadURL = "https://content.dropboxapi.com/2/files/upload"
)

// DropboxDestination uploads files to Dropbox.
type DropboxDestination struct {
	AppKey       string // OAuth client ID
	AppSecret    string // OAuth client secret
	AccessToken  string
	RefreshToken string
	TokenExpiry  time.Time
	FolderPath   string // Target folder in Dropbox (e.g., "/rss2rm")

	configChanged bool
}

// NewDropboxDestination creates a new Dropbox destination from config.
func NewDropboxDestination(config map[string]string) *DropboxDestination {
	var expiry time.Time
	if exp := config["token_expiry"]; exp != "" {
		expiry, _ = time.Parse(time.RFC3339, exp)
	}

	return &DropboxDestination{
		AppKey:        config["app_key"],
		AppSecret:     config["app_secret"],
		AccessToken:   config["access_token"],
		RefreshToken:  config["refresh_token"],
		TokenExpiry:   expiry,
		FolderPath:    config["folder_path"],
		configChanged: false,
	}
}

// Init validates the Dropbox destination configuration.
func (d *DropboxDestination) Init(ctx context.Context, config map[string]string) (map[string]string, error) {
	if config["app_key"] == "" {
		return nil, fmt.Errorf("app_key is required")
	}
	if config["app_secret"] == "" {
		return nil, fmt.Errorf("app_secret is required")
	}

	// Default folder path
	folderPath := config["folder_path"]
	if folderPath == "" {
		folderPath = "/rss2rm"
	}
	// Ensure path starts with /
	if !strings.HasPrefix(folderPath, "/") {
		folderPath = "/" + folderPath
	}

	return map[string]string{
		"app_key":       config["app_key"],
		"app_secret":    config["app_secret"],
		"access_token":  config["access_token"],
		"refresh_token": config["refresh_token"],
		"token_expiry":  config["token_expiry"],
		"folder_path":   folderPath,
	}, nil
}

// Upload sends the file to Dropbox.
func (d *DropboxDestination) Upload(ctx context.Context, filePath string, targetPath string) (string, error) {
	if !d.HasValidToken() {
		return "", fmt.Errorf("Dropbox destination not authorized. Please complete OAuth flow first")
	}

	// Refresh token if expired
	if err := d.refreshTokenIfNeeded(ctx); err != nil {
		return "", fmt.Errorf("failed to refresh token: %w", err)
	}

	// Read the file
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	// Build the Dropbox path
	filename := filepath.Base(filePath)
	dropboxPath := d.FolderPath
	if targetPath != "" {
		dropboxPath = filepath.Join(d.FolderPath, targetPath)
	}
	dropboxPath = filepath.Join(dropboxPath, filename)
	// Ensure forward slashes for Dropbox API
	dropboxPath = strings.ReplaceAll(dropboxPath, "\\", "/")

	// Prepare the Dropbox-API-Arg header
	apiArg := map[string]interface{}{
		"path":            dropboxPath,
		"mode":            "overwrite",
		"autorename":      false,
		"mute":            false,
		"strict_conflict": false,
	}
	apiArgJSON, err := json.Marshal(apiArg)
	if err != nil {
		return "", fmt.Errorf("failed to marshal API arg: %w", err)
	}

	// Create the upload request
	req, err := http.NewRequestWithContext(ctx, "POST", dropboxUploadURL, bytes.NewReader(fileData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+d.AccessToken)
	req.Header.Set("Dropbox-API-Arg", string(apiArgJSON))
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, string(body))
	}

	return dropboxPath, nil
}

func (d *DropboxDestination) Delete(ctx context.Context, remotePath string) error {
	return nil // Dropbox deletion not supported
}

// TestConnection verifies the Dropbox connection.
func (d *DropboxDestination) TestConnection(ctx context.Context) error {
	if !d.HasValidToken() {
		return fmt.Errorf("Dropbox destination not authorized. Please complete OAuth flow first")
	}

	// Refresh token if needed
	if err := d.refreshTokenIfNeeded(ctx); err != nil {
		return fmt.Errorf("failed to refresh token: %w", err)
	}

	// Call account info endpoint to verify token works
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.dropboxapi.com/2/users/get_current_account", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+d.AccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("connection test failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("connection test failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Type returns the destination type identifier.
func (d *DropboxDestination) Type() string {
	return "dropbox"
}

// GetUpdatedConfig implements service.ConfigUpdater.
func (d *DropboxDestination) GetUpdatedConfig() map[string]string {
	if !d.configChanged {
		return nil
	}
	return map[string]string{
		"app_key":       d.AppKey,
		"app_secret":    d.AppSecret,
		"access_token":  d.AccessToken,
		"refresh_token": d.RefreshToken,
		"token_expiry":  d.TokenExpiry.Format(time.RFC3339),
		"folder_path":   d.FolderPath,
	}
}

// HasValidToken checks if the destination has valid OAuth tokens.
func (d *DropboxDestination) HasValidToken() bool {
	return d.AccessToken != "" && d.RefreshToken != ""
}

// NeedsAuth returns true if the destination needs OAuth authorization.
func (d *DropboxDestination) NeedsAuth() bool {
	return !d.HasValidToken()
}

// GetAuthURL returns the OAuth authorization URL for this destination.
func (d *DropboxDestination) GetAuthURL(redirectURL, state string) string {
	params := url.Values{
		"client_id":         {d.AppKey},
		"redirect_uri":      {redirectURL},
		"response_type":     {"code"},
		"token_access_type": {"offline"}, // Request refresh token
		"state":             {state},
	}
	return dropboxAuthURL + "?" + params.Encode()
}

// ExchangeCode exchanges an authorization code for access/refresh tokens.
func (d *DropboxDestination) ExchangeCode(ctx context.Context, redirectURL, code string) error {
	data := url.Values{
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURL},
		"client_id":     {d.AppKey},
		"client_secret": {d.AppSecret},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", dropboxTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode token response: %w", err)
	}

	d.AccessToken = tokenResp.AccessToken
	d.RefreshToken = tokenResp.RefreshToken
	if tokenResp.ExpiresIn > 0 {
		d.TokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}
	d.configChanged = true
	return nil
}

// refreshTokenIfNeeded refreshes the access token if it's expired or about to expire.
func (d *DropboxDestination) refreshTokenIfNeeded(ctx context.Context) error {
	// Refresh if token expires within 5 minutes
	if d.TokenExpiry.IsZero() || time.Now().Add(5*time.Minute).Before(d.TokenExpiry) {
		return nil
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {d.RefreshToken},
		"client_id":     {d.AppKey},
		"client_secret": {d.AppSecret},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", dropboxTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token refresh failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return err
	}

	d.AccessToken = tokenResp.AccessToken
	if tokenResp.ExpiresIn > 0 {
		d.TokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}
	d.configChanged = true
	return nil
}

// Ensure interface compliance
var _ service.Destination = &DropboxDestination{}
var _ service.ConfigUpdater = &DropboxDestination{}
var _ service.OAuthDestination = &DropboxDestination{}
