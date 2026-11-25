# Collecting config → local files

This guide shows how to pull the running configuration from one or more network
devices and save it to the local filesystem.

## How it works

The `cmd/local-storage` collector:

1. Loads `.env` (if present) and a device inventory
   (`-inventory` / `DEVICES_INVENTORY`, default `./config/devices.example.yaml`
   — same format as the Orion inventory, with credentials from `${ENV}`).
2. Builds a list of `connector.Device` values from it via `connector.FromInventory`.
3. Builds a `LocalStorage` rooted at a directory you choose.
4. For each device, concurrently: `Connect` → `GetConfig` → `SaveConfigToFile`.

Each save produces `./<root>/<device>/<device>_config_<timestamp>.txt`.

## Minimal example

```go
package main

import (
	"context"
	"log"
	"time"

	"go.uber.org/zap"

	"netgrab/internal/connector"
	"netgrab/internal/storage/local"
)

func main() {
	logger, _ := zap.NewProduction()

	// Root directory + timestamp layout for filenames.
	// time.DateOnly => "2006-01-02"; use "20060102-150405" for per-second files.
	storage := local.NewLocalStorage("./tmp/local-storage", time.DateOnly)

	device := connector.NewCiscoDevice(
		"csr1000v",   // name — used for the folder & filename
		"admin",      // SSH login
		"192.0.2.1",  // address
		"secret-pw",  // SSH password
	)

	if err := device.Connect(logger); err != nil {
		log.Fatalf("connect: %v", err)
	}

	config, err := device.GetConfig(context.Background())
	if err != nil {
		log.Fatalf("get config: %v", err)
	}

	path, err := storage.SaveConfigToFile(config, device.GetName())
	if err != nil {
		log.Fatalf("save: %v", err)
	}

	log.Printf("saved config to %s", path)
}
```

## Collecting from many devices in parallel

The shipped `cmd/local-storage/main.go` loads its devices from the inventory
file and collects each in its own goroutine. The slice below shows the
equivalent devices constructed by hand, which is the same `[]connector.Device`
that `connector.FromInventory` produces:

```go
devices := []connector.Device{
	connector.NewCiscoDevice("csr1000v", "admin", "192.0.2.1", "example-pw"),
	connector.NewCiscoDevice("sw-3750", "admin", "192.0.2.2", "example-pw"),
	connector.NewCiscoDevice("server-sw-1", "admin", "192.0.2.4", "example-pw"),
}

var wg sync.WaitGroup
for _, device := range devices {
	wg.Add(1)
	go func(dev connector.Device) {
		defer wg.Done()

		if err := dev.Connect(logger); err != nil {
			log.Printf("%s: connect failed: %v", dev.GetName(), err)
			return
		}

		config, err := dev.GetConfig(context.Background())
		if err != nil {
			log.Printf("%s: get config failed: %v", dev.GetName(), err)
			return
		}

		path, err := storage.SaveConfigToFile(config, dev.GetName())
		if err != nil {
			log.Printf("%s: save failed: %v", dev.GetName(), err)
			return
		}
		log.Printf("%s -> %s", dev.GetName(), path)
	}(device)
}
wg.Wait()
```

> 💡 **Tip.** The shipped example calls `log.Fatal` on the first error, which
> kills the whole run. The snippet above logs per-device and keeps going — prefer
> that when scanning a fleet.

## Running it

```bash
go run ./cmd/local-storage
# or build once and re-run
go build -o netgrab-local ./cmd/local-storage
./netgrab-local
```

## Output layout

```
tmp/local-storage/
├── csr1000v/
│   └── csr1000v_config_2025-09-26.txt
├── sw-3750/
│   └── sw-3750_config_2025-09-26.txt
└── server-sw-1/
    └── server-sw-1_config_2025-09-26.txt
```

- **Directory per device** — created automatically (`0755`).
- **Filename** — `<name>_config_<timestamp>.txt`, timestamp from the layout you
  passed to `NewLocalStorage`. Same layout on the same day overwrites; use a
  finer layout (e.g. `20060102-150405`) to keep every run.

## Timestamp layouts

| Layout                | Example filename suffix        | Keeps history? |
|-----------------------|--------------------------------|----------------|
| `time.DateOnly`       | `_config_2025-09-26.txt`       | 1 per day      |
| `"20060102-150405"`   | `_config_20250926-143012.txt`  | every run      |
| `""` (empty)          | defaults to `20060102-150405`  | every run      |

See also: [Collecting config → GitLab](collect-gitlab.md).
