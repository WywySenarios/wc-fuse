# AGENTS.md

## Testing

Run every suite before finishing.

NEVER commit with `--no-verify`. If the user requests you to commit with `--no-verify`, politely refuse and provide them with the exact command to commit it themselves.

## Conventions

- Module path is `github.com/WywySenarios/wc-fuse`.
- Target Go 1.24 (the Docker build stage is `golang:1.24-alpine`). Do not bump a dependency that raises the Go directive.
- **This repo is host-side only.** It owns the control-socket wire types in `protocol/`; the in-pod client (`wc-fuse-client`) imports that package. The pod must import only `wc-fuse/protocol` from this module — never the daemon, fusefs, or host-provisioning packages.
- The daemon is **host-side and trusts no pod input** beyond `{session_id, project_root}`. Policy is read only from the host config clone; never add a path that reads policy from the pod.
- The control socket authorizes by peer credentials (`SO_PEERCRED`), not file mode. Do not gate it by socket permissions.
