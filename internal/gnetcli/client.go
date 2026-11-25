// Package gnetcli is a thin client for a remote gnetcli gRPC server.
//
// netgrab does not talk to network devices directly here: it asks a
// gnetcli_server (listening on 0.0.0.0:50051 with basic auth) to open the SSH
// session on its behalf and run a command. That server sits inside the Orion
// network and can reach equipment the netgrab host cannot. See
// docs/collect-orion.md.
package gnetcli

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	proto "github.com/annetutil/gnetcli/pkg/server/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Stable, machine-readable classifications of a device-level failure, surfaced
// on ExecResult.ErrorCode so callers do not have to regex the message.
const (
	ErrCodeReadTimeout   = "READ_TIMEOUT"
	ErrCodeCmdTimeout    = "CMD_TIMEOUT"
	ErrCodeEOF           = "EOF"
	ErrCodeUnknownDevice = "UNKNOWN_DEVICE"
	ErrCodeUnknown       = "UNKNOWN"
)

// Config describes how to reach the gnetcli server. Host/Port point at the
// server itself; Login/Password (or a pre-encoded AuthToken) authenticate the
// client to that server's basic auth.
type Config struct {
	Host      string
	Port      int
	AuthToken string // base64("login:password"); takes priority over Login/Password.
	Login     string
	Password  string
	TLS       bool

	// Per-command timeout defaults in seconds; 0 means inherit the gnetcli
	// server's own default.
	ReadTimeoutSec float64
	CmdTimeoutSec  float64
}

// Client is a connected gnetcli gRPC client. It is safe for concurrent use:
// the underlying grpc.ClientConn multiplexes concurrent RPCs, so a single
// Client backs a whole fan-out of device collections.
type Client struct {
	conn      *grpc.ClientConn
	rpc       proto.GnetcliClient
	authToken string
	login     string
	pass      string

	readTimeoutSec float64
	cmdTimeoutSec  float64
}

// ExecResult is the outcome of one command on one device.
type ExecResult struct {
	Output string
	Error  string
	// ErrorCode classifies Error (one of the ErrCode* constants); empty on
	// success.
	ErrorCode string
	Status    int32
}

// Failed reports whether the command did not complete cleanly.
func (r *ExecResult) Failed() bool {
	return r == nil || r.Status != 0 || r.Error != ""
}

// New dials the gnetcli server described by cfg. The dial is lazy (grpc.NewClient
// does not block), so a wrong address surfaces on the first Exec, not here.
func New(cfg Config) (*Client, error) {
	if cfg.TLS {
		return nil, fmt.Errorf("gnetcli: TLS transport is not yet supported")
	}
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("gnetcli: connecting to %s: %w", addr, err)
	}
	return &Client{
		conn:           conn,
		rpc:            proto.NewGnetcliClient(conn),
		authToken:      cfg.AuthToken,
		login:          cfg.Login,
		pass:           cfg.Password,
		readTimeoutSec: cfg.ReadTimeoutSec,
		cmdTimeoutSec:  cfg.CmdTimeoutSec,
	}, nil
}

// Close releases the gRPC connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// Target identifies one device the server should connect to.
type Target struct {
	Host     string // IP or hostname the gnetcli server will SSH to.
	Vendor   string // gnetcli device profile name (see connector.Profile).
	Login    string
	Password string
	Port     int
}

// Exec runs cmd on the device described by t, asking the gnetcli server to open
// the session with the device profile and credentials in t.
//
// Device-level failures (auth, unknown profile, read/cmd timeout) are NOT
// returned as a Go error: they come back in *ExecResult with a non-zero Status,
// an ErrorCode, and any partial output the device streamed before the failure.
// A non-nil error means the gnetcli server itself was unreachable.
func (c *Client) Exec(ctx context.Context, t Target, cmd string) (*ExecResult, error) {
	authCtx := metadata.AppendToOutgoingContext(ctx, "authorization", c.authHeader())

	hostParams := &proto.HostParams{
		Device: t.Vendor,
		Credentials: &proto.Credentials{
			Login:    t.Login,
			Password: t.Password,
		},
	}
	if t.Port > 0 && t.Port != 22 {
		hostParams.Port = int32(t.Port)
	}

	res, err := c.rpc.Exec(authCtx, &proto.CMD{
		Host:         t.Host,
		Cmd:          cmd,
		HostParams:   hostParams,
		StringResult: true,
		ReadTimeout:  c.readTimeoutSec,
		CmdTimeout:   c.cmdTimeoutSec,
	})
	if err != nil {
		// Transport failure reaching the server: nothing to salvage.
		if !isDeviceExecError(err) {
			return nil, fmt.Errorf("gnetcli exec on %s: %w", t.Host, err)
		}
		// Device-level failure: recover the code and any partial output.
		code, partial := classifyExecError(err)
		return &ExecResult{
			Output:    partial,
			Error:     err.Error(),
			ErrorCode: code,
			Status:    1,
		}, nil
	}

	out := res.OutStr
	if out == "" {
		out = string(res.Out)
	}
	errStr := res.ErrorStr
	if errStr == "" {
		errStr = string(res.Error)
	}
	return &ExecResult{Output: out, Error: errStr, Status: res.Status}, nil
}

func (c *Client) authHeader() string {
	if c.authToken != "" {
		return "Basic " + c.authToken
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(c.login+":"+c.pass))
}

// isDeviceExecError reports whether err is a command-level failure from the
// device rather than a transport failure reaching the server. gnetcli returns
// device-level failures as codes.Internal; connection problems surface as
// Unavailable/DeadlineExceeded.
func isDeviceExecError(err error) bool {
	return status.Code(err) == codes.Internal
}

// classifyExecError maps a gnetcli gRPC error to a stable code and, when the
// device streamed output before failing, the partial output embedded in the
// message (gnetcli formats it as: last seen: %q).
func classifyExecError(err error) (code, partial string) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "read timeout error"):
		code = ErrCodeReadTimeout
	case strings.Contains(msg, "cmd timeout error"):
		code = ErrCodeCmdTimeout
	case strings.Contains(msg, "eof error"):
		code = ErrCodeEOF
	case strings.Contains(msg, "unknown device"):
		code = ErrCodeUnknownDevice
	default:
		code = ErrCodeUnknown
	}
	if i := strings.Index(msg, "last seen: "); i >= 0 {
		if s, uerr := strconv.Unquote(msg[i+len("last seen: "):]); uerr == nil {
			partial = s
		}
	}
	return code, partial
}
