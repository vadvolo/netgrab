package connector

import (
	"github.com/annetutil/gnetcli/pkg/cmd"
	dcreds "github.com/annetutil/gnetcli/pkg/credentials"
	"github.com/annetutil/gnetcli/pkg/device/huawei"
	"github.com/annetutil/gnetcli/pkg/streamer/ssh"
	"go.uber.org/zap"
)

type HuaweiDevice struct {
	*device
}

func NewHuaweiDevice(name string, login string, address string, password string) *CiscoDevice {
	cmds := cmd.NewCmdList([]string{"dis cur"})
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

func (d *HuaweiDevice) Connect(logger *zap.Logger) {
	creds := dcreds.NewSimpleCredentials(
		dcreds.WithUsername(d.login),
		dcreds.WithPassword(dcreds.Secret(d.password)),
		dcreds.WithLogger(logger),
	)

	connector := ssh.NewStreamer(d.address, creds, ssh.WithLogger(logger))
	d.gnetDevice = huawei.NewDevice(connector)
}
