//go:build linux

package fusefs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/WywySenarios/wc-fuse/policy"
)

// TestRealMount exercises a real mount. It is opt-in because it needs
// /dev/fuse and fusermount3; CI runs it in the privileged Docker-in-
// Docker job with WC_FUSE_MOUNT_TEST=1.
func TestRealMount(t *testing.T) {
	if os.Getenv("WC_FUSE_MOUNT_TEST") == "" {
		t.Skip("set WC_FUSE_MOUNT_TEST=1 to run real-mount tests")
	}

	backing := t.TempDir()
	if err := os.WriteFile(filepath.Join(backing, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(backing, "ro"), 0o755); err != nil {
		t.Fatal(err)
	}

	mnt := t.TempDir()
	cfgDir := t.TempDir()
	writeFile(t, filepath.Join(cfgDir, "fs.yaml"), "target_home: /home/wc/workspace\npermissions:\n  - path: ro\n    read: true\n    write: false\n  - path: \"\"\n    read: true\n    write: true\n    execute: true\n")
	writeFile(t, filepath.Join(cfgDir, "project-roots.yaml"), "project_roots:\n  - "+backing+"\n")
	pol, err := policy.Load(cfgDir)
	if err != nil {
		t.Fatal(err)
	}

	srv, err := Mount(backing, mnt, policy.NewStore(pol), WithAllowOther(false))
	if err != nil {
		t.Skipf("cannot mount fuse here: %v", err)
	}
	defer func() { _ = srv.Unmount() }()
	_ = srv.WaitMount()

	// Read the backing file through the mount.
	if data, err := os.ReadFile(filepath.Join(mnt, "hello.txt")); err != nil {
		t.Fatalf("read through mount: %v", err)
	} else if string(data) != "hi" {
		t.Fatalf("read through mount = %q, want %q", data, "hi")
	}

	// The root rule allows writes.
	if err := os.WriteFile(filepath.Join(mnt, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write through mount: %v", err)
	}

	// The ro rule denies writes.
	if err := os.WriteFile(filepath.Join(mnt, "ro", "nope.txt"), []byte("x"), 0o644); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("write to a read-only path: err = %v, want permission denied", err)
	}

	// An absolute symlink target escapes the workspace namespace.
	if err := os.Symlink("/etc/passwd", filepath.Join(mnt, "escape")); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("escaping symlink: err = %v, want permission denied", err)
	}

	time.Sleep(50 * time.Millisecond)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
