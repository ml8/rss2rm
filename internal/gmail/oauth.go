// Package gmail provides OAuth2 configuration and an API client
// for sending emails with attachments via the Gmail API.
package gmail

import (
	"context"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
)

// OAuthConfig holds the configuration for Gmail OAuth2.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// Token represents stored OAuth2 tokens.
type Token struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
}

// NewOAuthConfig creates a new OAuth2 configuration for Gmail.
func NewOAuthConfig(clientID, clientSecret, redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{gmail.GmailSendScope},
		Endpoint:     google.Endpoint,
	}
}

// GetAuthURL returns the URL for the user to authorize the application.
// The state parameter should contain the destination ID to identify which
// destination to update after the OAuth flow completes.
func GetAuthURL(config *oauth2.Config, state string) string {
	return config.AuthCodeURL(state,
		oauth2.AccessTypeOffline,                    // Request refresh token
		oauth2.SetAuthURLParam("prompt", "consent"), // Always show consent to get refresh token
	)
}

// ExchangeCode exchanges an authorization code for access and refresh tokens.
func ExchangeCode(ctx context.Context, config *oauth2.Config, code string) (*oauth2.Token, error) {
	return config.Exchange(ctx, code)
}

// TokenSource creates an oauth2.TokenSource from stored tokens.
// The token source will automatically refresh expired tokens.
func TokenSource(ctx context.Context, config *oauth2.Config, token *Token) oauth2.TokenSource {
	oauthToken := &oauth2.Token{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
		TokenType:    "Bearer",
	}
	return config.TokenSource(ctx, oauthToken)
}

// TokenFromSource extracts the current token from a token source.
// This is useful for persisting refreshed tokens.
func TokenFromSource(ts oauth2.TokenSource) (*Token, error) {
	t, err := ts.Token()
	if err != nil {
		return nil, err
	}
	return &Token{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		Expiry:       t.Expiry,
	}, nil
}

// HasValidToken checks if the token configuration is complete and potentially valid.
func HasValidToken(token *Token) bool {
	return token != nil && token.AccessToken != "" && token.RefreshToken != ""
}
