# Supported devices

netgrab talks to devices through the `connector.Device` interface. Two vendor
connectors ship today, both over SSH via `gnetcli`.

## The Device interface

```go
type Device interface {
	Connect(logger *zap.Logger) error
	GetConfig(ctx context.Context) ([]byte, error)
	GetName() string
}
```

- **Connect** — pings the host, then prepares the SSH streamer + credentials.
- **GetConfig** — opens the session, runs the vendor command list, returns the
  config bytes.
- **GetName** — the identifier used for filenames / repo paths.

## Cisco

```go
device := connector.NewCiscoDevice(
	"csr1000v",   // name
	"admin",      // login
	"192.0.2.1",  // address
	"secret-pw",  // password
)
```

- Command list: `enable`, then `show run`.
- The output of `show run` (the second command) is returned as the config.
- `Connect` pings the host first and returns
  `device unavailable: ...` if it is unreachable.

## Huawei

```go
device := connector.NewHuaweiDevice(
	"core-sw-1",
	"admin",
	"192.0.2.6",
	"secret-pw",
)
```

- Command list: `dis cur` (display current configuration).

> ⚠️ **Known limitation.** In the current source, `NewHuaweiDevice` returns a
> `*CiscoDevice` and `HuaweiDevice.Connect` has a different signature than the
> interface requires. If you rely on Huawei, verify/patch
> `internal/connector/huawei.go` so it mirrors the Cisco connector (returns
> `*HuaweiDevice`, `Connect(logger) error`, and wires `huawei.NewDevice`).

## Adding a new vendor

`gnetcli` ships device profiles for many vendors (Juniper, Arista, Nokia, …).
To add one, mirror `internal/connector/cisco.go`:

```go
package connector

import (
	"github.com/annetutil/gnetcli/pkg/cmd"
	dcreds "github.com/annetutil/gnetcli/pkg/credentials"
	"github.com/annetutil/gnetcli/pkg/device/juniper" // pick the vendor package
	"github.com/annetutil/gnetcli/pkg/streamer/ssh"
	"go.uber.org/zap"
	"golang.org/x/xerrors"
)

type JuniperDevice struct{ *device }

func NewJuniperDevice(name, login, address, password string) *JuniperDevice {
	// The command whose output you want to capture must be the LAST in the list;
	// GetConfig returns res[1].Output(), i.e. the second command's output.
	cmds := cmd.NewCmdList([]string{"set cli screen-length 0", "show configuration"})
	return &JuniperDevice{device: &device{
		name: name, login: login, address: address, password: password, cmds: cmds,
	}}
}

func (d *JuniperDevice) Connect(logger *zap.Logger) error {
	if err := d.Ping(); err != nil {
		return xerrors.Errorf("%s: %w", ErrDeviceUnavailable, err)
	}
	creds := dcreds.NewSimpleCredentials(
		dcreds.WithUsername(d.login),
		dcreds.WithPassword(dcreds.Secret(d.password)),
		dcreds.WithLogger(logger),
	)
	connector := ssh.NewStreamer(d.address, creds, ssh.WithLogger(logger))
	d.gnetDevice = juniper.NewDevice(connector)
	return nil
}
```

> 📌 **`GetConfig` reads `res[1]`.** The generic `GetConfig` returns the output of
> the **second** command in the list. Structure your command list so the config
> dump is index 1 (e.g. a paging/enable command at index 0, the `show` at index 1).

## Connectivity check

Every connector inherits a `Ping()` helper that shells out to the OS `ping`
(`-c 1` on Unix, `-n 1` on Windows) and fails on timeout / unreachable / 100%
loss. Cisco's `Connect` uses it as a pre-flight check.
