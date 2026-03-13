package destinations

import (
	"context"
	"fmt"
	"net/smtp"
	"path/filepath"
	"rss2rm/internal/service"
	"strconv"

	"github.com/jordan-wright/email"
)

// EmailDestination sends PDFs as email attachments via SMTP.
type EmailDestination struct {
	SMTPServer string
	SMTPPort   int
	Username   string
	Password   string
	ToEmail    string
	FromEmail  string
}

// NewEmailDestination creates an [EmailDestination] from the given config
// map, which should contain "server", "port", "username", "password",
// "to_email", and "from_email".
func NewEmailDestination(config map[string]string) *EmailDestination {
	port, _ := strconv.Atoi(config["port"])
	return &EmailDestination{
		SMTPServer: config["server"],
		SMTPPort:   port,
		Username:   config["username"],
		Password:   config["password"],
		ToEmail:    config["to_email"],
		FromEmail:  config["from_email"],
	}
}

func (d *EmailDestination) Init(ctx context.Context, config map[string]string) (map[string]string, error) {
	required := []string{"server", "port", "username", "password", "to_email", "from_email"}
	for _, field := range required {
		if config[field] == "" {
			return nil, fmt.Errorf("missing required field: %s", field)
		}
	}
	return config, nil

}

func (d *EmailDestination) Upload(ctx context.Context, filePath string, targetPath string) (string, error) {
	e := email.NewEmail()
	e.From = d.FromEmail
	e.To = []string{d.ToEmail}
	e.Subject = fmt.Sprintf("[%s] %s", targetPath, filepath.Base(filePath))
	e.Text = []byte("Please find the attached document.")

	if _, err := e.AttachFile(filePath); err != nil {
		return "", fmt.Errorf("failed to attach file: %w", err)
	}

	addr := fmt.Sprintf("%s:%d", d.SMTPServer, d.SMTPPort)
	auth := smtp.PlainAuth("", d.Username, d.Password, d.SMTPServer)

	if err := e.Send(addr, auth); err != nil {
		return "", fmt.Errorf("failed to send email: %w", err)
	}

	return "sent", nil
}

func (d *EmailDestination) Delete(ctx context.Context, remotePath string) error {
	return nil // Email does not support deletion
}

func (d *EmailDestination) TestConnection(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", d.SMTPServer, d.SMTPPort)
	// 1. Dial
	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer c.Quit()

	// 2. Auth (if username provided)
	if d.Username != "" {
		auth := smtp.PlainAuth("", d.Username, d.Password, d.SMTPServer)
		if ok, _ := c.Extension("AUTH"); ok {
			if err = c.Auth(auth); err != nil {
				return fmt.Errorf("authentication failed: %w", err)
			}
		}
	}
	return nil
}

func (d *EmailDestination) Type() string {
	return "email"
}

var _ service.Destination = &EmailDestination{}
