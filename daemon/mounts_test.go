package daemon

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/WywySenarios/wc-fuse/policy"
)

type fakeMounter struct {
	mu      sync.Mutex
	mounted map[string]bool
}

func newFakeMounter() *fakeMounter { return &fakeMounter{mounted: map[string]bool{}} }

func (f *fakeMounter) Mount(_, mountpoint string, _ *policy.Store) (MountServer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mounted[mountpoint] {
		return nil, errors.New("already mounted")
	}
	f.mounted[mountpoint] = true
	return &fakeServer{f: f, mp: mountpoint}, nil
}

func (f *fakeMounter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.mounted)
}

type fakeServer struct {
	f  *fakeMounter
	mp string
}

func (s *fakeServer) Unmount() error {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	delete(s.f.mounted, s.mp)
	return nil
}

func TestMountManagerLifecycle(t *testing.T) {
	m := newFakeMounter()
	mg := NewMountManager(m, "/dev/wc/workspace")
	root := "/home/eric/src/proj"

	mp, err := mg.Mount(root, nil)
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if want := filepath.Join("/dev/wc/workspace", ProjectHash(root)); mp != want {
		t.Errorf("mount point = %q, want %q", mp, want)
	}
	if mg.OpenMounts() != 1 {
		t.Errorf("open mounts = %d, want 1", mg.OpenMounts())
	}

	// Idempotent: a second mount returns the same point and does not
	// mount again.
	mp2, err := mg.Mount(root, nil)
	if err != nil {
		t.Fatalf("second Mount: %v", err)
	}
	if mp2 != mp {
		t.Errorf("second mount point = %q, want %q", mp2, mp)
	}
	if m.count() != 1 {
		t.Errorf("underlying mounts = %d, want 1 (idempotent)", m.count())
	}

	if err := mg.Unmount(root); err != nil {
		t.Fatalf("Unmount: %v", err)
	}
	if mg.OpenMounts() != 0 {
		t.Errorf("open mounts after unmount = %d, want 0", mg.OpenMounts())
	}

	// Idempotent: unmounting again is a no-op.
	if err := mg.Unmount(root); err != nil {
		t.Fatalf("second Unmount: %v", err)
	}
}

func TestProjectHashStableAndDistinct(t *testing.T) {
	a := ProjectHash("/home/eric/src/a")
	b := ProjectHash("/home/eric/src/a/")
	c := ProjectHash("/home/eric/src/b")
	if a != b {
		t.Errorf("hash should ignore a trailing slash: %q != %q", a, b)
	}
	if a == c {
		t.Errorf("distinct roots must hash differently: %q == %q", a, c)
	}
	if len(a) != 16 {
		t.Errorf("hash length = %d, want 16", len(a))
	}
}
