# Troubleshooting

## `device unavailable: the host is not reachable`

`Connect` pings the device before opening SSH and this ping failed.

- Confirm the address is correct and routable from where netgrab runs.
- Check for ICMP filtering — some devices/firewalls drop ping. If ICMP is
  blocked but SSH works, remove/relax the `Ping()` pre-check in the connector.
- On restricted hosts the `ping` binary may be missing or need privileges.

## `CONFIG_PATH environment variable must be set`

The GitLab (and app) config loader needs `CONFIG_PATH` pointing at an existing
file:

```bash
export CONFIG_PATH=./config/gitlab.yaml
```

The same message is (misleadingly) printed when the file path is set but does
not exist — double-check the path resolves from your working directory.

## SSH auth / connection failures during `GetConfig`

Errors are wrapped as `SendCommands: connecting to device <addr>: ...`.

- Verify login/password.
- Ensure the device allows SSH from the netgrab host.
- Confirm the vendor connector matches the device (Cisco vs Huawei) — a wrong
  profile can hang on the prompt or return empty output.

## Empty or wrong config captured

`GetConfig` returns the output of the **second** command in the list
(`res[1].Output()`). If you changed the command list, make sure the config dump
is at index 1 (e.g. `enable` at 0, `show run` at 1). See
[Supported devices](devices.md).

## GitLab: `failed to update file, status code: 400 ... Could not update refs`

Concurrent pushes to the same branch. netgrab already retries 3× with a linear
backoff. If it still fails:

- Reduce collection concurrency, or
- Give each device its own branch/subdirectory.

## GitLab: `status code: 401 / 403`

Token problem. Ensure `GITLAB_TOKEN` is set and the token has `api` (or
`write_repository`) scope for the target project.

## GitLab: file always shows `init` then updates

Expected on first run — a new file is created with placeholder content `init`,
then immediately updated with the real config in the same run.

## One bad device aborts the whole run (local collector)

The shipped `cmd/local-storage` uses `log.Fatal` inside each goroutine, so the
first failure stops the process. Switch to per-device logging (see
[Collecting config → local files](collect-local.md#collecting-from-many-devices-in-parallel))
to keep collecting the rest of the fleet.
