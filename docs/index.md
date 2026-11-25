# netgrab

**netgrab** collects running configuration from network devices over SSH and
stores it either on the local filesystem or in a GitLab repository. It is
written in Go and uses [gnetcli](https://github.com/annetutil/gnetcli) to talk
to devices, so vendor-specific prompt handling, pagination, and privileged-mode
transitions are handled for you.

## What it does

```
┌──────────┐   SSH    ┌────────────┐   config bytes   ┌──────────────┐
│ netgrab  │─────────▶│  device    │─────────────────▶│  storage     │
│ (Go app) │  gnetcli │ Cisco/HW   │  show run/dis cur│ local│gitlab │
└──────────┘          └────────────┘                  └──────────────┘
```

1. **Connect** — pings the device, then opens an SSH session with the given
   credentials.
2. **Collect** — runs the vendor's "show running config" command
   (`enable` + `show run` for Cisco, `dis cur` for Huawei).
3. **Store** — writes the captured bytes to a timestamped local file, or commits
   them to a GitLab project via the REST API.

## Documentation

| Guide | What it covers |
|-------|----------------|
| [Collecting config → local files](collect-local.md) | Grab configs and save them to disk |
| [Collecting Orion config via gnetcli server](collect-orion.md) | Collect all Orion vendors/models through the remote gnetcli server |
| [Parsing & exporting (CSV/JSON/YAML/TOML/Terraform)](parse-export.md) | Parse configs into the model, store locally + in S3, export for NetBox |
| [Integrating with netgrab](integration.md) | How another service consumes netgrab's device data (S3/pb/JSON) |
| [Collecting config → GitLab](collect-gitlab.md) | Grab configs and version them in a GitLab repo |
| [Supported devices](devices.md) | Cisco & Huawei connectors, adding new vendors |
| [Configuration reference](configuration.md) | YAML files and environment variables |
| [Troubleshooting](troubleshooting.md) | Common errors and how to fix them |

## Quick start

```bash
# 1. Build the binaries
go build ./cmd/local-storage
go build ./cmd/gitlab-storage

# 2. Collect configs to local files (edit the device list in the source first)
go run ./cmd/local-storage

# 3. Inspect the result
ls ./tmp/local-storage/<device-name>/
```

By default the local collector writes one folder per device under
`./tmp/local-storage`, with a timestamped file per run:

```
tmp/local-storage/
└── csr1000v/
    └── csr1000v_config_2025-09-26.txt
```

## Concepts

- **Device** — anything implementing the `connector.Device` interface
  (`Connect`, `GetConfig`, `GetName`). Cisco and Huawei constructors are
  provided.
- **Storage** — a sink for collected config bytes. Two implementations exist:
  `local` (filesystem) and `gitlab` (REST API).
- **Collector** — the `main` in `cmd/local-storage` or `cmd/gitlab-storage`
  wires a list of devices to a storage backend.

> ⚠️ **Credentials.** The example collectors hard-code device credentials in
> `main.go`. For anything beyond a lab, move secrets into environment variables
> or a secret manager before running. See [Configuration](configuration.md).
