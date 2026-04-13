// Package uploader provides a client for the reMarkable cloud API,
// handling authentication, token management, and file uploads.
package uploader

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"sync"

	"github.com/juruen/rmapi/api"
	"github.com/juruen/rmapi/config"
	"github.com/juruen/rmapi/model"
	"github.com/juruen/rmapi/transport"
)

// Client communicates with the reMarkable cloud API to upload documents.
type Client struct {
	apiCtx      api.ApiCtx
	deviceToken string
	userToken   string
	mu          sync.Mutex
}

// NewClient returns a Client and optionally a refreshed UserToken if it was updated.
func NewClient(userToken, deviceToken string) (*Client, string, error) {
	if userToken == "" || deviceToken == "" {
		return nil, "", fmt.Errorf("user_token and device_token are required")
	}

	client := &Client{
		deviceToken: deviceToken,
		userToken:   userToken,
	}

	refreshedToken, err := client.authenticate(userToken)
	if err != nil {
		return nil, "", err
	}

	return client, refreshedToken, nil
}

// authenticate creates API context, refreshing token if needed. Returns new token if refreshed.
func (c *Client) authenticate(userToken string) (string, error) {
	tokens := model.AuthTokens{
		UserToken:   userToken,
		DeviceToken: c.deviceToken,
	}

	httpCtx := transport.CreateHttpClientCtx(tokens)
	apiCtx, err := api.CreateApiCtx(&httpCtx, api.Version15)
	if err == nil {
		c.apiCtx = apiCtx
		c.userToken = userToken
		return "", nil
	}

	slog.Warn("auth failed, attempting token refresh", "component", "uploader", "error", err)

	newUserToken, err := refreshUserToken(&httpCtx)
	if err != nil {
		return "", fmt.Errorf("auth failed and refresh failed: %w", err)
	}

	slog.Info("token refreshed successfully", "component", "uploader")

	tokens.UserToken = newUserToken
	httpCtx = transport.CreateHttpClientCtx(tokens)

	apiCtx, err = api.CreateApiCtx(&httpCtx, api.Version15)
	if err != nil {
		return "", fmt.Errorf("auth failed after refresh: %w", err)
	}

	c.apiCtx = apiCtx
	c.userToken = newUserToken
	return newUserToken, nil
}

// refreshAndRetry refreshes the token and returns the new token if successful.
func (c *Client) refreshAndRetry() (string, error) {
	slog.Info("refreshing token due to 401", "component", "uploader")

	tokens := model.AuthTokens{
		DeviceToken: c.deviceToken,
	}
	httpCtx := transport.CreateHttpClientCtx(tokens)

	newUserToken, err := refreshUserToken(&httpCtx)
	if err != nil {
		return "", fmt.Errorf("token refresh failed: %w", err)
	}

	refreshedToken, err := c.authenticate(newUserToken)
	if err != nil {
		return "", err
	}

	if refreshedToken != "" {
		return refreshedToken, nil
	}
	return newUserToken, nil
}

// RegisterDevice exchanges a one-time code for device and user tokens.
func RegisterDevice(code string) (string, string, error) {
	const newDeviceURL = "https://webapp-prod.cloud.remarkable.engineering/token/json/2/device/new"

	// Generate random Device ID
	deviceID := fmt.Sprintf("rss2rm-%x", randomBytes(8))

	payload := map[string]string{
		"code":       code,
		"deviceDesc": "desktop-linux",
		"deviceID":   deviceID,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}

	// 1. Get Device Token
	resp, err := http.Post(newDeviceURL, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return "", "", fmt.Errorf("registration request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("registration failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Response is plain text string (the token)
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	deviceToken := strings.TrimSpace(buf.String())

	if deviceToken == "" {
		return "", "", fmt.Errorf("received empty device token")
	}

	// 2. Get User Token
	// We can use our refreshUserToken helper, but we need a transport context.
	tokens := model.AuthTokens{
		DeviceToken: deviceToken,
	}
	httpCtx := transport.CreateHttpClientCtx(tokens)

	userToken, err := refreshUserToken(&httpCtx)
	if err != nil {
		return "", "", fmt.Errorf("failed to get user token from device token: %w", err)
	}

	return deviceToken, userToken, nil
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func refreshUserToken(httpCtx *transport.HttpClientCtx) (string, error) {
	resp := transport.BodyString{}
	// DeviceBearer auth type uses the device token to get a user token
	err := httpCtx.Post(transport.DeviceBearer, config.NewUserDevice, nil, &resp)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// uploadInternal does the actual upload work without locking or retry logic.
func (c *Client) uploadInternal(filePath string, targetDirName string) (string, error) {
	tree := c.apiCtx.Filetree()
	if tree == nil {
		return "", fmt.Errorf("filetree not available")
	}

	root := tree.Root()
	if root == nil {
		return "", fmt.Errorf("root node not found")
	}

	targetDirName = strings.TrimSpace(targetDirName)
	if targetDirName == "" {
		targetDirName = "RSS"
	}

	// Check if directory exists in root
	var dirID string

	dirNode, err := root.FindByName(targetDirName)
	if err == nil && dirNode != nil {
		if !dirNode.IsDirectory() {
			return "", fmt.Errorf("node %s exists but is not a directory", targetDirName)
		}
		dirID = dirNode.Id()
	} else {
		doc, err := c.apiCtx.CreateDir(root.Id(), targetDirName, true)
		if err != nil {
			return "", fmt.Errorf("failed to create directory %s: %w", targetDirName, err)
		}
		dirID = doc.ID
		c.apiCtx.Filetree().AddDocument(doc)
	}

	doc, err := c.apiCtx.UploadDocument(dirID, filePath, true, nil)
	if err != nil {
		return "", fmt.Errorf("failed to upload document: %w", err)
	}

	return doc.ID, nil
}

// Upload uploads a file to the target directory, with automatic token refresh on 401.
// Returns (documentID, refreshedToken, error).
func (c *Client) Upload(filePath string, targetDirName string) (string, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	docID, err := c.uploadInternal(filePath, targetDirName)
	if err == nil {
		return docID, "", nil
	}

	// Check if it's a 401 error
	if !errors.Is(err, transport.ErrUnauthorized) {
		return "", "", err
	}

	slog.Warn("got 401, refreshing token and retrying upload", "component", "uploader")

	newToken, refreshErr := c.refreshAndRetry()
	if refreshErr != nil {
		return "", "", fmt.Errorf("upload failed and token refresh failed: %w (original: %v)", refreshErr, err)
	}

	// Retry upload
	docID, retryErr := c.uploadInternal(filePath, targetDirName)
	if retryErr != nil {
		return "", newToken, fmt.Errorf("upload failed after token refresh: %w", retryErr)
	}

	return docID, newToken, nil
}

// Delete removes a document from the reMarkable cloud by its ID.
func (c *Client) Delete(documentID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	node := c.apiCtx.Filetree().NodeById(documentID)
	if node == nil {
		return fmt.Errorf("document not found: %s", documentID)
	}
	return c.apiCtx.DeleteEntry(node, false, true)
}
