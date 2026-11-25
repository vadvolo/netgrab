// Package annetoil is a client for annet-oil's HTTP "execute" API.
//
// This is an alternative collection path to talking to the gnetcli server
// directly: annet-oil already owns the Orion inventory *and* every device's
// credentials, so netgrab can POST just a host + command and let annet-oil
// resolve the login/password, pick the right vendor profile, and run the
// session. No device secrets ever touch netgrab.
//
// Endpoint: POST {BaseURL}/execute/  (BaseURL already includes /api/v0)
// Auth:     Authorization: Bearer <token>
package annetoil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Stable error codes annet-oil returns in ExecResult.ErrorCode (mirrors the
// server's classification).
const (
	ErrCodeReadTimeout     = "READ_TIMEOUT"
	ErrCodeCmdTimeout      = "CMD_TIMEOUT"
	ErrCodeEOF             = "EOF"
	ErrCodeUnknownDevice   = "UNKNOWN_DEVICE"
	ErrCodeAuth            = "AUTH_ERROR"
	ErrCodeConnect         = "CONNECT_ERROR"
	ErrCodeCommandRejected = "COMMAND_REJECTED"
	ErrCodeSessionLimit    = "SESSION_LIMIT"
	ErrCodeUnknown         = "UNKNOWN"
)

// Config describes how to reach the annet-oil API.
type Config struct {
	// BaseURL is the API root including the version prefix, e.g.
	// "http://annet-oil.example.net:8181/api/v0".
	BaseURL string
	// Token is the bearer token (Authorization: Bearer <token>).
	Token string
	// RequestTimeout bounds a single HTTP call; 0 defaults to 5 minutes
	// (collecting a full config can be slow on big devices).
	RequestTimeout time.Duration
}

// Client is an annet-oil execute client. Safe for concurrent use (net/http
// Client is).
type Client struct {
	base  string
	token string
	http  *http.Client
}

// execRequest is the POST body of /execute/.
type execRequest struct {
	Host     string  `json:"host"`
	Command  string  `json:"command"`
	Vendor   string  `json:"vendor,omitempty"`
	TimeoutS float64 `json:"timeout_s,omitempty"`
}

// execResponse is the /execute/ reply.
type execResponse struct {
	Output    string `json:"output"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	Status    int32  `json:"status"`
}

// ExecResult is the outcome of one command on one device.
type ExecResult struct {
	Output    string
	Error     string
	ErrorCode string
	Status    int32
}

// Failed reports whether the command did not complete cleanly. annet-oil can
// signal a device-side rejection via ErrorCode alone (e.g. COMMAND_REJECTED
// with Status 0 and an empty Error, when the device printed its own CLI error
// banner as output), so a non-empty ErrorCode counts as a failure too —
// otherwise that error banner would be stored as if it were a real config.
func (r *ExecResult) Failed() bool {
	return r == nil || r.Status != 0 || r.Error != "" || r.ErrorCode != ""
}

// New builds a client. BaseURL and Token are required.
func New(cfg Config) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("annetoil: BaseURL is required")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("annetoil: Token is required")
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	return &Client{
		base:  base,
		token: strings.TrimSpace(cfg.Token),
		http:  &http.Client{Timeout: timeout},
	}, nil
}

// Exec runs command on host through annet-oil. vendor is optional: it sets the
// gnetcli device profile for hosts annet-oil may not have in its inventory
// (pass the profile name, e.g. "cisco", "ros", "huawei"); leave empty to let
// annet-oil decide from its own inventory.
//
// A non-nil error means the API call itself failed (network, auth, 5xx).
// Device-level failures come back in *ExecResult with a non-zero Status and an
// ErrorCode, mirroring the gnetcli client's contract.
func (c *Client) Exec(ctx context.Context, host, command, vendor string, timeoutSec float64) (*ExecResult, error) {
	body, err := json.Marshal(execRequest{
		Host: host, Command: command, Vendor: vendor, TimeoutS: timeoutSec,
	})
	if err != nil {
		return nil, fmt.Errorf("annetoil: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/execute/", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("annetoil: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("annetoil: POST %s/execute/: %w", c.base, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))

	// 429 is the concurrent-session cap; surface it as a device-level result so
	// the caller can back off without treating it as a transport failure.
	if resp.StatusCode == http.StatusTooManyRequests {
		return &ExecResult{Error: "session limit", ErrorCode: ErrCodeSessionLimit, Status: 1}, nil
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("annetoil: auth failed (HTTP %d): check token", resp.StatusCode)
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("annetoil: server error HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	var er execResponse
	if err := json.Unmarshal(raw, &er); err != nil {
		return nil, fmt.Errorf("annetoil: decode response (HTTP %d): %w", resp.StatusCode, err)
	}
	return &ExecResult{
		Output:    er.Output,
		Error:     er.Error,
		ErrorCode: er.ErrorCode,
		Status:    er.Status,
	}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
