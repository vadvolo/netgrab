# Collecting Orion config via the gnetcli server

Unlike the Cisco/Huawei connectors — which open an SSH session **directly** from
the machine running netgrab — Orion collection goes through a **remote gnetcli
gRPC server**. netgrab asks that server to open the SSH session and run the
config command; the server sits inside the Orion network and can reach equipment
the laptop cannot.

```
┌──────────┐  gRPC (50051)  ┌──────────────────────┐   SSH   ┌──────────────────┐
│ netgrab  │───────────────▶│   gnetcli_server     │────────▶│  Orion equipment │
│ (host)   │  Exec(cmd)     │ gnetcli.example.net  │  per    │ RouterOS/Eltex/  │
└────┬─────┘                └──────────────────────┘  vendor │ Cisco/Huawei/... │
     │ config bytes                                        └──────────────────┘
     ▼
 local storage (./tmp/local-storage/<device>/…)
```

## Two collection paths: `-via gnetcli` vs `-via annet-oil`

`orion-collect` can reach devices two ways:

| `-via` | How it works | Credentials |
|--------|--------------|-------------|
| `gnetcli` (default) | netgrab calls the gnetcli gRPC server directly | netgrab holds the device creds (env `DEVICE_USERNAME`/`PASSWORD`, …) |
| `annet-oil` | netgrab POSTs `host`+`command` to annet-oil's HTTP execute API | **annet-oil resolves the creds** — none needed on the netgrab side |

The **annet-oil path is usually easier**: annet-oil already owns the Orion
inventory and every device's credentials, so you only supply a bearer token.

```
┌──────────┐  POST /api/v0/execute/   ┌──────────────────┐  gRPC   ┌───────────┐
│ netgrab  │─────────────────────────▶│  annet-oil API   │────────▶│  gnetcli  │──▶ device
│          │  {host, command}         │  :8181 (+creds)  │         │  server   │
└──────────┘  Authorization: Bearer   └──────────────────┘         └───────────┘
```

Set the token (and optionally base URL) in `config/orion.yaml` under `annetoil`,
or via `ANNETOIL_TOKEN` / `ANNETOIL_BASE_URL`, then:

```bash
# Dry-run the plan (no token needed):
go run ./cmd/orion-collect -config ./config/orion.yaml -via annet-oil -vendor ros -limit 5 -dry-run

# Real collection through annet-oil (token required):
go run ./cmd/orion-collect -config ./config/orion.yaml -via annet-oil -vendor ros -limit 5
```

annet-oil's execute API whitelists read-only commands, which covers every
collection command netgrab uses: `show running-config` (Cisco/IOS-like),
`/export` (RouterOS), `display current-configuration` (Huawei).

## Prerequisites

1. **Network reachability** — the gnetcli server (e.g. `gnetcli.example.net`) is
   reachable from where netgrab runs, over your management network/VPN.
2. **gnetcli_server running** on that host, bound to `0.0.0.0:50051` with basic
   auth (see the annet-oil `configs/systemd/gnetcli.service`). Verify:

   ```bash
   nc -vz "$GNETCLI_HOST" 50051
   ```

   If it only binds `localhost`, tunnel it: `ssh -L 50051:localhost:50051 user@gnetcli.example.net`
   and set `GNETCLI_HOST=localhost`.

## Configure

Copy `config/orion.yaml` and supply secrets via the environment (nothing secret
is committed):

```bash
export GNETCLI_LOGIN=...          # gnetcli server basic-auth user
export GNETCLI_PASSWORD=...       # gnetcli server basic-auth password
# device credential groups referenced by the inventory:
export DEVICE_USERNAME=...        DEVICE_PASSWORD=...
export INFRA_ROUTER_USERNAME=...  INFRA_ROUTER_PASSWORD=...
export CLIENT_ROUTER_USERNAME=... CLIENT_ROUTER_PASSWORD=...
```

Point `inventoryPath` at the full Orion inventory (the same file annet-oil
uses):

```yaml
inventoryPath: "/path/to/annet-oil/resources/inventory.orion.yaml"
```

## Run

```bash
# Dry run — resolve profiles and print the plan, no server contact:
go run ./cmd/orion-collect -config ./config/orion.yaml -dry-run

# Collect everything supported (fans out at `concurrency` workers):
go run ./cmd/orion-collect -config ./config/orion.yaml

# Narrow it down while testing:
go run ./cmd/orion-collect -config ./config/orion.yaml -vendor eltex -limit 5
go run ./cmd/orion-collect -config ./config/orion.yaml -host site-a-eltex-1
```

Flags: `-vendor`, `-platform`, `-host` (substring), `-limit`, `-dry-run`,
`-config`.

Configs land under `storagePath` (default `./tmp/local-storage`), one folder per
device with a timestamped file per run.

## Supported types and models

Every vendor/platform in the Orion inventory maps to a gnetcli device profile
and a config command (see `internal/connector/profile.go`). Highlights:

| Platform | gnetcli profile | Config command |
|----------|-----------------|----------------|
| `routeros`, `swos` (MikroTik) | `ros` | `/export` |
| `ios`, `iosxe`, `iosxr` (Cisco) | `cisco` | `show running-config` |
| `nxos` (Cisco) | `nxos` | `show running-config` |
| `mes` (Eltex) | `eltex`* | `show running-config` |
| `smartax`, `olt` (Huawei) | `huawei` | `display current-configuration` |
| `ruijie`, `procurve` (HP), `zynos`/`ies` (Zyxel), `tplink`, `dcn` | `cisco` | `show running-config` |
| `airos` (Ubiquiti) | `pc` | `cat /tmp/system.cfg` |
| `epmp` (Cambium), `mimosa` | `pc` | `config show` |
| `linux`, `generic` | `pc` | best-effort |

\* `eltex` must be present in the gnetcli server's `dev-conf`; the stock server
uses a custom gnetcli build that adds it.

Device classes with **no CLI config** — Windows/Linux hosts, APC PDU/UPS,
Carel HVAC controllers, Padtec optical transport, Fujitsu iRMC BMCs — are marked
`Supported: false` and skipped (reported as `skipped_unsupported`). On a full
mixed inventory this is a small fraction (roughly a hundred devices).

## How it hangs together

- `internal/gnetcli` — the gRPC client (`Exec` on a `Target`). Device-level
  failures (auth, timeout, unknown profile) come back as a failed `ExecResult`
  with an `ErrorCode`, not a Go error; only an unreachable server is an error.
- `internal/connector/profile.go` — the vendor/model → profile+command registry.
- `internal/connector/orion.go` — `OrionDevice`, which satisfies the same
  `connector.Device` interface as the Cisco/Huawei connectors but runs through
  the server.
- `internal/inventory` — loads the inventory + credential groups (`${ENV}`
  expanded).
- `cmd/orion-collect` — the local collection runner.
