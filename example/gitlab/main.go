package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	// GitLab configuration — supplied via the environment / .env, never
	// hardcoded. See .env.example.
	gitlabHost := os.Getenv("GITLAB_URL")             // e.g. https://gitlab.example.com
	projectID := os.Getenv("GITLAB_PROJECT")          // URL-encoded "namespace/project" or numeric ID
	personalAccessToken := os.Getenv("GITLAB_TOKEN")  // glpat-xxxxxxxxxxxx
	filePath := "csr1000v_config.txt"
	branch := "main"
	commitMessage := "Update file.txt via API"

	// The new content you want to put in the file
	newContent := "This is the updated content for the file."

	// First, get the current file to obtain its blob ID
	fmt.Printf("%s\n", filePath)
	_, err := getFileInfo(gitlabHost, projectID, filePath, branch, personalAccessToken)
	if err != nil {
		log.Fatalf("Error getting file info: %v", err)
	}

	// Update the file
	err = updateFile(gitlabHost, projectID, filePath, branch, commitMessage,
		personalAccessToken, newContent)
	if err != nil {
		log.Fatalf("Error updating file: %v", err)
	}

	fmt.Println("File updated successfully!")
}

type FileInfo struct {
	BlobID string `json:"blob_id"`
}

func getFileInfo(gitlabHost, projectID, filePath, branch, token string) (*FileInfo, error) {
	encodedFilePath := strings.ReplaceAll(filePath, "/", "%2F")

	url := fmt.Sprintf("%s/api/v4/projects/%s/repository/files/%s?ref=%s",
		gitlabHost, projectID, encodedFilePath, branch)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("PRIVATE-TOKEN", token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get file info, status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	blobIDStart := strings.Index(string(body), `"blob_id":"`) + len(`"blob_id":"`)
	blobIDEnd := strings.Index(string(body)[blobIDStart:], `"`) + blobIDStart
	blobID := string(body)[blobIDStart:blobIDEnd]

	return &FileInfo{BlobID: blobID}, nil
}

func updateFile(gitlabHost, projectID, filePath, branch, commitMessage,
	token, content string) error {

	encodedFilePath := strings.ReplaceAll(filePath, "/", "%2F")

	url := fmt.Sprintf("%s/api/v4/projects/%s/repository/files/%s",
		gitlabHost, projectID, encodedFilePath)

	contentBase64 := base64.StdEncoding.EncodeToString([]byte(content))

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

	req.Header.Add("PRIVATE-TOKEN", token)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to update file, status code: %d, response: %s",
			resp.StatusCode, string(body))
	}

	return nil
}
