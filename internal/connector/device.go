package connector

import (
	"context"
	"fmt"
	"github.com/annetutil/gnetcli/pkg/cmd"
	"github.com/annetutil/gnetcli/pkg/device/genericcli"
	"go.uber.org/zap"
	"golang.org/x/xerrors"
	"os/exec"
	"runtime"
	"strings"
)

//go:generate mockgen -source=device.go -destination=./mock/mock_device.go -package=mock DeviceInterface
type Device interface {
	Connect(logger *zap.Logger) error
	GetConfig(ctx context.Context) ([]byte, error)
	GetName() string
}

type device struct {
	gnetDevice genericcli.GenericDevice

	name     string
	login    string
	address  string
	password string
	cmds     []cmd.Cmd
}

func (d *device) GetName() string {
	return d.name
}

func (d *device) GetConfig(ctx context.Context) ([]uint8, error) {
	const op = "SendCommands"

	err := d.gnetDevice.Connect(ctx)
	if err != nil {
		return nil, xerrors.Errorf("%s: connecting to device %s: %w", op, d.address, err)
	}
	defer d.gnetDevice.Close()

	res, err := d.gnetDevice.ExecuteBulk(d.cmds)
	if err != nil {
		return nil, xerrors.Errorf("%s: %w", op, err)
	}

	if res[0].Error() != nil {
		return nil, xerrors.Errorf("%s: %w", op, res[0].Error())
	}

	config := res[1].Output()

	return config, nil
}

func (d *device) Ping() error {
	var command *exec.Cmd

	if runtime.GOOS == "windows" {
		command = exec.Command("ping", "-n", "1", d.address)
	} else {
		command = exec.Command("ping", "-c", "1", d.address)
	}

	out, err := command.CombinedOutput()

	if err != nil {
		return fmt.Errorf("there was an error pinging the host: %w", err)
	}

	outStr := string(out)
	if strings.Contains(outStr, "Request timeout") || strings.Contains(outStr, "Destination Host Unreachable") || strings.Contains(outStr, "100% packet loss") {
		return fmt.Errorf("the host is not reachable")
	} else {
		return nil
	}
}
