package connector

import (
	"github.com/annetutil/gnetcli/pkg/cmd"
	dcreds "github.com/annetutil/gnetcli/pkg/credentials"
	"github.com/annetutil/gnetcli/pkg/device/cisco"
	"github.com/annetutil/gnetcli/pkg/streamer/ssh"
	"go.uber.org/zap"
	"golang.org/x/xerrors"
)

var (
	ErrDeviceUnavailable = "device unavailable"
)

type CiscoDevice struct {
	*device
}

func NewCiscoDevice(name string, login string, address string, password string) *CiscoDevice {
	cmds := cmd.NewCmdList([]string{"enable", "show run"})
	return &CiscoDevice{
		device: &device{
			name:     name,
			login:    login,
			address:  address,
			password: password,
			cmds:     cmds,
		},
	}
}

func (d *CiscoDevice) Connect(logger *zap.Logger) error {

	if err := d.Ping(); err != nil {
		logger.Error("device unavailable", zap.Error(err))
		return xerrors.Errorf("%s: %w", ErrDeviceUnavailable, err)
	}

	if err := d.Ping(); err != nil {
		logger.Error("device unavailable", zap.Error(err))
		return xerrors.Errorf("%s: %w", ErrDeviceUnavailable, err)
	}

	creds := dcreds.NewSimpleCredentials(
		dcreds.WithUsername(d.login),
		dcreds.WithPassword(dcreds.Secret(d.password)),
		dcreds.WithLogger(logger),
	)

	connector := ssh.NewStreamer(d.address, creds, ssh.WithLogger(logger))
	d.gnetDevice = cisco.NewDevice(connector)

	return nil
}
