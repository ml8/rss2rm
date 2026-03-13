package destinations

import (
	"context"
	"fmt"
	"rss2rm/internal/service"
	"rss2rm/internal/uploader"
)

// RemarkableDestination uploads PDFs to a reMarkable tablet via the
// reMarkable cloud API. It implements [service.Destination] and
// [service.ConfigUpdater] to persist refreshed auth tokens.
type RemarkableDestination struct {
	UserToken     string
	DeviceToken   string
	originalToken string // Track original token to detect changes
	configChanged bool
}

// NewRemarkableDestination creates a [RemarkableDestination] from the
// given config map, which should contain "user_token" and "device_token".
func NewRemarkableDestination(config map[string]string) *RemarkableDestination {
	userToken := config["user_token"]
	return &RemarkableDestination{
		UserToken:     userToken,
		DeviceToken:   config["device_token"],
		originalToken: userToken,
		configChanged: false,
	}
}

func (d *RemarkableDestination) Init(ctx context.Context, config map[string]string) (map[string]string, error) {
	// If tokens are already provided, verify them
	if config["user_token"] != "" && config["device_token"] != "" {
		return config, nil
	}

	// If a code is provided, register
	if code, ok := config["code"]; ok && code != "" {
		deviceToken, userToken, err := uploader.RegisterDevice(code)
		if err != nil {
			return nil, err
		}
		return map[string]string{
			"user_token":   userToken,
			"device_token": deviceToken,
		}, nil
	}

	return nil, fmt.Errorf("missing registration code or tokens")
}

func (d *RemarkableDestination) Upload(ctx context.Context, filePath string, targetPath string) (string, error) {
	client, refreshedToken, err := uploader.NewClient(d.UserToken, d.DeviceToken)
	if err != nil {
		return "", err
	}
	if refreshedToken != "" {
		d.UserToken = refreshedToken
		d.configChanged = true
	}

	docID, newRefreshedToken, err := client.Upload(filePath, targetPath)
	if err != nil {
		return "", err
	}

	if newRefreshedToken != "" {
		d.UserToken = newRefreshedToken
		d.configChanged = true
	}

	return docID, nil
}

func (d *RemarkableDestination) Delete(ctx context.Context, remotePath string) error {
	client, refreshedToken, err := uploader.NewClient(d.UserToken, d.DeviceToken)
	if err != nil {
		return err
	}
	if refreshedToken != "" {
		d.UserToken = refreshedToken
		d.configChanged = true
	}
	return client.Delete(remotePath)
}

func (d *RemarkableDestination) TestConnection(ctx context.Context) error {
	client, refreshedToken, err := uploader.NewClient(d.UserToken, d.DeviceToken)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	// Track token refresh even during test
	if refreshedToken != "" {
		d.UserToken = refreshedToken
		d.configChanged = true
	}
	_ = client
	return nil
}

func (d *RemarkableDestination) Type() string {
	return "remarkable"
}

// GetUpdatedConfig implements service.ConfigUpdater.
// Returns the current config if tokens were refreshed, nil otherwise.
func (d *RemarkableDestination) GetUpdatedConfig() map[string]string {
	if !d.configChanged {
		return nil
	}
	return map[string]string{
		"user_token":   d.UserToken,
		"device_token": d.DeviceToken,
	}
}

// Ensure interface compliance
var _ service.Destination = &RemarkableDestination{}
var _ service.ConfigUpdater = &RemarkableDestination{}
