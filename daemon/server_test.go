package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WywySenarios/wc-fuse/policy"
	"github.com/WywySenarios/wc-fuse/protocol"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func policyStore(t *testing.T, roots ...string) *policy.Store {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "fs.yaml"),
		"target_home: /home/wc/workspace\npermissions:\n  - path: \"\"\n    read: true\n    write: true\n    execute: true\n")
	var b strings.Builder
	b.WriteString("project_roots:\n")
	for _, r := range roots {
		fmt.Fprintf(&b, "  - %s\n", r)
	}
	writeFile(t, filepath.Join(dir, "project-roots.yaml"), b.String())
	p, err := policy.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return policy.NewStore(p)
}

func newTestServer(t *testing.T, allowedUID uint32, roots ...string) string {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "ctl", "wc-fuse.sock")
	mounts := NewMountManager(newFakeMounter(), filepath.Join(t.TempDir(), "workspace"))
	srv, err := NewServer(Config{
		SocketPath: socket,
		AllowedUID: allowedUID,
		Policy:     policyStore(t, roots...),
		Mounts:     mounts,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() { _ = srv.Serve() }()
	t.Cleanup(func() { _ = srv.Close() })
	return socket
}

func roundTrip(t *testing.T, socket string, req protocol.Request) protocol.Response {
	t.Helper()
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var resp protocol.Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

func TestServerMountUnmountStatus(t *testing.T) {
	socket := newTestServer(t, uint32(os.Getuid()), "/home/eric/src")
	root := "/home/eric/src/proj"

	resp := roundTrip(t, socket, protocol.Request{Op: protocol.OpMount, SessionID: "s1", ProjectRoot: root})
	if !resp.OK {
		t.Fatalf("mount failed: %s %s", resp.Code, resp.Error)
	}
	if resp.MountPoint == "" {
		t.Error("mount response must carry a mount point")
	}
	if resp.OpenMounts != 1 {
		t.Errorf("open mounts = %d, want 1", resp.OpenMounts)
	}

	if got := roundTrip(t, socket, protocol.Request{Op: protocol.OpStatus}); got.OpenMounts != 1 {
		t.Errorf("status open mounts = %d, want 1", got.OpenMounts)
	}

	resp = roundTrip(t, socket, protocol.Request{Op: protocol.OpUnmount, ProjectRoot: root})
	if !resp.OK || resp.OpenMounts != 0 {
		t.Errorf("unmount = %+v, want ok with 0 open mounts", resp)
	}
}

func TestServerDeniesOutsideAllowlist(t *testing.T) {
	socket := newTestServer(t, uint32(os.Getuid()), "/home/eric/src")

	resp := roundTrip(t, socket, protocol.Request{Op: protocol.OpMount, ProjectRoot: "/etc"})
	if resp.OK {
		t.Fatal("mount of a root outside the allowlist must be refused")
	}
	if resp.Code != protocol.CodeDenied {
		t.Errorf("code = %q, want %q", resp.Code, protocol.CodeDenied)
	}
}

func TestServerBadRequests(t *testing.T) {
	socket := newTestServer(t, uint32(os.Getuid()), "/home/eric/src")

	if resp := roundTrip(t, socket, protocol.Request{Op: "bogus"}); resp.Code != protocol.CodeBadRequest {
		t.Errorf("unknown op code = %q, want %q", resp.Code, protocol.CodeBadRequest)
	}
	if resp := roundTrip(t, socket, protocol.Request{Op: protocol.OpMount}); resp.Code != protocol.CodeBadRequest {
		t.Errorf("missing project_root code = %q, want %q", resp.Code, protocol.CodeBadRequest)
	}
}

func TestServerRejectsForeignUID(t *testing.T) {
	// Pretend the allowed uid is someone else; our own connection (the
	// test process uid) must be closed before any request is served.
	socket := newTestServer(t, uint32(os.Getuid())+1, "/home/eric/src")

	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	encErr := json.NewEncoder(conn).Encode(protocol.Request{Op: protocol.OpStatus})
	var resp protocol.Response
	decErr := json.NewDecoder(conn).Decode(&resp)
	if encErr == nil && decErr == nil {
		t.Fatal("a foreign-uid connection must not receive a response")
	}
}
