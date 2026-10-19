# wc-fuse

This repo is part of the Wywy repo line and is subject to Wywy conventions.

WywyCode's host-side FUSE workspace, responsible for:

- rwx policy engine
- FUSE daemon
- FUSE control socket
- systemD units & installer

This repo does not provide a client.

## A brief explanation

The daemon and its root setup helper are host-side; on the machine that contains the files you want to have a FUSE over (and consequently outside of a container).

The daemon trusts no pod input beyond `{session_id, project_root}`.

- **Policy is host-authoritative.** The daemon reads `fs.yaml` (rwx rules) and `project-roots.yaml` (the allowlist) from the host config clone (`/etc/wc/config.d`), never from the pod. It refuses any `project_root` outside the allowlist.
- Some superuser actions are needed, including:
  - `/dev/wc` is root-created (0711) by tmpfiles
  - the `wc-fuse-root.service` **system** unit marks `/dev/wc` as rshared
  - `/dev/wc/workspace` & `/dev/wc/control` and their permissions are modified for unprivileged use
- the `wc-fuse.service` **user** unit runs the unprivileged daemon.

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
  - /home/eric/src
```

## Control protocol

JSON over `/dev/wc/control/wc-fuse.sock`:

| op        | request                                               | response                                          |
| --------- | ----------------------------------------------------- | ------------------------------------------------- |
| `mount`   | `{"op":"mount","session_id":"s","project_root":"/p"}` | `{"ok":true,"mount_point":"...","open_mounts":n}` |
| `unmount` | `{"op":"unmount","project_root":"/p"}`                | `{"ok":true,"open_mounts":n}`                     |
| `status`  | `{"op":"status"}`                                     | `{"ok":true,"open_mounts":n}`                     |

## Installation

```sh
scripts/install.sh --dry-run    # preview
scripts/install.sh              # install
scripts/uninstall.sh            # remove
```
