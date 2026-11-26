# Contributing to wc-fuse

## Layout

- `policy/`, the permissions engine.
- `fusefs/` — the policy-enforcing passthrough filesystem (go-fuse loopback).
- `daemon/` — the control-socket server, `SO_PEERCRED` authorization, and the per-project mount lifecycle.
- `cmd/wc-fuse-daemon/`, the FUSE (user unit) daemon
- `ansible/`, the role and playbooks that provision the host (units, tmpfiles, policy).
- `ansible/roles/wc_fuse/files/wc-config/`, the policy submodule (tracks `main`).
- `Makefile`, the build and provisioning entry points.
- `scripts/tests/`, test suites

The in-pod client lives in the separate `wc-fuse-client` repo and imports this repo's `protocol` package for the control-socket wire types.

## Docs & Examples

### Timeline example

With `--workspace /dev/wc/workspace` and an allowlist containing `/srv`. Holders and active mounts are shown after each event:

| Step | Call                                | Effect                                                             | Holders    | # active mounts |
| ---- | ----------------------------------- | ------------------------------------------------------------------ | ---------- | --------------- |
| 1    | `mount s1 /srv/ex-dir`              | Creates mount at `/dev/wc/workspace/a1cc46b3f53a362f`              | `{s1}`     | 1               |
| 2    | `mount s2 /srv/ex-dir`              | Adds holder to `/dev/wc/workspace/a1cc46b3f53a362f`                | `{s1, s2}` | 1               |
| 3    | `mount s3 /srv/ex-dir2`             | Creates mount at `/dev/wc/workspace/a05909a72a8e608b`              | `{s3}`     | 2               |
| 4    | `unmount s1 /srv/ex-dir`            | Removes holder from `/dev/wc/workspace/a1cc46b3f53a362f`           | `{s2}`     | 2               |
| 5    | `unmount s2 /srv/ex-dir`            | Unmounts `/dev/wc/workspace/a1cc46b3f53a362f`: no holders are left | —          | 1               |
| 6    | `mount s4 /srv/ex-dir`              | Remounts `/dev/wc/workspace/a1cc46b3f53a362f`                      | `{s4}`     | 2               |
| 7    | session-less `unmount /srv/ex-dir2` | Force teardown for `/dev/wc/workspace/a05909a72a8e608b`            | —          | 1               |

After step 7:

- Two distinct mountpoints have been created: `a1cc46b3f53a362f` and `a05909a72a8e608b`.
- One mount is active, for `/srv/ex-dir`.
- Two mounting locks remain: one per source directory.
- Two mountpoint directories remain. Unmount does not delete them.
- A later `mount /srv/ex-dir` reuses `a1cc46b3f53a362f`.

Note: force teardown will leave other holders with a broken filesystem. @TODO leases.

### Mounting lock

A mounting lock is used when performing mounting operations.

The lock is held for the duration of one `Mount` or `Unmount` call and released when that call returns. It is not held while the mount is live, and file I/O does not take it.

Each source directory has its own mounting lock.

For example,

- Two callers asking for `/srv/ex-dir` might contend on a mounting lock.
- Two callers asking for `/srv/ex-dir1` and `/srv/ex-dir2` respectively will not contend on a mounting lock.

In the [timeline example](#timeline-example):

| Step | Operation                   | Mounting lock  | Held across                                         | Notes                                 |
| ---- | --------------------------- | -------------- | --------------------------------------------------- | ------------------------------------- |
| 1    | `Mount(s1, /srv/ex-dir)`    | `/srv/ex-dir`  | Mountpoint creation, `mounter.Mount`, map insertion | Released when `Mount` returns         |
| 2    | `Mount(s2, /srv/ex-dir)`    | `/srv/ex-dir`  | Holder insertion                                    | Waits if event 1 still holds the lock |
| 3    | `Mount(s3, /srv/ex-dir2)`   | `/srv/ex-dir2` | Mountpoint creation, `mounter.Mount`, map insertion | Does not wait for events 1 or 2       |
| 4    | `Unmount(s1, /srv/ex-dir)`  | `/srv/ex-dir`  | Holder removal                                      | Released when `Unmount` returns       |
| 5    | `Unmount(s2, /srv/ex-dir)`  | `/srv/ex-dir`  | Last-holder check, `server.Unmount`, map removal    | Released when `Unmount` returns       |
| 6    | `Mount(s4, /srv/ex-dir)`    | `/srv/ex-dir`  | Existing-mount lookup, holder insertion             | Reuses the existing mountpoint        |
| 7    | `Unmount("", /srv/ex-dir2)` | `/srv/ex-dir2` | Force teardown, `server.Unmount`, map removal       | Released when `Unmount` returns       |

## Testing

`scripts/tests/` suites must pass before commit. pre-commit runs them.

```sh
pre-commit run
```

It is highly advised to run tests before making modifications to set a baseline.

The real-mount suite is opt-in via `WC_FUSE_MOUNT_TEST=1`.
