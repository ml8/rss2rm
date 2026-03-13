package destinations

import (
	"context"
	"fmt"
	"path/filepath"
	"rss2rm/internal/gmail"
	"rss2rm/internal/service"
	"time"
)

// GmailDestination sends PDFs via Gmail API.
type GmailDestination struct {
	ClientID     string
	ClientSecret string
	AccessToken  string
	RefreshToken string
	TokenExpiry  time.Time
	ToEmail      string

	configChanged bool
}

// NewGmailDestination creates a new Gmail destination from config.
func NewGmailDestination(config map[string]string) *GmailDestination {
	var expiry time.Time
	if exp := config["token_expiry"]; exp != "" {
		expiry, _ = time.Parse(time.RFC3339, exp)
	}

	return &GmailDestination{
		ClientID:      config["client_id"],
		ClientSecret:  config["client_secret"],
		AccessToken:   config["access_token"],
		RefreshToken:  config["refresh_token"],
		TokenExpiry:   expiry,
		ToEmail:       config["to_email"],
		configChanged: false,
	}
}

// Init validates the Gmail destination configuration.
// OAuth flow happens externally via the /api/v1/oauth/callback endpoint.
func (d *GmailDestination) Init(ctx context.Context, config map[string]string) (map[string]string, error) {
	// Validate required fields
	if config["client_id"] == "" {
		return nil, fmt.Errorf("client_id is required")
	}
	if config["client_secret"] == "" {
		return nil, fmt.Errorf("client_secret is required")
	}
	if config["to_email"] == "" {
		return nil, fmt.Errorf("to_email is required")
	}

	// Return config as-is; tokens will be added via OAuth callback
	return map[string]string{
		"client_id":     config["client_id"],
		"client_secret": config["client_secret"],
		"to_email":      config["to_email"],
		"access_token":  config["access_token"],
		"refresh_token": config["refresh_token"],
		"token_expiry":  config["token_expiry"],
	}, nil
}

// Upload sends the file as an email attachment via Gmail.
func (d *GmailDestination) Upload(ctx context.Context, filePath string, targetPath string) (string, error) {
	if !d.HasValidToken() {
		return "", fmt.Errorf("Gmail destination not authorized. Please complete OAuth flow first")
	}

	// Create OAuth config and token source
	// RedirectURL isn't needed for token refresh, but required for config
	oauthConfig := gmail.NewOAuthConfig(d.ClientID, d.ClientSecret, "")
	token := &gmail.Token{
		AccessToken:  d.AccessToken,
		RefreshToken: d.RefreshToken,
		Expiry:       d.TokenExpiry,
	}
	tokenSource := gmail.TokenSource(ctx, oauthConfig, token)

	// Create Gmail client
	client, err := gmail.NewClient(ctx, tokenSource)
	if err != nil {
		return "", fmt.Errorf("failed to create Gmail client: %w", err)
	}

	// Build email subject from filename
	filename := filepath.Base(filePath)
	subject := fmt.Sprintf("RSS Article: %s", targetPath)
	if targetPath == "" {
		subject = fmt.Sprintf("RSS Article: %s", filename)
	}

	body := fmt.Sprintf("Please find the attached article: %s", filename)

	// Send email
	err = client.SendWithAttachment(ctx, d.ToEmail, subject, body, filePath)
	if err != nil {
		return "", fmt.Errorf("failed to send email: %w", err)
	}

	// Check for token refresh
	newToken, err := client.GetCurrentToken()
	if err == nil && newToken.AccessToken != d.AccessToken {
		d.AccessToken = newToken.AccessToken
		d.RefreshToken = newToken.RefreshToken
		d.TokenExpiry = newToken.Expiry
		d.configChanged = true
	}

	return fmt.Sprintf("sent to %s", d.ToEmail), nil
}

func (d *GmailDestination) Delete(ctx context.Context, remotePath string) error {
	return nil // Gmail does not support deletion
}

// TestConnection verifies that the Gmail credentials are valid.
func (d *GmailDestination) TestConnection(ctx context.Context) error {
	if !d.HasValidToken() {
		return fmt.Errorf("Gmail destination not authorized. Please complete OAuth flow first")
	}

	// Create OAuth config and token source
	oauthConfig := gmail.NewOAuthConfig(d.ClientID, d.ClientSecret, "")
	token := &gmail.Token{
		AccessToken:  d.AccessToken,
		RefreshToken: d.RefreshToken,
		Expiry:       d.TokenExpiry,
	}
	tokenSource := gmail.TokenSource(ctx, oauthConfig, token)

	// Create Gmail client
	client, err := gmail.NewClient(ctx, tokenSource)
	if err != nil {
		return fmt.Errorf("failed to create Gmail client: %w", err)
	}

	// Test connection
	err = client.TestConnection(ctx)
	if err != nil {
		return err
	}

	// Check for token refresh
	newToken, err := client.GetCurrentToken()
	if err == nil && newToken.AccessToken != d.AccessToken {
		d.AccessToken = newToken.AccessToken
		d.RefreshToken = newToken.RefreshToken
		d.TokenExpiry = newToken.Expiry
		d.configChanged = true
	}

	return nil
}

// Type returns the destination type identifier.
func (d *GmailDestination) Type() string {
	return "gmail"
}

// GetUpdatedConfig implements service.ConfigUpdater.
// Returns the current config if tokens were refreshed, nil otherwise.
func (d *GmailDestination) GetUpdatedConfig() map[string]string {
	if !d.configChanged {
		return nil
	}
	return map[string]string{
		"client_id":     d.ClientID,
		"client_secret": d.ClientSecret,
		"access_token":  d.AccessToken,
		"refresh_token": d.RefreshToken,
		"token_expiry":  d.TokenExpiry.Format(time.RFC3339),
		"to_email":      d.ToEmail,
	}
}

// HasValidToken checks if the destination has valid OAuth tokens.
func (d *GmailDestination) HasValidToken() bool {
	return d.AccessToken != "" && d.RefreshToken != ""
}

// NeedsAuth returns true if the destination needs OAuth authorization.
func (d *GmailDestination) NeedsAuth() bool {
	return !d.HasValidToken()
}

// GetAuthURL returns the OAuth authorization URL for this destination.
func (d *GmailDestination) GetAuthURL(redirectURL, state string) string {
	oauthConfig := gmail.NewOAuthConfig(d.ClientID, d.ClientSecret, redirectURL)
	return gmail.GetAuthURL(oauthConfig, state)
}

// SetTokens updates the destination with OAuth tokens.
func (d *GmailDestination) SetTokens(accessToken, refreshToken string, expiry time.Time) {
	d.AccessToken = accessToken
	d.RefreshToken = refreshToken
	d.TokenExpiry = expiry
	d.configChanged = true
}

// ExchangeCode exchanges an authorization code for access/refresh tokens.
// Implements service.OAuthDestination.
func (d *GmailDestination) ExchangeCode(ctx context.Context, redirectURL, code string) error {
	oauthConfig := gmail.NewOAuthConfig(d.ClientID, d.ClientSecret, redirectURL)
	token, err := gmail.ExchangeCode(ctx, oauthConfig, code)
	if err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}

	d.AccessToken = token.AccessToken
	d.RefreshToken = token.RefreshToken
	d.TokenExpiry = token.Expiry
	d.configChanged = true
	return nil
}

// Ensure interface compliance
var _ service.Destination = &GmailDestination{}
var _ service.ConfigUpdater = &GmailDestination{}
var _ service.OAuthDestination = &GmailDestination{}
