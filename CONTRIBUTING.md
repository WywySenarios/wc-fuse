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

## Testing

`scripts/tests/` suites must pass before commit. pre-commit runs them.

```sh
pre-commit run
```

It is highly advised to run tests before making modifications to set a baseline.

The real-mount suite is opt-in via `WC_FUSE_MOUNT_TEST=1`.
