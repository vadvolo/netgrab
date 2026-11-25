package gitlab

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"go.uber.org/zap"
	"golang.org/x/xerrors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
)

type Storage struct {
	logger  *zap.Logger
	url     string
	project string
	branch  string
	token   string
}

func NewStorage(cfg *Config) *Storage {
	return &Storage{
		url:     cfg.URL,
		project: cfg.Project,
		branch:  cfg.Branch,
		token:   cfg.Token,
		logger:  cfg.Logger,
	}
}

func (s *Storage) SaveConfig(config []byte, deviceName string) (string, error) {

	filePath := fmt.Sprintf("%s.txt", deviceName)

	_, err := s.getFileInfo(filePath)
	if errors.Is(err, ErrNotFound) {
		err = s.createFile(filePath)
		if err != nil {
			return "", xerrors.Errorf("creating file: %w", err)
		}
	}
	if err != nil {
		return "", err
	}

	s.logger.Info("updating file", zap.String("filePath", filePath))

	err = s.updateFile(filePath, s.branch, fmt.Sprintf("update %s", filePath), config)
	if err != nil {
		log.Fatalf("Error updating file %s: %s", filePath, err)
	}

	return "File updated successfully!", nil
}

func (s *Storage) createFile(filePath string) error {

	s.logger.Info("creating file", zap.String("filePath", filePath))

	url := fmt.Sprintf(
		"%s/api/v4/projects/%s/repository/files/%s?ref=%s",
		s.url,
		s.project,
		filePath,
		s.branch,
	)

	requestBody := map[string]interface{}{
		"branch":         "main",
		"content":        "init",
		"commit_message": "Create new file via API",
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return xerrors.Errorf("marshaling: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return xerrors.Errorf("newRequest: %w", err)
	}

	req.Header.Set("Private-Token", s.token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return xerrors.Errorf("client.Do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	return nil
}

// FileInfo is the subset of a GitLab repository-file response netgrab needs.
type FileInfo struct {
	BlobID string `json:"blob_id"`
}

func (s *Storage) getFileInfo(filePath string) (*FileInfo, error) {
	// URL encode the file path
	encodedFilePath := strings.ReplaceAll(filePath, "/", "%2F")

	url := fmt.Sprintf(
		"%s/api/v4/projects/%s/repository/files/%s?ref=%s",
		s.url,
		s.project,
		encodedFilePath,
		s.branch,
	)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("PRIVATE-TOKEN", s.token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, ErrNotFound
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Simple string parsing for demonstration
	// In a real application, you should use json.Unmarshal
	blobIDStart := strings.Index(string(body), `"blob_id":"`) + len(`"blob_id":"`)
	blobIDEnd := strings.Index(string(body)[blobIDStart:], `"`) + blobIDStart
	blobID := string(body)[blobIDStart:blobIDEnd]

	return &FileInfo{BlobID: blobID}, nil
}

func (s *Storage) updateFile(filePath, branch, commitMessage string, config []byte) error {
	const maxRetries = 3
	const retryDelay = time.Second

	encodedFilePath := strings.ReplaceAll(filePath, "/", "%2F")
	url := fmt.Sprintf("%s/api/v4/projects/%s/repository/files/%s",
		s.url, s.project, encodedFilePath)

	fmt.Println("updating url", url)
	contentBase64 := base64.StdEncoding.EncodeToString(config)

	for attempt := 1; attempt <= maxRetries; attempt++ {
		requestBody := fmt.Sprintf(`{
			"branch": "%s",
			"commit_message": "%s",
			"content": "%s",
			"encoding": "base64"
		}`, branch, commitMessage, contentBase64)

		req, err := http.NewRequest("PUT", url, strings.NewReader(requestBody))
		if err != nil {
			return err
		}

		req.Header.Add("PRIVATE-TOKEN", s.token)
		req.Header.Add("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}

		if resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return nil
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 400 && strings.Contains(string(body), "Could not update refs") {
			if attempt < maxRetries {
				s.logger.Warn("GitLab ref update conflict, retrying",
					zap.Int("attempt", attempt),
					zap.String("file", filePath))
				time.Sleep(retryDelay * time.Duration(attempt))
				continue
			}
		}

		return fmt.Errorf("failed to update file, status code: %d, response: %s",
			resp.StatusCode, string(body))
	}

	return fmt.Errorf("max retries exceeded for file %s", filePath)
}
