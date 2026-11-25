# Integrating with netgrab — consuming device data

netgrab is a collection/parse pipeline, not an online API. Another service
consumes its output by **reading the artifacts it writes** to shared storage
(S3 or a filesystem). This guide shows the data contract and three ways to read
it, from easiest to most strongly-typed.

## What netgrab produces, and where

Every run writes under a storage root (local dir and/or an S3 bucket) with a
stable key layout:

| Key | Content |
|-----|---------|
| `configs/<device>/<device>_<ts>.txt` | raw CLI dump (as collected) |
| `parsed/<device>/<device>_<ts>.pb` | parsed model — protobuf binary |
| `parsed/<device>/<device>_<ts>.json` | parsed model — protojson (camelCase) |
| `devices/<hostname>_<ip>.json` | **latest** parsed model for one device, stable key (no timestamp) |
| `exports/<ts>/devices.{json,yaml,toml,csv}` | **whole-fleet** parsed model |
| `exports/<ts>/netbox.tf` | Terraform for NetBox |

`<ts>` is `YYYYMMDD-HHMMSS` (UTC). The **latest** `<ts>` per device/export is the
current one; sort keys descending to find it.

Example S3 location (a MinIO instance):

```
endpoint   http://minio.example.net:9000   (path-style)
bucket     netgrab
region     us-east-1
```

## The data contract: `DeviceConfig`

One device after parsing. Field names below are the **export JSON** form
(snake_case, the recommended integration surface). The protobuf/protojson form
is identical in meaning (camelCase names; see `proto/netgrab/v1/device.proto`).

```jsonc
{
  "hostname": "client-router-1",
  "vendor": "ros",                 // inventory vendor
  "platform": "routeros",          // inventory platform
  "model": "RBSXTsq5nD",           // if discoverable
  "os_version": "6.49.17",
  "serial": "SN0123456789",
  "management_ip": "192.0.2.20",
  "source_command": "/export",     // CLI command that produced the raw config
  "raw_config_sha256": "2874…",    // integrity / change detection
  "raw_config_bytes": 4964,
  "collected_at": "2026-11-09T12:55:51Z",   // RFC3339 UTC
  "parser": "mikrotik-routeros",   // which parser produced this
  "interfaces": [
    {
      "name": "bridge1",
      "description": "",
      "enabled": true,
      "type": "bridge",            // ethernet|vlan|loopback|lag|bridge|wlan|tunnel|mgmt
      "mtu": 1500,
      "mac_address": "",
      "ip_addresses": ["192.0.2.20/29"],  // CIDR
      "mode": "routed",            // access|trunk|routed|"" (unspecified)
      "access_vlan": 0,            // untagged VLAN when mode=access
      "tagged_vlans": [],          // tagged VLANs when mode=trunk
      "parent": ""
    }
  ],
  "vlans":  [ { "id": 100, "name": "users" } ],
  "routes": [ { "prefix": "0.0.0.0/0", "next_hop": "192.0.2.1", "distance": 1 } ],
  "warnings": []                   // non-fatal parser notes
}
```

Omitted/empty fields may be absent in JSON (`omitempty`). `parser: "generic"`
means only identity was extracted (no dedicated parser for that platform yet).

## Targeted single-device access — `devices/<hostname>_<ip>.json`

When you need **one specific device** and want to fetch it directly (no listing,
no timestamp guessing), read the stable per-device key:

```
devices/<hostname>_<ip>.json      e.g.  devices/client-router-1_192.0.2.13.json
```

It holds that device's latest parsed model (same clean JSON object as a fleet
`devices.json` entry) and is **overwritten on every collection**, so a plain GET
always returns the current state. `<hostname>` is the inventory device name and
`<ip>` its management IP, both path-sanitized (`/`, space, `:` → `_`).

```python
key = f"devices/{hostname}_{ip}.json"
dev = json.loads(s3.get_object(Bucket="netgrab", Key=key)["Body"].read())
```

For bulk or discovery, use the fleet export below.

## Option A — read the fleet export (simplest, any language)

`exports/<ts>/devices.json` is one document, `{ "devices": [ DeviceConfig… ] }`.
Pull the latest and iterate. Example with Python + boto3 against the MinIO
bucket:

```python
import boto3, json

s3 = boto3.client(
    "s3",
    endpoint_url="http://minio.example.net:9000",
    aws_access_key_id="…", aws_secret_access_key="…",
    region_name="us-east-1",
)
# newest exports/<ts>/devices.json
keys = [o["Key"] for o in s3.list_objects_v2(Bucket="netgrab", Prefix="exports/")["Contents"]
        if o["Key"].endswith("/devices.json")]
latest = max(keys)
doc = json.loads(s3.get_object(Bucket="netgrab", Key=latest)["Body"].read())

for d in doc["devices"]:
    for i in d.get("interfaces", []):
        for cidr in i.get("ip_addresses", []):
            print(d["hostname"], i["name"], cidr)
```

Prefer `devices.csv` for spreadsheet/ETL loads (one row per interface) or
`devices.yaml` / `devices.toml` if your stack prefers those.

## Option B — strongly-typed via protobuf

For a Go (or any protobuf) service that wants typed access and per-device
granularity, consume `parsed/<device>/<ts>.pb` and decode with the shared
schema `proto/netgrab/v1/device.proto`.

```go
import (
    netgrabv1 "netgrab/internal/model/netgrabv1" // or your own generated package
    "google.golang.org/protobuf/proto"
)

var dc netgrabv1.DeviceConfig
_ = proto.Unmarshal(pbBytes, &dc)      // pbBytes read from S3/filesystem
for _, i := range dc.Interfaces { /* … */ }
```

Generate stubs in your own repo with `protoc --go_out=…` against that `.proto`
(it has no external imports). The `.pb` is the same model the pipeline produced,
byte-for-byte.

## Option C — per-device JSON (protojson)

`parsed/<device>/<ts>.json` is protojson (camelCase: `osVersion`,
`managementIp`, `mode: "INTERFACE_MODE_ACCESS"`). Use it when you want one
device without parsing the whole fleet file and don't need protobuf tooling.

## Freshness & change detection

- **Latest wins:** keys are timestamped; take the max `<ts>`.
- **Change detection:** compare `raw_config_sha256` between runs — unchanged
  hash means the device config did not change, so you can skip re-processing.
- **Collection cadence:** netgrab is run on demand / on a schedule; it does not
  push. Poll the bucket, or trigger a run and then read the new `exports/<ts>/`.

## If you only need a filesystem

The same layout exists on disk under `storagePath` (default
`./tmp/local-storage`) when S3 is disabled or in addition to it. Point your
reader there instead of S3 — identical keys.
