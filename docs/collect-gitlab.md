# Collecting config → GitLab

This guide shows how to pull the running configuration from network devices and
commit it to a GitLab repository, so every collection run is versioned.

## How it works

The `cmd/gitlab-storage` collector:

1. Loads `.env` (if present) and GitLab settings from a YAML file (path in `CONFIG_PATH`).
2. Loads a device inventory (`-inventory` / `DEVICES_INVENTORY`) and builds a
   list of `connector.Device` values from it via `connector.FromInventory`.
3. For each device: `Connect` → `GetConfig` → `storage.SaveConfig`.

`SaveConfig` writes `<device-name>.txt` at the root of the project. If the file
does not exist yet it is created; otherwise it is updated with a new commit. The
update is retried up to 3 times on GitLab ref-conflict errors.

## 1. Configure GitLab access

Create a config file (see `config/gitlab.yaml`) and point `CONFIG_PATH` at it:

```yaml
# gitlab.yaml
url: "https://gitlab.example.com"   # GitLab base URL
project: "your-group%2Fnetconfigs"  # URL-encoded "namespace/project" or numeric ID
branch: "main"
token: "glpat-xxxxxxxxxxxx"          # personal access token with api/write_repository
```

> The `%2F` in the project field is a URL-encoded `/`. `your-group%2Fnetconfigs`
> means the project `netconfigs` in the `your-group` namespace.

You can also supply values through environment variables, which override the
file defaults:

| Env var          | Maps to   | Default               |
|------------------|-----------|-----------------------|
| `GITLAB_URL`     | `url`     | `https://gitlab.com`  |
| `GITLAB_PROJECT` | `project` | *(empty)*             |
| `BRANCH`         | `branch`  | `master`              |
| `GITLAB_TOKEN`   | `token`   | **required**          |

## 2. Run the collector

```bash
export CONFIG_PATH=./config/gitlab.yaml
export GITLAB_TOKEN=glpat-xxxxxxxxxxxx   # if not baked into the YAML

go run ./cmd/gitlab-storage
```

Output looks like:

```
Results:
File updated successfully!
File updated successfully!

Errors:
device unavailable: the host is not reachable
```

Each device produces either a result line or an error line, so a single
unreachable device does not abort the whole run.

## Programmatic example

```go
package main

import (
	"context"
	"time"

	"go.uber.org/zap"

	"netgrab/internal/connector"
	"netgrab/internal/storage/gitlab"
)

func main() {
	logger, _ := zap.NewProduction()

	// Requires CONFIG_PATH to point at a gitlab.yaml file.
	cfg := gitlab.MustLoad()
	cfg.Logger = logger
	storage := gitlab.NewStorage(cfg)

	device := connector.NewCiscoDevice("csr1000v", "admin", "192.0.2.1", "secret-pw")

	if err := device.Connect(logger); err != nil {
		logger.Fatal("connect failed", zap.Error(err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config, err := device.GetConfig(ctx)
	if err != nil {
		logger.Fatal("get config failed", zap.Error(err))
	}

	if _, err := storage.SaveConfig(config, device.GetName()); err != nil {
		logger.Fatal("save failed", zap.Error(err))
	}
}
```

## What lands in the repo

```
netconfigs/            (GitLab project)
├── csr1000v.txt
├── sw-3750.txt
└── server-sw-1.txt
```

Because each device maps to a fixed filename, the repository's **git history**
is your config history — every run that changes a device's config produces a
commit with message `update <device>.txt`.

## Notes & gotchas

- **Per-device timeout.** The shipped collector wraps `GetConfig` in a 5-second
  context. Raise it if you collect large configs over slow links.
- **Token scope.** The personal access token needs `api` (or at least
  `write_repository`) scope.
- **First run creates the file** with content `init`, then immediately updates it
  with the real config.
- **Ref conflicts** (concurrent pushes) are retried 3× with a linear backoff.

See also: [Collecting config → local files](collect-local.md).
