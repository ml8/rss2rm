package destinations

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"rss2rm/internal/service"
	"strings"
	"time"
)

// Notion OAuth2 endpoints
const (
	notionAuthURL  = "https://api.notion.com/v1/oauth/authorize"
	notionTokenURL = "https://api.notion.com/v1/oauth/token"
	notionAPIBase  = "https://api.notion.com/v1"
	notionVersion  = "2022-06-28"
)

// NotionDestination creates pages in Notion for each article.
type NotionDestination struct {
	ClientID     string // OAuth client ID
	ClientSecret string // OAuth client secret
	AccessToken  string
	RefreshToken string
	TokenExpiry  time.Time
	ParentPageID string // Parent page ID where new pages will be created

	configChanged bool
}

// NewNotionDestination creates a new Notion destination from config.
func NewNotionDestination(config map[string]string) *NotionDestination {
	var expiry time.Time
	if exp := config["token_expiry"]; exp != "" {
		expiry, _ = time.Parse(time.RFC3339, exp)
	}

	return &NotionDestination{
		ClientID:      config["client_id"],
		ClientSecret:  config["client_secret"],
		AccessToken:   config["access_token"],
		RefreshToken:  config["refresh_token"],
		TokenExpiry:   expiry,
		ParentPageID:  config["parent_page_id"],
		configChanged: false,
	}
}

// Init validates the Notion destination configuration.
func (d *NotionDestination) Init(ctx context.Context, config map[string]string) (map[string]string, error) {
	if config["client_id"] == "" {
		return nil, fmt.Errorf("client_id is required")
	}
	if config["client_secret"] == "" {
		return nil, fmt.Errorf("client_secret is required")
	}
	// parent_page_id is optional - if not set, user must grant access to a page during OAuth

	return map[string]string{
		"client_id":      config["client_id"],
		"client_secret":  config["client_secret"],
		"access_token":   config["access_token"],
		"refresh_token":  config["refresh_token"],
		"token_expiry":   config["token_expiry"],
		"parent_page_id": config["parent_page_id"],
	}, nil
}

