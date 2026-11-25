package connector

import (
	"context"

	"go.uber.org/zap"
	"golang.org/x/xerrors"

	"netgrab/internal/gnetcli"
)

// OrionDevice is a device reachable through the remote gnetcli server rather
// than over a direct SSH streamer. It covers every Orion vendor/model: the
// concrete CLI grammar and config command come from its Profile, and the remote
// gnetcli server does the actual SSH.
//
// It satisfies the Device interface, so it drops into the same collection loop
// as the direct-SSH connectors.
type OrionDevice struct {
	client  *gnetcli.Client
	profile Profile

	name     string
	address  string
	login    string
	password string
	port     int
	vendor   string
	platform string
}

// NewOrionDevice builds a server-backed device. vendor/platform select the
// collection Profile (command + gnetcli device grammar) via ResolveProfile.
func NewOrionDevice(client *gnetcli.Client, name, address string, port int, login, password, vendor, platform string) *OrionDevice {
	profile, _ := ResolveProfile(vendor, platform)
	return &OrionDevice{
		client:   client,
		profile:  profile,
		name:     name,
		address:  address,
		login:    login,
		password: password,
		port:     port,
		vendor:   vendor,
		platform: platform,
	}
}

func (d *OrionDevice) GetName() string {
	return d.name
}

// Profile exposes the resolved collection profile (for logging / skip
// decisions before a collection run).
func (d *OrionDevice) Profile() Profile {
	return d.profile
}

// Connect validates that this device class has a known, collectable profile.
// There is no per-device socket to open: the gRPC connection to the gnetcli
// server is shared and established once by the caller.
func (d *OrionDevice) Connect(logger *zap.Logger) error {
	if d.profile.GnetcliDevice == "" || !d.profile.Supported {
		logger.Warn("skipping device: no collectable profile",
			zap.String("device", d.name),
			zap.String("vendor", d.vendor),
			zap.String("platform", d.platform),
			zap.String("note", d.profile.Note),
		)
		return xerrors.Errorf("%s: unsupported vendor/platform %q/%q", ErrDeviceUnavailable, d.vendor, d.platform)
	}
	return nil
}

// GetConfig asks the gnetcli server to run the profile's config command on the
// device and returns the captured configuration bytes.
func (d *OrionDevice) GetConfig(ctx context.Context) ([]byte, error) {
	const op = "OrionDevice.GetConfig"

	if !d.profile.Supported {
		return nil, xerrors.Errorf("%s: unsupported vendor/platform %q/%q", op, d.vendor, d.platform)
	}

	res, err := d.client.Exec(ctx, gnetcli.Target{
		Host:     d.address,
		Vendor:   d.profile.GnetcliDevice,
		Login:    d.login,
		Password: d.password,
		Port:     d.port,
	}, d.profile.Command)
	if err != nil {
		return nil, xerrors.Errorf("%s: server unreachable for %s: %w", op, d.address, err)
	}
	if res.Failed() {
		return nil, xerrors.Errorf("%s: %s (%s): %s", op, d.name, res.ErrorCode, res.Error)
	}
	return []byte(res.Output), nil
}
