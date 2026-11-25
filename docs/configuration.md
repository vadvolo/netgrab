# Configuration reference

netgrab reads configuration from YAML files (loaded with
[cleanenv](https://github.com/ilyakaznacheev/cleanenv)) and/or environment
variables. Both collectors resolve the config file path from the `CONFIG_PATH`
environment variable.

## Local storage config

Used by the general app config (`internal/config`). See `config/local.yaml`.

```yaml
enabled: true
storagePath: "./tmp/local-storage"

httpServer:
  address: "localhost:8080"
  timeout: 3s
  idle_timeout: 60s
  user: admin
  password: ""   # set via HTTP_SERVER_PASSWORD
```

| Field         | Env var             | Default            | Notes            |
|---------------|---------------------|--------------------|------------------|
| `env`         | `ENV`               | `local`            | required         |
| `storagePath` | `STORAGE_PATH`      | `./local-storage`  | root output dir  |

> The `cmd/local-storage` example constructs `LocalStorage` directly in code
> (`local.NewLocalStorage("./tmp/local-storage", time.DateOnly)`) rather than
> reading this file, so edit the source to change its output path.

## GitLab storage config

Used by `cmd/gitlab-storage`. See `config/gitlab.yaml`.

```yaml
url: "https://gitlab.example.com"
project: "your-group%2Fnetconfigs"
branch: "main"
token: "glpat-xxxxxxxxxxxx"
```

| Field     | Env var          | Default              | Notes                          |
|-----------|------------------|----------------------|--------------------------------|
| `url`     | `GITLAB_URL`     | `https://gitlab.com` | GitLab base URL                |
| `project` | `GITLAB_PROJECT` | *(empty)*            | URL-encoded path or numeric ID |
| `branch`  | `BRANCH`         | `master`             | target branch                  |
| `token`   | `GITLAB_TOKEN`   | **required**         | PAT with `api` scope           |

Environment variables override the file values. Example:

```bash
export CONFIG_PATH=./config/gitlab.yaml
export GITLAB_URL=https://gitlab.example.com
export GITLAB_PROJECT=netops%2Fconfigs
export GITLAB_TOKEN=glpat-xxxxxxxxxxxx
go run ./cmd/gitlab-storage
```

## Device credentials

The `local-storage` and `gitlab-storage` collectors load their devices from a
**device inventory** (`-inventory` / `DEVICES_INVENTORY`, default
`./config/devices.example.yaml`) and build the `[]connector.Device` slice with
`connector.FromInventory`. The inventory lists device name/address/vendor; the
login and password live in `${ENV}` references inside credential groups, so no
secret is committed:

```yaml
# config/devices.yaml  (gitignored; copy from devices.example.yaml)
devices:
  - hostname: "csr1000v"
    ip: 192.0.2.1
    vendor: cisco
    platform: ios
credentials:
  default:
    login: "${DEVICE_USERNAME}"
    password: "${DEVICE_PASSWORD}"
```

Supply the secrets via the environment (every command loads `.env` at startup —
see `.env.example`):

```bash
export DEVICE_USERNAME=admin
export DEVICE_PASSWORD=secret
```

### Constructing devices directly

For a one-off or programmatic use you can still build a `connector.Device`
yourself — read the credentials from the environment, never hardcode them:

```go
connector.NewCiscoDevice(
	"csr1000v",
	os.Getenv("DEVICE_USERNAME"),
	"192.0.2.1",
	os.Getenv("DEVICE_PASSWORD"),
)
```