// Upload creates a Notion page for the article.
// Note: Notion doesn't support direct file uploads, so we create a page with article info.
// The filePath is used to extract the title, and targetPath provides context (feed name).
func (d *NotionDestination) Upload(ctx context.Context, filePath string, targetPath string) (string, error) {
	if !d.HasValidToken() {
		return "", fmt.Errorf("Notion destination not authorized. Please complete OAuth flow first")
	}

	// Refresh token if needed
	if err := d.refreshTokenIfNeeded(ctx); err != nil {
		return "", fmt.Errorf("failed to refresh token: %w", err)
	}

	// Extract title from filename (remove extension)
	filename := filepath.Base(filePath)
	title := strings.TrimSuffix(filename, filepath.Ext(filename))

	// Build the page creation request
	pageData := d.buildPageRequest(title, targetPath)

	reqBody, err := json.Marshal(pageData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal page data: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", notionAPIBase+"/pages", bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+d.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Notion-Version", notionVersion)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("page creation failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("page creation failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response to get page URL
	var pageResp struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pageResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return pageResp.URL, nil
}

// buildPageRequest creates the Notion API request body for creating a page.
func (d *NotionDestination) buildPageRequest(title, feedName string) map[string]interface{} {
	// Build content blocks
	children := []map[string]interface{}{
		{
			"object": "block",
			"type":   "paragraph",
			"paragraph": map[string]interface{}{
				"rich_text": []map[string]interface{}{
					{
						"type": "text",
						"text": map[string]string{
							"content": fmt.Sprintf("Imported from RSS feed: %s", feedName),
						},
					},
				},
			},
		},
		{
			"object": "block",
			"type":   "paragraph",
			"paragraph": map[string]interface{}{
				"rich_text": []map[string]interface{}{
					{
						"type": "text",
						"text": map[string]string{
							"content": fmt.Sprintf("Date: %s", time.Now().Format("January 2, 2006")),
						},
					},
				},
			},
		},
		{
			"object":  "block",
			"type":    "divider",
			"divider": map[string]interface{}{},
		},
	}

	pageData := map[string]interface{}{
		"properties": map[string]interface{}{
			"title": map[string]interface{}{
				"title": []map[string]interface{}{
					{
						"text": map[string]string{
							"content": title,
						},
					},
				},
			},
		},
		"children": children,
	}

	// Set parent - either specified page or will use the page granted during OAuth
	if d.ParentPageID != "" {
		pageData["parent"] = map[string]string{
			"page_id": d.ParentPageID,
		}
	} else {
		// When no parent is specified, use the first page the user granted access to
		// This requires the user to have selected a page during OAuth
		// For now, we'll need to search for an available page
		pageData["parent"] = map[string]string{
			"page_id": d.ParentPageID, // Will fail if empty, prompting user to set one
		}
	}

	return pageData
}

func (d *NotionDestination) Delete(ctx context.Context, remotePath string) error {
	return nil // Notion deletion not supported
}

// TestConnection verifies the Notion connection.
func (d *NotionDestination) TestConnection(ctx context.Context) error {
	if !d.HasValidToken() {
		return fmt.Errorf("Notion destination not authorized. Please complete OAuth flow first")
	}

	// Refresh token if needed
	if err := d.refreshTokenIfNeeded(ctx); err != nil {
		return fmt.Errorf("failed to refresh token: %w", err)
	}

	// Call users/me endpoint to verify token works
	req, err := http.NewRequestWithContext(ctx, "GET", notionAPIBase+"/users/me", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+d.AccessToken)
	req.Header.Set("Notion-Version", notionVersion)

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
func (d *NotionDestination) Type() string {
	return "notion"
}

// GetUpdatedConfig implements service.ConfigUpdater.
func (d *NotionDestination) GetUpdatedConfig() map[string]string {
	if !d.configChanged {
		return nil
	}
	return map[string]string{
		"client_id":      d.ClientID,
		"client_secret":  d.ClientSecret,
		"access_token":   d.AccessToken,
		"refresh_token":  d.RefreshToken,
		"token_expiry":   d.TokenExpiry.Format(time.RFC3339),
		"parent_page_id": d.ParentPageID,
	}
}

// HasValidToken checks if the destination has valid OAuth tokens.
func (d *NotionDestination) HasValidToken() bool {
	// Notion may not always return a refresh token for all integration types
	return d.AccessToken != ""
}

// NeedsAuth returns true if the destination needs OAuth authorization.
func (d *NotionDestination) NeedsAuth() bool {
	return !d.HasValidToken()
}

// GetAuthURL returns the OAuth authorization URL for this destination.
func (d *NotionDestination) GetAuthURL(redirectURL, state string) string {
	params := url.Values{
		"client_id":     {d.ClientID},
		"redirect_uri":  {redirectURL},
		"response_type": {"code"},
		"owner":         {"user"},
		"state":         {state},
	}
	return notionAuthURL + "?" + params.Encode()
}

// ExchangeCode exchanges an authorization code for access tokens.
// Notion uses HTTP Basic Auth for the token endpoint.
func (d *NotionDestination) ExchangeCode(ctx context.Context, redirectURL, code string) error {
	reqBody := map[string]string{
		"grant_type":   "authorization_code",
		"code":         code,
		"redirect_uri": redirectURL,
	}
	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", notionTokenURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return err
	}

	// Notion uses Basic Auth with client_id:client_secret
	credentials := base64.StdEncoding.EncodeToString([]byte(d.ClientID + ":" + d.ClientSecret))
	req.Header.Set("Authorization", "Basic "+credentials)
	req.Header.Set("Content-Type", "application/json")

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
		AccessToken          string `json:"access_token"`
		RefreshToken         string `json:"refresh_token"`
		BotID                string `json:"bot_id"`
		DuplicatedTemplateID string `json:"duplicated_template_id"`
		WorkspaceID          string `json:"workspace_id"`
		WorkspaceName        string `json:"workspace_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode token response: %w", err)
	}

	d.AccessToken = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		d.RefreshToken = tokenResp.RefreshToken
	}
	// Store the duplicated template ID as parent if provided
	if tokenResp.DuplicatedTemplateID != "" && d.ParentPageID == "" {
		d.ParentPageID = tokenResp.DuplicatedTemplateID
	}
	d.configChanged = true
	return nil
}

// refreshTokenIfNeeded refreshes the access token if it's expired.
func (d *NotionDestination) refreshTokenIfNeeded(ctx context.Context) error {
	// If no refresh token or no expiry set, skip refresh
	if d.RefreshToken == "" || d.TokenExpiry.IsZero() {
		return nil
	}

	// Refresh if token expires within 5 minutes
	if time.Now().Add(5 * time.Minute).Before(d.TokenExpiry) {
		return nil
	}

	reqBody := map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": d.RefreshToken,
	}
	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", notionTokenURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return err
	}

	credentials := base64.StdEncoding.EncodeToString([]byte(d.ClientID + ":" + d.ClientSecret))
	req.Header.Set("Authorization", "Basic "+credentials)
	req.Header.Set("Content-Type", "application/json")

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
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return err
	}

	d.AccessToken = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		d.RefreshToken = tokenResp.RefreshToken
	}
	d.configChanged = true
	return nil
}

// SetParentPageID sets the parent page ID after OAuth flow.
func (d *NotionDestination) SetParentPageID(pageID string) {
	d.ParentPageID = pageID
	d.configChanged = true
}

// Ensure interface compliance
var _ service.Destination = &NotionDestination{}
var _ service.ConfigUpdater = &NotionDestination{}
var _ service.OAuthDestination = &NotionDestination{}
