package destinations

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileDestination_Init(t *testing.T) {
	// Create a temp dir for testing
	tmpDir, err := os.MkdirTemp("", "rss2rm-test-root")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name    string
		config  map[string]string
		wantErr bool
	}{
		{
			name:    "Valid config",
			config:  map[string]string{"path": tmpDir},
			wantErr: false,
		},
		{
			name:    "Missing path",
			config:  map[string]string{},
			wantErr: true,
		},
		{
			name:    "Path is empty",
			config:  map[string]string{"path": ""},
			wantErr: true,
		},
		{
			name:    "Path creates new directory",
			config:  map[string]string{"path": filepath.Join(tmpDir, "new-subdir")},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &FileDestination{}
			_, err := d.Init(context.Background(), tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("Init() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFileDestination_Upload(t *testing.T) {
	// Create a temp dir for the destination root
	destRoot, err := os.MkdirTemp("", "rss2rm-dest-root")
	if err != nil {
		t.Fatalf("Failed to create temp dest dir: %v", err)
	}
	defer os.RemoveAll(destRoot)

	// Create a dummy source file
	srcFile, err := os.CreateTemp("", "rss2rm-src-file-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp source file: %v", err)
	}
	content := []byte("Hello, World!")
	if _, err := srcFile.Write(content); err != nil {
		t.Fatalf("Failed to write to src file: %v", err)
	}
	srcFile.Close()
	defer os.Remove(srcFile.Name())

	d := NewFileDestination(map[string]string{"path": destRoot})

	// Test Upload
	targetSubDir := "MyFeed"
	uploadedPath, err := d.Upload(context.Background(), srcFile.Name(), targetSubDir)
	if err != nil {
		t.Fatalf("Upload() failed: %v", err)
	}

	// Verify file exists at destination
	expectedPath := filepath.Join(destRoot, targetSubDir, filepath.Base(srcFile.Name()))
	if uploadedPath != expectedPath {
		t.Errorf("Upload() returned path %s, expected %s", uploadedPath, expectedPath)
	}

	readContent, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("Failed to read uploaded file: %v", err)
	}

	if string(readContent) != string(content) {
		t.Errorf("Uploaded content mismatch. Got %s, want %s", readContent, content)
	}
}
