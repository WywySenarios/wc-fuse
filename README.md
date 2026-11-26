# wc-fuse

This repo is part of the Wywy repo line and is subject to Wywy conventions.

WywyCode's host-side FUSE workspace, responsible for:

- rwx policy engine
- FUSE daemon
- FUSE control socket

This repo does not provide a client.

## A brief explanation

The daemon should run on the host containing the files you want to mount over.

The daemon trusts no input beyond `{session_id, project_root}`.
Clients must supply their own IDs.

### Filesystem

This FUSE's goal is to create a permissions restricted mountpoint at `/dev/wc/workspace/<hash>`.
The hash is calculated based on the source directory.
Implementation detail: multiple consumers asking for the same source directory will result in those consumers sharing the same mountpoint.

The client is responsible for finding a way to get that directory into the filepath.
You cannot configure where the destination of the mountpoint is.
For example, you can bind-mount `/dev/wc/workspace/<hash>` into a Docker container.

## Permissions & Configuration

Rules use **first-match** precedence (top-to-bottom).

A rule's `path` matches by string, after trimming surrounding whitespace and `/` on both sides: the query equals the rule path, or continues past it with `/`.

Every permission bit defaults to `0` (no access).

If a file does not meet any permissions rule, it will have permissions `000`.

An empty (`""`) `path` value will match everything; a missing `path` key is a load error (as are blank and `.` values).

Directory permissions are ignored when looking at child files. This means that locking down a directory does not lock down child files (unless the directory rule matches first!).

Example configuration:

```yaml
target_home: /home/wc/workspace
permissions:
  - path: src # first match wins: readable + writable
    read: true
    write: true
  - path: "" # catch-all last rule: everything else read-only
    read: true
```

Example `/etc/wc/config.d/project-roots.yaml`:

```yaml
project_roots:
  - /srv/wc/projects
```

## Control protocol

JSON over `/dev/wc/control/wc-fuse.sock`:

| op        | request                                                                                                                                                                     | response                                          |
| --------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------- |
| `mount`   | `{"op":"mount","session_id":"s","project_root":"/p"}` — `session_id` is required and must be non-empty                                                                      | `{"ok":true,"mount_point":"...","open_mounts":n}` |
| `unmount` | `{"op":"unmount","session_id":"s","project_root":"/p"}` releases session `s`'s hold; `{"op":"unmount","project_root":"/p"}` (no `session_id`) force-tears-down every holder | `{"ok":true,"open_mounts":n}`                     |
| `status`  | `{"op":"status"}`                                                                                                                                                           | `{"ok":true,"open_mounts":n}`                     |

@TODO add leases

## Installation

Clone with submodules (`git clone --recurse-submodules …`) or initialize them
with `git submodule update --init`.

```sh
uv sync
make provision
```

Run the commands as the user that will own the workspace.

Overrides that might interest you:

- `wc_fuse_allowed_uid` (default `10001`, the pod uid), passed with `make provision ARGS="-e wc_fuse_allowed_uid=…"`

### Uninstallation

```sh
make uninstall
```
