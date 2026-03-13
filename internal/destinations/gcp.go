package destinations

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"rss2rm/internal/service"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

// GCPDestination uploads PDFs to a Google Cloud Storage bucket.
type GCPDestination struct {
	BucketName  string
	Credentials string // Path to JSON key file
}

// NewGCPDestination creates a [GCPDestination] from the given config map,
// which should contain "bucket" and "credentials" (path to JSON key file).
func NewGCPDestination(config map[string]string) *GCPDestination {
	return &GCPDestination{
		BucketName:  config["bucket"],
		Credentials: config["credentials"],
	}
}

func (d *GCPDestination) Init(ctx context.Context, config map[string]string) (map[string]string, error) {
	if config["bucket"] == "" {
		return nil, fmt.Errorf("missing bucket name")
	}
	if config["credentials"] == "" {
		return nil, fmt.Errorf("missing credentials file path")
	}
	// Validate credentials file exists
	if _, err := os.Stat(config["credentials"]); os.IsNotExist(err) {
		return nil, fmt.Errorf("credentials file not found: %s", config["credentials"])
	}

	return config, nil
}

func (d *GCPDestination) Upload(ctx context.Context, filePath string, targetPath string) (string, error) {
	// targetPath acts as a folder prefix in the bucket
	objectName := filepath.Join(targetPath, filepath.Base(filePath))

	client, err := storage.NewClient(ctx, option.WithCredentialsFile(d.Credentials))
	if err != nil {
		return "", fmt.Errorf("storage.NewClient: %w", err)
	}
	defer client.Close()

	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("os.Open: %w", err)
	}
	defer f.Close()

	ctx, cancel := context.WithTimeout(ctx, time.Second*60)
	defer cancel()

	wc := client.Bucket(d.BucketName).Object(objectName).NewWriter(ctx)
	if _, err = io.Copy(wc, f); err != nil {
		return "", fmt.Errorf("io.Copy: %w", err)
	}
	if err := wc.Close(); err != nil {
		return "", fmt.Errorf("Writer.Close: %w", err)
	}

	return fmt.Sprintf("gs://%s/%s", d.BucketName, objectName), nil
}

func (d *GCPDestination) Delete(ctx context.Context, remotePath string) error {
	return nil // GCP deletion not supported
}

func (d *GCPDestination) TestConnection(ctx context.Context) error {
	client, err := storage.NewClient(ctx, option.WithCredentialsFile(d.Credentials))
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Close()

	// Check bucket existence/access
	bucket := client.Bucket(d.BucketName)
	if _, err := bucket.Attrs(ctx); err != nil {
		return fmt.Errorf("failed to access bucket '%s': %w", d.BucketName, err)
	}
	return nil
}

func (d *GCPDestination) Type() string {
	return "gcp"
}

var _ service.Destination = &GCPDestination{}
