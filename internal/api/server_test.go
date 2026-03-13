package api

import (
	"testing"
	"time"

	"rss2rm/internal/db"
)

func TestParseSchedule(t *testing.T) {
	tests := []struct {
		input   string
		hour    int
		min     int
		wantErr bool
	}{
		{"07:00", 7, 0, false},
		{"23:59", 23, 59, false},
		{"00:00", 0, 0, false},
		{"9:30", 9, 30, false},
		{"invalid", 0, 0, true},
		{"25:00", 0, 0, true},
		{"12:60", 0, 0, true},
	}

	for _, tt := range tests {
		h, m, err := parseSchedule(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseSchedule(%q) expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSchedule(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if h != tt.hour || m != tt.min {
			t.Errorf("parseSchedule(%q) = %d:%d, want %d:%d", tt.input, h, m, tt.hour, tt.min)
		}
	}
}

func TestIsDigestDue(t *testing.T) {
	loc := time.Local

	// Today at 10:00 AM
	now := time.Date(2026, 2, 21, 10, 0, 0, 0, loc)

	tests := []struct {
		name     string
		digest   db.Digest
		now      time.Time
		expected bool
	}{
		{
			name: "never generated, past schedule time",
			digest: db.Digest{
				Schedule: "07:00",
				Active:   true,
				// LastGenerated is zero
			},
			now:      now,
			expected: true,
		},
		{
			name: "never generated, before schedule time",
			digest: db.Digest{
				Schedule: "11:00",
				Active:   true,
			},
			now:      now,
			expected: false,
		},
		{
			name: "generated yesterday, past schedule time today",
			digest: db.Digest{
				Schedule:      "07:00",
				Active:        true,
				LastGenerated: time.Date(2026, 2, 20, 7, 0, 0, 0, loc),
			},
			now:      now,
			expected: true,
		},
		{
			name: "generated today after schedule, not due again",
			digest: db.Digest{
				Schedule:      "07:00",
				Active:        true,
				LastGenerated: time.Date(2026, 2, 21, 7, 30, 0, 0, loc),
			},
			now:      now,
			expected: false,
		},
		{
			name: "empty schedule",
			digest: db.Digest{
				Schedule: "",
				Active:   true,
			},
			now:      now,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isDigestDue(tt.digest, tt.now)
			if got != tt.expected {
				t.Errorf("isDigestDue() = %v, want %v", got, tt.expected)
			}
		})
	}
}
