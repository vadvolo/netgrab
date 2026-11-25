package local

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalStorage_SaveConfigToFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mockTime := time.Date(2023, 5, 15, 10, 30, 0, 0, time.UTC)
	mockTP := &mockTimeProvider{fixedTime: mockTime}
	const layout = "20060102-150405"

	invalidPath := filepath.Join(tmpDir, string([]byte{0}), "invalid_path")

	tests := []struct {
		name        string
		storage     LocalStorage
		config      []byte
		deviceName  string
		expectError bool
	}{
		{
			name:       "Successful config save",
			storage:    LocalStorage{Path: tmpDir, tp: mockTP, layout: layout},
			config:     []byte("test configuration content"),
			deviceName: "router1",
		},
		{
			name:       "Save with empty device name",
			storage:    LocalStorage{Path: tmpDir, tp: mockTP, layout: layout},
			config:     []byte("test configuration content"),
			deviceName: "",
		},
		{
			name:        "Invalid directory path",
			storage:     LocalStorage{Path: invalidPath, tp: mockTP, layout: layout},
			config:      []byte("test configuration content"),
			deviceName:  "router1",
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ts := mockTime.Format(layout)
			// SaveConfigToFile writes under a per-device subdirectory.
			expectedFilename := filepath.Join(tc.storage.Path, tc.deviceName,
				fmt.Sprintf("%s_config_%s.txt", tc.deviceName, ts))

			filename, err := tc.storage.SaveConfigToFile(tc.config, tc.deviceName)

			if tc.expectError {
				if err == nil {
					t.Errorf("Expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if filename != expectedFilename {
				t.Errorf("Expected filename %s, got %s", expectedFilename, filename)
			}
			if _, err := os.Stat(filename); os.IsNotExist(err) {
				t.Errorf("File %s was not created", filename)
			}
			content, err := os.ReadFile(filename)
			if err != nil {
				t.Fatalf("Failed to read created file: %v", err)
			}
			if string(content) != string(tc.config) {
				t.Errorf("File content mismatch. Expected: %s, Got: %s", tc.config, content)
			}
		})
	}
}

func TestLocalStorage_Put(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewLocalStorage(tmpDir, "")
	loc, err := s.Put(context.Background(), "parsed/router1/router1_20230101-000000.json", []byte("{}"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	want := filepath.Join(tmpDir, "parsed", "router1", "router1_20230101-000000.json")
	if loc != want {
		// Put returns an absolute path; compare resolved.
		if abs, _ := filepath.Abs(want); abs != loc {
			t.Errorf("locator = %s, want %s", loc, want)
		}
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("file not created: %v", err)
	}
}
