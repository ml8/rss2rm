package destinations

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"rss2rm/internal/service"
)

// FileDestination writes PDFs to the local filesystem.
type FileDestination struct {
	RootPath string
}

// NewFileDestination creates a [FileDestination] from the given config
// map, which should contain a "path" key.
func NewFileDestination(config map[string]string) *FileDestination {
	return &FileDestination{
		RootPath: config["path"],
	}
}

func (d *FileDestination) Init(ctx context.Context, config map[string]string) (map[string]string, error) {
	path, ok := config["path"]
	if !ok || path == "" {
		return nil, fmt.Errorf("missing 'path' configuration")
	}

	// Check if directory exists or can be created
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(path, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory: %w", err)
		}
	} else if err != nil {
		return nil, err
	} else if !info.IsDir() {
		return nil, fmt.Errorf("path exists but is not a directory")
	}

	return config, nil
}

func (d *FileDestination) Upload(ctx context.Context, srcPath string, targetPath string) (string, error) {
	// targetPath here effectively acts as the subdirectory or filename prefix
	// If targetPath is just a name like "TechNews", we create a directory.

	fullDir := filepath.Join(d.RootPath, targetPath)
	if err := os.MkdirAll(fullDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create target dir: %w", err)
	}

	fileName := filepath.Base(srcPath)
	dstPath := filepath.Join(fullDir, fileName)

	src, err := os.Open(srcPath)
	if err != nil {
		return "", err
	}
	defer src.Close()

	dst, err := os.Create(dstPath)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", err
	}

	return dstPath, nil
}

func (d *FileDestination) Delete(ctx context.Context, remotePath string) error {
	return os.Remove(remotePath)
}

func (d *FileDestination) TestConnection(ctx context.Context) error {
	// For file system, check if RootPath exists and is writable
	info, err := os.Stat(d.RootPath)
	if err != nil {
		return fmt.Errorf("root path not accessible: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("root path is not a directory")
	}
	// Try to write a temp file
	tmpFile, err := os.CreateTemp(d.RootPath, ".write_test_*")
	if err != nil {
		return fmt.Errorf("root path not writable: %w", err)
	}
	tmpFile.Close()
	os.Remove(tmpFile.Name())
	return nil
}

func (d *FileDestination) Type() string {
	return "file"
}

var _ service.Destination = &FileDestination{}
