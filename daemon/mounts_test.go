package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

func (f *fakeMounter) has(mountpoint string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mounted[mountpoint]
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

// blockingMounter blocks Mount for blockRoot until release is closed and closes entered on entry.
// Other roots pass straight through, exposing whether unrelated mounts serialize.
type blockingMounter struct {
	*fakeMounter

	blockRoot string
	entered   chan struct{}
	release   chan struct{}

	enterOnce sync.Once
}

func (b *blockingMounter) Mount(target, mountpoint string, store *policy.Store) (MountServer, error) {
	if filepath.Clean(target) == filepath.Clean(b.blockRoot) {
		b.enterOnce.Do(func() { close(b.entered) })
		<-b.release
	}
	return b.fakeMounter.Mount(target, mountpoint, store)
}

func TestMountManagerLifecycle(t *testing.T) {
	m := newFakeMounter()
	workspace := t.TempDir()
	mg := NewMountManager(m, workspace)
	root := "/srv/wc/projects/proj"

	mp, err := mg.Mount("s1", root, nil)
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if want := filepath.Join(workspace, ProjectHash(root)); mp != want {
		t.Errorf("mount point = %q, want %q", mp, want)
	}
	if mg.OpenMounts() != 1 {
		t.Errorf("open mounts = %d, want 1", mg.OpenMounts())
	}

	// Idempotent: a second mount returns the same point and does not
	// mount again.
	mp2, err := mg.Mount("s1", root, nil)
	if err != nil {
		t.Fatalf("second Mount: %v", err)
	}
	if mp2 != mp {
		t.Errorf("second mount point = %q, want %q", mp2, mp)
	}
	if m.count() != 1 {
		t.Errorf("underlying mounts = %d, want 1 (idempotent)", m.count())
	}

	if err := mg.Unmount("s1", root); err != nil {
		t.Fatalf("Unmount: %v", err)
	}
	if mg.OpenMounts() != 0 {
		t.Errorf("open mounts after unmount = %d, want 0", mg.OpenMounts())
	}

	// Idempotent: unmounting again is a no-op.
	if err := mg.Unmount("s1", root); err != nil {
		t.Fatalf("second Unmount: %v", err)
	}
}

func TestLifecycleDoesNotBlockAcrossProjects(t *testing.T) {
	const (
		rootA   = "/srv/wc/projects/proj-a"
		rootB   = "/srv/wc/projects/proj-b"
		timeout = time.Second
	)

	f := newFakeMounter()
	blocking := &blockingMounter{
		fakeMounter: f,
		blockRoot:   rootA,
		entered:     make(chan struct{}),
		release:     make(chan struct{}),
	}
	mg := NewMountManager(blocking, t.TempDir())

	aResult := make(chan error, 1)
	go func() {
		_, err := mg.Mount("s1", rootA, nil)
		aResult <- err
	}()

	select {
	case <-blocking.entered:
	case <-time.After(timeout):
		t.Fatal("Mount(rootA) never reached the mounter")
	}

	var releaseOnce sync.Once
	releaseA := func() { releaseOnce.Do(func() { close(blocking.release) }) }
	aJoined := false
	t.Cleanup(func() {
		releaseA()
		if aJoined {
			return
		}
		select {
		case <-aResult:
		case <-time.After(timeout):
			t.Error("Mount(rootA) did not return after its mounter was released")
		}
	})

	// Mounting a different root must not wait for root A's in-flight mount.
	var bMountPoint string
	bMounted := make(chan error, 1)
	go func() {
		mp, err := mg.Mount("s1", rootB, nil)
		bMountPoint = mp
		bMounted <- err
	}()
	select {
	case err := <-bMounted:
		if err != nil {
			t.Fatalf("Mount(rootB): %v", err)
		}
	case <-time.After(timeout):
		t.Fatal("Mount(rootB) blocked behind Mount(rootA)")
	}
	if bMountPoint == "" {
		t.Fatal("Mount(rootB) returned an empty mount point")
	}

	// OpenMounts must not wait for root A's in-flight mount.
	opened := make(chan int, 1)
	go func() { opened <- mg.OpenMounts() }()
	select {
	case <-opened:
	case <-time.After(timeout):
		t.Fatal("OpenMounts blocked behind Mount(rootA)")
	}

	// Unmounting root B must not wait for root A's in-flight mount.
	unmounted := make(chan error, 1)
	go func() { unmounted <- mg.Unmount("s1", rootB) }()
	select {
	case err := <-unmounted:
		if err != nil {
			t.Fatalf("Unmount(rootB): %v", err)
		}
	case <-time.After(timeout):
		t.Fatal("Unmount(rootB) blocked behind Mount(rootA)")
	}

	releaseA()
	select {
	case err := <-aResult:
		aJoined = true
		if err != nil {
			t.Fatalf("Mount(rootA): %v", err)
		}
	case <-time.After(timeout):
		t.Fatal("Mount(rootA) did not return after its mounter was released")
	}

	if got := mg.OpenMounts(); got != 1 {
		t.Errorf("open mounts = %d, want 1", got)
	}
	if !f.has(mg.Mountpoint(rootA)) {
		t.Errorf("root A is not mounted after its mount completed")
	}
	if f.has(mg.Mountpoint(rootB)) {
		t.Errorf("root B is still mounted after Unmount")
	}
}

func TestMountCreatesMountpoint(t *testing.T) {
	mg := NewMountManager(newFakeMounter(), t.TempDir())
	mp, err := mg.Mount("s1", "/srv/wc/projects/proj", nil)
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	info, err := os.Stat(mp)
	if err != nil {
		t.Fatalf("mount point was not created: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("mount point is not a directory: %s", mp)
	}
}

func TestProjectHashStableAndDistinct(t *testing.T) {
	a := ProjectHash("/srv/wc/projects/a")
	b := ProjectHash("/srv/wc/projects/a/")
	c := ProjectHash("/srv/wc/projects/b")
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
