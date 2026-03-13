package gmail

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"

	"golang.org/x/oauth2"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// Client provides Gmail API operations.
type Client struct {
	service     *gmail.Service
	tokenSource oauth2.TokenSource
}

// NewClient creates a new Gmail client using the provided token source.
func NewClient(ctx context.Context, tokenSource oauth2.TokenSource) (*Client, error) {
	service, err := gmail.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, fmt.Errorf("failed to create Gmail service: %w", err)
	}

	return &Client{
		service:     service,
		tokenSource: tokenSource,
	}, nil
}

// SendWithAttachment sends an email with a file attachment.
func (c *Client) SendWithAttachment(ctx context.Context, toEmail, subject, body, attachmentPath string) error {
	// Read attachment file
	attachmentData, err := os.ReadFile(attachmentPath)
	if err != nil {
		return fmt.Errorf("failed to read attachment: %w", err)
	}

	filename := filepath.Base(attachmentPath)
	mimeType := mime.TypeByExtension(filepath.Ext(attachmentPath))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	// Build MIME message
	message, err := buildMIMEMessage(toEmail, subject, body, filename, mimeType, attachmentData)
	if err != nil {
		return fmt.Errorf("failed to build MIME message: %w", err)
	}

	// Base64url encode the message
	encodedMessage := base64.URLEncoding.EncodeToString(message)

	// Send via Gmail API
	msg := &gmail.Message{
		Raw: encodedMessage,
	}

	_, err = c.service.Users.Messages.Send("me", msg).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

// TestConnection verifies that the Gmail client can access the API.
func (c *Client) TestConnection(ctx context.Context) error {
	// Try to get the user's profile - this verifies the token works
	_, err := c.service.Users.GetProfile("me").Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("Gmail API connection test failed: %w", err)
	}
	return nil
}

// GetCurrentToken returns the current token from the token source.
// Useful for persisting refreshed tokens.
func (c *Client) GetCurrentToken() (*Token, error) {
	return TokenFromSource(c.tokenSource)
}

// buildMIMEMessage creates a MIME multipart message with an attachment.
func buildMIMEMessage(to, subject, body, filename, mimeType string, attachmentData []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Write headers
	headers := fmt.Sprintf("MIME-Version: 1.0\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"Content-Type: multipart/mixed; boundary=%s\r\n\r\n",
		to, subject, writer.Boundary())
	buf.Reset()
	buf.WriteString(headers)

	// Create multipart writer with the same boundary
	writer = multipart.NewWriter(&buf)
	writer.SetBoundary(writer.Boundary())

	// Recreate buffer with proper structure
	buf.Reset()
	writer = multipart.NewWriter(&buf)

	// Write text part
	textHeader := make(textproto.MIMEHeader)
	textHeader.Set("Content-Type", "text/plain; charset=UTF-8")
	textPart, err := writer.CreatePart(textHeader)
	if err != nil {
		return nil, err
	}
	io.WriteString(textPart, body)

	// Write attachment part
	attachHeader := make(textproto.MIMEHeader)
	attachHeader.Set("Content-Type", fmt.Sprintf("%s; name=%q", mimeType, filename))
	attachHeader.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	attachHeader.Set("Content-Transfer-Encoding", "base64")

	attachPart, err := writer.CreatePart(attachHeader)
	if err != nil {
		return nil, err
	}

	// Write base64-encoded attachment with line wrapping
	encoder := base64.NewEncoder(base64.StdEncoding, &lineWrapper{w: attachPart, lineLen: 76})
	_, err = encoder.Write(attachmentData)
	if err != nil {
		return nil, err
	}
	encoder.Close()

	writer.Close()

	// Build final message with headers
	var finalBuf bytes.Buffer
	finalBuf.WriteString(fmt.Sprintf("MIME-Version: 1.0\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"Content-Type: multipart/mixed; boundary=%s\r\n\r\n",
		to, subject, writer.Boundary()))
	finalBuf.Write(buf.Bytes())

	return finalBuf.Bytes(), nil
}

// lineWrapper wraps lines at a specified length (for base64 encoding).
type lineWrapper struct {
	w       io.Writer
	lineLen int
	col     int
}

func (lw *lineWrapper) Write(p []byte) (n int, err error) {
	for _, b := range p {
		if lw.col >= lw.lineLen {
			if _, err := lw.w.Write([]byte("\r\n")); err != nil {
				return n, err
			}
			lw.col = 0
		}
		if _, err := lw.w.Write([]byte{b}); err != nil {
			return n, err
		}
		lw.col++
		n++
	}
	return n, nil
}
