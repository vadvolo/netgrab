# Parsing configs & exporting to CSV / JSON / YAML / TOML / Terraform

After `orion-collect` grabs raw configuration from the Orion fleet, netgrab can
**parse** each dump into a normalized, vendor-neutral model and **export** that
model into downstream formats — including Terraform for NetBox.

```
 collect (gnetcli)        parse                     export
┌───────────────┐   ┌───────────────────┐   ┌──────────────────────────┐
│ raw config    │──▶│ DeviceConfig (pb) │──▶│ json · yaml · toml · csv │
│ show run /... │   │ ifaces·vlans·routes│   │ tf (NetBox)              │
└───────────────┘   └───────────────────┘   └──────────────────────────┘
        │                     │                          │
        ▼                     ▼                          ▼
  configs/<dev>/*.txt   parsed/<dev>/*.pb,*.json    exports/<ts>/*.{json,…,tf}
```

## The model (protobuf)

The canonical model lives in [`proto/netgrab/v1/device.proto`](../proto/netgrab/v1/device.proto)
and is generated into `internal/model/netgrabv1`. A `DeviceConfig` captures:

- **identity** — hostname, vendor, platform, model, OS version, serial, mgmt IP;
- **provenance** — source command, raw-config SHA-256, size, collection time,
  which parser produced it;
- **facts** — interfaces (description, admin state, MTU, MAC, IPs, access/trunk
  mode, VLANs), VLAN definitions, and static routes;
- **warnings** — anything the parser could not fully understand.

Regenerate the Go after editing the `.proto`:

```bash
make proto   # runs protoc with protoc-gen-go
```

## Parsers

`internal/parser` resolves a parser the same way collection resolves a profile —
most-specific platform first, then vendor, then a generic fallback that never
fails:

| Parser | Platforms / vendors | Notes |
|--------|---------------------|-------|
| `cisco-ios` | ios, iosxe, iosxr, nxos, mes, ruijie, procurve, zynos, tplink, dcn; vendors cisco, eltex, hp, zyxel, … | IOS-style "global + indented interface" grammar |
| `mikrotik-routeros` | routeros, swos; vendor ros | `/export` sections, `key=value`, line-continuations |
| `huawei-vrp` | smartax, olt; vendors huawei, h3c | VRP: `sysname`, `vlan N smart`/`N to M`, `port default/trunk vlan`, `ip route-static`; OLT uses `display saved-configuration` |
| `ubiquiti-airos` | airos; vendor ubiquiti | `system.cfg` dotted `key=value`: `netconf.*` interfaces, `route.*`, SSID/radio |
| `cambium-epmp` | epmp; vendor cambium | `config show` two-column `key value`: bridge/lan/wan address groups, SSID, gateway route |
| `generic` | everything else (Mimosa, Linux, …) | identity + best-effort hostname only |

Parsers are pure and deterministic (they never read the clock or network), so
they are easy to unit-test against real captured configs — see
`internal/parser/parser_test.go`.

## Storage: local + S3

`internal/storage` defines a `Store` (`Put(key, data)`) with two backends:

- **local** — files under `storagePath` (default `./tmp/local-storage`);
- **s3** — any S3-compatible store. For example a **MinIO** instance
  (`minio.example.net:9000`, bucket `netgrab`, region `us-east-1`, path-style),
  or a **cloud object store** (e.g. bucket `orion-net-cfg`,
  `https://s3.example-cloud.net`, `us-east-1`) — see the commented block in
  `config/orion.yaml`.

MinIO requires path-style addressing (`use_path_style: true`); most cloud stores
use the default virtual-hosted style.

`storage.Build` returns a `Multi` that fans each write out to local **and** S3
when S3 is enabled, so a single run keeps both in sync. Keys are stable:

```
configs/<device>/<device>_<timestamp>.txt    # raw dump
parsed/<device>/<device>_<timestamp>.pb       # protobuf binary model
parsed/<device>/<device>_<timestamp>.json     # protojson (human-readable)
devices/<hostname>_<ip>.json                   # latest per-device model, stable key
exports/<timestamp>/devices.{json,yaml,toml,csv}
exports/<timestamp>/netbox.tf
```

The `devices/<hostname>_<ip>.json` files (written by collection and by
`netgrab-export -per-device`, default on) give consumers a stable, directly
addressable key per device — see [Integrating with netgrab](integration.md).

### Enabling S3

netgrab talks to S3 through the AWS SDK directly — **no AWS CLI needed to run
it**. Prepare a static access key for a service account with object-storage
write access, then either export the standard env vars or configure a shared
profile:

```bash
export S3_ENABLED=true
export AWS_ACCESS_KEY_ID=<key-id>
export AWS_SECRET_ACCESS_KEY=<secret>
# or: set `profile: s3-storage` in config and configure ~/.aws
```

To verify the bucket independently with the AWS CLI (optional):

```bash
aws --profile s3-storage --endpoint-url "$S3_ENDPOINT" \
  s3 ls "s3://$S3_BUCKET/" --recursive
```

## Exporters

`internal/exporter` renders a `DeviceConfigSet` into five formats. Every text
format shares one projection (`exporter.View`) so enums become readable strings
and timestamps become RFC3339.

| Format | Extension | Shape |
|--------|-----------|-------|
| `json` | `.json` | indented document, `{ "devices": [...] }` |
| `yaml` | `.yaml` | same document as YAML |
| `toml` | `.toml` | same document as TOML |
| `csv`  | `.csv`  | flat, one row per interface (device columns repeated) |
| `tf`   | `.tf`   | Terraform for the NetBox provider `e-breuninger/netbox` |

### NetBox Terraform

The `tf` exporter emits `netbox_manufacturer`, `netbox_device_type`,
`netbox_device`, `netbox_device_interface`, `netbox_ip_address` and
`netbox_vlan` resources, wired by Terraform references. Two facts a config can't
carry — **site** and **device role** — are exposed as input variables:

```bash
terraform apply \
  -var 'netbox_site_id=1' \
  -var 'netbox_device_role_id=1'
```

Interface media types are a best-effort mapping (`virtual` / `lag` /
`1000base-t`); refine them per hardware after import. The generated file is
`terraform fmt`-clean and passes `terraform validate`.

## Running the exporter

```bash
# Export whatever the collector has already parsed (reads parsed/*.pb):
go run ./cmd/netgrab-export -config ./config/orion.yaml

# Only some formats:
go run ./cmd/netgrab-export -format tf,json

# Parse raw *.txt dumps offline instead (useful on captured samples):
go run ./cmd/netgrab-export -raw-dir ./tmp/local-storage -vendor cisco -platform ios
```

When run offline with `-raw-dir`, device names are matched against the inventory
to recover each device's vendor/platform; unmatched devices fall back to the
`-vendor`/`-platform` flags.

## Backfilling S3

To seed S3 with files already in local storage (e.g. collected before S3 was
enabled, or after switching backends), upload them under the same keys:

```bash
go run ./cmd/netgrab-backfill -config ./config/orion.yaml -prefixes configs,parsed
```

It reads `<storage>/configs` and `<storage>/parsed` and PUTs each file to the
configured bucket at its relative key. Existing objects are overwritten.
