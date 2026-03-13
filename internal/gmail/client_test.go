package gmail

import (
	"bytes"
	"strings"
	"testing"
)

func TestBuildMIMEMessage(t *testing.T) {
	to := "test@example.com"
	subject := "Test Subject"
	body := "This is the email body"
	filename := "test.pdf"
	mimeType := "application/pdf"
	attachmentData := []byte("fake pdf content")

	message, err := buildMIMEMessage(to, subject, body, filename, mimeType, attachmentData)
	if err != nil {
		t.Fatalf("buildMIMEMessage() error = %v", err)
	}

	messageStr := string(message)

	// Check headers
	if !strings.Contains(messageStr, "MIME-Version: 1.0") {
		t.Error("Message missing MIME-Version header")
	}
	if !strings.Contains(messageStr, "To: test@example.com") {
		t.Error("Message missing To header")
	}
	if !strings.Contains(messageStr, "Subject: Test Subject") {
		t.Error("Message missing Subject header")
	}
	if !strings.Contains(messageStr, "Content-Type: multipart/mixed") {
		t.Error("Message missing multipart/mixed Content-Type")
	}

	// Check body content
	if !strings.Contains(messageStr, "Content-Type: text/plain") {
		t.Error("Message missing text/plain part")
	}
	if !strings.Contains(messageStr, body) {
		t.Error("Message missing body text")
	}

	// Check attachment headers
	if !strings.Contains(messageStr, "Content-Type: application/pdf") {
		t.Error("Message missing attachment Content-Type")
	}
	if !strings.Contains(messageStr, "Content-Disposition: attachment") {
		t.Error("Message missing Content-Disposition")
	}
	if !strings.Contains(messageStr, `filename="test.pdf"`) {
		t.Error("Message missing attachment filename")
	}
	if !strings.Contains(messageStr, "Content-Transfer-Encoding: base64") {
		t.Error("Message missing base64 encoding header")
	}
}

func TestBuildMIMEMessage_SpecialCharacters(t *testing.T) {
	// Test with special characters in subject and body
	to := "user@example.com"
	subject := "Test: Special & Characters"
	body := "Body with special chars: <>&\""
	filename := "file with spaces.pdf"
	mimeType := "application/pdf"
	attachmentData := []byte("content")

	message, err := buildMIMEMessage(to, subject, body, filename, mimeType, attachmentData)
	if err != nil {
		t.Fatalf("buildMIMEMessage() error = %v", err)
	}

	messageStr := string(message)

	if !strings.Contains(messageStr, subject) {
		t.Error("Subject not properly included")
	}
	if !strings.Contains(messageStr, body) {
		t.Error("Body not properly included")
	}
}

func TestLineWrapper(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		lineLen  int
		expected string
	}{
		{
			name:     "short input no wrapping needed",
			input:    "abc",
			lineLen:  76,
			expected: "abc",
		},
		{
			name:     "exact line length",
			input:    "abcdef",
			lineLen:  6,
			expected: "abcdef",
		},
		{
			name:     "needs one wrap",
			input:    "abcdefgh",
			lineLen:  4,
			expected: "abcd\r\nefgh",
		},
		{
			name:     "needs multiple wraps",
			input:    "abcdefghijkl",
			lineLen:  4,
			expected: "abcd\r\nefgh\r\nijkl",
		},
		{
			name:     "single char line length",
			input:    "abc",
			lineLen:  1,
			expected: "a\r\nb\r\nc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			lw := &lineWrapper{w: &buf, lineLen: tt.lineLen}

			n, err := lw.Write([]byte(tt.input))
			if err != nil {
				t.Fatalf("Write() error = %v", err)
			}
			if n != len(tt.input) {
				t.Errorf("Write() returned %d, want %d", n, len(tt.input))
			}

			got := buf.String()
			if got != tt.expected {
				t.Errorf("lineWrapper output = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestLineWrapper_MultipleWrites(t *testing.T) {
	var buf bytes.Buffer
	lw := &lineWrapper{w: &buf, lineLen: 4}

	// Write in chunks
	lw.Write([]byte("ab"))
	lw.Write([]byte("cd"))
	lw.Write([]byte("ef"))

	expected := "abcd\r\nef"
	got := buf.String()
	if got != expected {
		t.Errorf("lineWrapper output = %q, want %q", got, expected)
	}
}

func TestLineWrapper_EmptyInput(t *testing.T) {
	var buf bytes.Buffer
	lw := &lineWrapper{w: &buf, lineLen: 76}

	n, err := lw.Write([]byte{})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != 0 {
		t.Errorf("Write() returned %d, want 0", n)
	}
	if buf.Len() != 0 {
		t.Errorf("buffer not empty: %q", buf.String())
	}
}
