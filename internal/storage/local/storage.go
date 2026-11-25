package local

import (
	"context"
	"fmt"
	"golang.org/x/xerrors"
	"os"
	"path/filepath"
	"time"
)

type timeProvider interface {
	Now() time.Time
}

type realTimeProvider struct{}

func (r realTimeProvider) Now() time.Time {
	return time.Now()
}

type mockTimeProvider struct {
	fixedTime time.Time
}

func (m *mockTimeProvider) Now() time.Time {
	return m.fixedTime
}

type LocalStorage struct {
	Path   string
	tp     timeProvider
	layout string
}

func NewLocalStorage(path string, layout string) *LocalStorage {
	if layout == "" {
		layout = "20060102-150405"
	}

	return &LocalStorage{
		Path:   path,
		tp:     realTimeProvider{},
		layout: layout,
	}
}

func (s *LocalStorage) SaveConfigToFile(config []byte, deviceName string) (string, error) {
	deviceDir := fmt.Sprintf("%s/%s", s.Path, deviceName)
	if err := os.MkdirAll(deviceDir, 0755); err != nil {
		return "", xerrors.Errorf("failed to create directory %s: %w", s.Path, err)
	}

	timestamp := s.tp.Now().Format(s.layout)
	filename := filepath.Join(deviceDir, fmt.Sprintf("%s_config_%s.txt", deviceName, timestamp))

	if err := os.WriteFile(filename, config, 0644); err != nil {
		return "", xerrors.Errorf("failed to write config to file %s: %w", filename, err)
	}

	return filename, nil
}

// Put implements storage.Store: it writes data at a '/'-separated key relative
// to the storage root, creating parent directories as needed. The returned
// locator is the absolute filesystem path.
func (s *LocalStorage) Put(_ context.Context, key string, data []byte) (string, error) {
	full := filepath.Join(s.Path, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return "", xerrors.Errorf("local: mkdir for %s: %w", full, err)
	}
	if err := os.WriteFile(full, data, 0644); err != nil {
		return "", xerrors.Errorf("local: write %s: %w", full, err)
	}
	abs, err := filepath.Abs(full)
	if err != nil {
		return full, nil
	}
	return abs, nil
}
