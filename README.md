# netgrab

**netgrab** collects running configuration from network devices, parses it into
a normalized vendor-neutral model, stores the result (local filesystem, S3, or a
GitLab repo), and exports it to CSV / JSON / YAML / TOML and Terraform for
NetBox. It is written in Go and talks to devices through
[gnetcli](https://github.com/annetutil/gnetcli), so vendor-specific prompts,
pagination, and privileged-mode transitions are handled for you.

```
   collect                 parse                  store                  export
┌────────────┐   bytes   ┌─────────────┐  pb/json ┌──────────────┐     ┌──────────────────────┐
│ device CLI │ ────────▶ │ DeviceConfig │ ───────▶ │ local + S3    │ ──▶ │ json·yaml·toml·csv·tf │
│ show run…  │           │ (protobuf)   │          │ (+ GitLab)    │     │ (NetBox Terraform)    │
└────────────┘           └─────────────┘          └──────────────┘     └──────────────────────┘
```

## Features

- **Multi-vendor** — Cisco IOS/XE/XR/NX-OS, Huawei, MikroTik RouterOS, Eltex,
  Ruijie, HP ProCurve, Zyxel, Ubiquiti AirOS, Cambium, and more, via gnetcli
  device profiles.
- **Two collection paths** — direct SSH, or through a remote gnetcli gRPC server
  (and annet-oil's execute API) for fleets the local host can't reach.
- **Normalized model** — one protobuf `DeviceConfig` (identity, interfaces,
  VLANs, routes, provenance) that every parser produces and every exporter
  consumes.
- **Pluggable storage** — local filesystem, any S3-compatible store (AWS S3,
  MinIO, …), or GitLab (config history = git history).
- **Exports** — CSV/JSON/YAML/TOML and Terraform for the NetBox provider.
- **Secrets stay out of the repo** — config via YAML + environment / `.env`.

## Quick start

```bash
# Build
go build ./...

# Collect from devices listed in an inventory → local files
cp config/devices.example.yaml config/devices.yaml   # edit for your devices
cp .env.example .env                                  # set DEVICE_USERNAME / DEVICE_PASSWORD
go run ./cmd/local-storage -inventory ./config/devices.yaml

# Inspect the result
ls ./tmp/local-storage/<device-name>/
```

Fleet collection through a gnetcli server, then export:

```bash
go run ./cmd/orion-collect -config ./config/orion.yaml -dry-run   # print the plan
go run ./cmd/orion-collect -config ./config/orion.yaml            # collect + parse + store
go run ./cmd/netgrab-export -config ./config/orion.yaml -format tf,json
```

## Configuration & secrets

Configuration is layered: `config/*.yaml` holds structure and non-secret
defaults; environment variables (loaded from `.env` at startup, see
`.env.example`) override everything and carry all secrets. Nothing sensitive is
committed — the repo ships `*.example.yaml` samples; real inventories, tokens
and `.env` are gitignored. See [docs/configuration.md](docs/configuration.md).

## Documentation

| Guide | What it covers |
|-------|----------------|
| [Architecture](ARCHITECTURE.md) | How the pieces fit together |
| [Collecting → local files](docs/collect-local.md) | Grab configs to disk |
| [Collecting → GitLab](docs/collect-gitlab.md) | Version configs in a GitLab repo |
| [Collecting Orion config](docs/collect-orion.md) | Fleet collection via the gnetcli server |
| [Parsing & exporting](docs/parse-export.md) | Parse into the model; export CSV/JSON/…/Terraform |
| [Integrating with netgrab](docs/integration.md) | Consume the stored device data |
| [Supported devices](docs/devices.md) | Connectors and adding new vendors |
| [Configuration reference](docs/configuration.md) | YAML files and environment variables |
| [Troubleshooting](docs/troubleshooting.md) | Common errors and fixes |

## Development

```bash
make build   # go build ./...
make test    # go test ./...
make proto   # regenerate Go from proto/netgrab/v1/device.proto (needs protoc)
make tidy    # go mod tidy
```
