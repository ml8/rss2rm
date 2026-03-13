package service

import (
	"context"
)

// Destination is the interface that all upload targets must implement.
// Each destination handles initialization, file upload, and connection
// testing for a specific backend (e.g., reMarkable, email, Dropbox).
type Destination interface {
	// Init initializes the destination (e.g., interactive auth flow).
	// It returns a config map to be stored in the DB.
	Init(ctx context.Context, config map[string]string) (map[string]string, error)

	// Upload sends a file to the destination.
	// returns the path/ID of the uploaded file or error
	Upload(ctx context.Context, filePath string, targetPath string) (string, error)

	// Delete removes a previously uploaded file from the destination.
	// remotePath is the path or ID returned by Upload.
	Delete(ctx context.Context, remotePath string) error

	// TestConnection verifies that the destination is reachable and credentials are valid.
	TestConnection(ctx context.Context) error

	// Type returns the identifier string (e.g., "remarkable")
	Type() string
}

// ConfigUpdater is an optional interface that destinations can implement
// if their config may change during operation (e.g., token refresh).
// After Upload(), the caller should check if the destination implements
// ConfigUpdater and persist any changes.
type ConfigUpdater interface {
	// GetUpdatedConfig returns the current config if it has changed since creation.
	// Returns nil if no changes occurred.
	GetUpdatedConfig() map[string]string
}

// OAuthDestination is an optional interface for destinations that require
// OAuth2 authorization (e.g., Gmail, Dropbox). The server uses this interface
// to handle OAuth flows generically without knowing about specific providers.
type OAuthDestination interface {
	// NeedsAuth returns true if the destination requires OAuth authorization.
	NeedsAuth() bool

	// GetAuthURL returns the OAuth authorization URL for the user to visit.
	// redirectURL is the callback URL, state is used to identify the destination.
	GetAuthURL(redirectURL, state string) string

	// ExchangeCode exchanges an authorization code for access/refresh tokens.
	// This updates the destination's internal token state.
	ExchangeCode(ctx context.Context, redirectURL, code string) error
}
