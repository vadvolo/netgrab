# netgrab architecture

netgrab is a Go pipeline that **collects** running configuration from network
devices, **parses** it into a normalized vendor-neutral model, **stores** the
artifacts (locally and/or in S3), and **exports** them into downstream formats
(CSV/JSON/YAML/TOML and Terraform for NetBox).

It talks to devices through [gnetcli](https://github.com/annetutil/gnetcli), so
vendor-specific prompt handling, pagination, and privileged-mode transitions are
handled for us.

```
   collect                 parse                  store                  export
┌────────────┐   bytes   ┌─────────────┐  pb/json ┌──────────────┐     ┌──────────────────────┐
│ device CLI │ ────────▶ │ DeviceConfig │ ───────▶ │ local + S3    │ ──▶ │ json·yaml·toml·csv·tf │
│ show run…  │           │ (protobuf)   │          │ (+ GitLab)    │     │ (NetBox Terraform)    │
└────────────┘           └─────────────┘          └──────────────┘     └──────────────────────┘
```

## Two collection paths

Everything that collects config implements one small interface
(`internal/connector.Device`):

```go
type Device interface {
    Connect(logger *zap.Logger) error
    GetConfig(ctx context.Context) ([]byte, error)
    GetName() string
}
```

There are two concrete ways to reach a device:

1. **Direct SSH** — `CiscoDevice` / `HuaweiDevice` open an SSH session straight
   from the host running netgrab (via gnetcli's SSH streamer). Used by the
   `local-storage` and `gitlab-storage` collectors.

2. **Remote gnetcli server** — `OrionDevice` does *not* dial the device itself.
   It asks a remote **gnetcli gRPC server** (which sits inside the device
   network) to open the SSH session and run the command on its behalf. Used by
   `orion-collect`, which can reach an entire multi-vendor fleet the local host
   cannot. A second variant routes through **annet-oil's HTTP execute API**,
   which owns the inventory and credentials, so netgrab sends only `host +
   command`.

```
Direct:   netgrab ──SSH──▶ device

Orion:    netgrab ──gRPC(50051)──▶ gnetcli_server ──SSH──▶ device
          netgrab ──HTTP execute──▶ annet-oil API ──gRPC──▶ gnetcli_server ──SSH──▶ device
```

Because all three satisfy `Device`, they drop into the same collection loop.

## Package map

| Package | Responsibility |
|---------|----------------|
| `cmd/local-storage` | Direct-SSH collector → local filesystem. Loads devices from an inventory. |
| `cmd/gitlab-storage` | Direct-SSH collector → commits each config to a GitLab repo. |
| `cmd/orion-collect` | Fleet collector via the gnetcli server (or annet-oil). Collect → parse → store, fanned out across a worker pool. |
| `cmd/netgrab-export` | Reads stored parsed models (or raw dumps) and writes the export formats. |
| `cmd/netgrab-backfill` | Uploads existing local artifacts to S3 under the same keys. |
| `internal/connector` | The `Device` interface + Cisco/Huawei/Orion connectors, and the vendor/model → gnetcli-profile registry (`profile.go`). |
| `internal/gnetcli` | Thin gRPC client for the remote gnetcli server (`Exec` on a target). |
| `internal/annetoil` | HTTP client for annet-oil's execute API (the credential-less collection path). |
| `internal/inventory` | Loads the device inventory (devices + named credential groups, `${ENV}`-expanded). |
| `internal/parser` | Per-vendor parsers that turn a raw CLI dump into a `DeviceConfig`. Pure and deterministic. |
| `internal/model/netgrabv1` | Generated Go for the canonical protobuf model. |
| `internal/storage` | The `Store` interface, key layout, the local/S3 backends, and the `Multi` fan-out. |
| `internal/storage/gitlab` | GitLab REST backend (create/update file with retry). |
| `internal/exporter` | Renders a `DeviceConfigSet` into json/yaml/toml/csv/tf. |
| `internal/config` | Config structs + loading (cleanenv: YAML + env overrides). |
| `proto/netgrab/v1` | The canonical model `.proto` (source of truth). |

## The canonical model

`proto/netgrab/v1/device.proto` defines `DeviceConfig` — the common denominator
across every vendor:

- **identity** — hostname, vendor, platform, model, OS version, serial, mgmt IP;
- **provenance** — source command, raw-config SHA-256, size, collection time,
  which parser produced it;
- **facts** — interfaces (desc, admin state, MTU, MAC, IPs, access/trunk mode,
  VLANs), VLAN definitions, static routes;
- **warnings** — anything a parser could not fully understand.

This is netgrab's *output* model. It is independent of gnetcli's wire protocol:
gnetcli has no device-model enum — a device type is just a free-form string
(`HostParams.device`) resolved against a Go map of built-in profiles on the
server. So there is nothing to "sync" from gnetcli into this proto; the two
models serve different layers.

Regenerate the Go after editing the `.proto` with `make proto`.

## Profile resolution

`internal/connector/profile.go` maps an inventory device to (a) the gnetcli
device-profile name the server should load and (b) the command that dumps the
full config. Resolution prefers the most specific `platform`, then falls back to
`vendor`, then skips unknown/unsupported classes (UPS/PDU, BMCs, optical
transport, OS hosts) rather than guessing.

The gnetcli server ships 10 built-in profiles (`cisco, nxos, huawei, h3c,
juniper, arista, bcomos, ros, pc, netconf`); IOS-like vendors (Ruijie, ProCurve,
Zyxel, TP-Link, DCN, …) reuse `cisco`, and raw-shell CPE (AirOS, Cambium, Linux)
reuse `pc`. `eltex` is **not** a built-in — it must be added to the server's
dev-conf, otherwise the server returns `unknown device`.

## Storage layout

`internal/storage` exposes a tiny `Store` (`Put(key, data) → locator`) and a
`Multi` that fans each write to several backends (local **and** S3) at once.
Keys are stable and timestamped:

```
configs/<device>/<device>_<ts>.txt      raw CLI dump
parsed/<device>/<device>_<ts>.pb        protobuf model
parsed/<device>/<device>_<ts>.json      protojson (camelCase)
devices/<hostname>_<ip>.json            latest per-device model (stable key, overwritten)
exports/<ts>/devices.{json,yaml,toml,csv}
exports/<ts>/netbox.tf
```

Consumers read these artifacts directly (see [docs/integration.md](docs/integration.md));
netgrab is a pipeline, not an online API. Change detection is a SHA-256 compare
on the raw config.

## Configuration & secrets

Config is layered: a YAML file (`config/*.yaml`) provides structure and
non-secret defaults; environment variables (loaded from `.env` at startup via
`godotenv`, then overlaid by cleanenv) override everything and carry all
secrets. **No credential, token, or real endpoint is committed** — the repo
ships `*.example.yaml` samples and `.env.example`, while the real
`inventory.orion.yaml`, `devices.yaml`, and `.env` are gitignored.

## Extending netgrab

- **New direct-SSH vendor** — mirror `internal/connector/cisco.go`: pick the
  gnetcli vendor package, build the command list (the config dump must be the
  last command — `GetConfig` returns `res[1]`).
- **New gnetcli profile** — add a `platformProfiles`/`vendorProfiles` entry with
  the server's device-name and the config command. Confirm the server knows that
  name (built-in or dev-conf).
- **New parser** — add a parser in `internal/parser` keyed by platform/vendor
  with a generic fallback; parsers are pure, so unit-test against captured
  configs.
- **New export format** — add a renderer in `internal/exporter` over the shared
  `View` projection.
