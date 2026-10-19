package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sync"

	"github.com/WywySenarios/wc-fuse/policy"
)

// MountServer is a running mounted filesystem.
type MountServer interface {
	Unmount() error
}

// Mounter starts and stops per-project workspace mounts.
type Mounter interface {
	Mount(target, mountpoint string, store *policy.Store) (MountServer, error)
}

type mount struct {
	projectRoot string
	mountPoint  string
	server      MountServer
}

// MountManager owns the live workspace mounts. It holds no
// authoritative refcount: mount and unmount are idempotent, and the
// caller (the harness) owns the per-project active-turn count. A crash
// therefore cannot leave a daemon-side count that blocks unmount.
type MountManager struct {
	mounter   Mounter
	workspace string

	mu     sync.Mutex
	mounts map[string]*mount // keyed by cleaned project root
}

// NewMountManager returns a manager mounting under workspace.
func NewMountManager(mounter Mounter, workspace string) *MountManager {
	return &MountManager{
		mounter:   mounter,
		workspace: workspace,
		mounts:    map[string]*mount{},
	}
}

// ProjectHash is the stable mount directory name for a project root.
func ProjectHash(projectRoot string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(projectRoot)))
	return hex.EncodeToString(sum[:])[:16]
}

// Mountpoint is the workspace path a project root mounts at.
func (m *MountManager) Mountpoint(projectRoot string) string {
	return filepath.Join(m.workspace, ProjectHash(projectRoot))
}

// Mount mounts projectRoot's workspace if it is not already mounted and
// returns its mount point. It is idempotent.
func (m *MountManager) Mount(projectRoot string, store *policy.Store) (string, error) {
	key := filepath.Clean(projectRoot)

	m.mu.Lock()
	defer m.mu.Unlock()
	if ex, ok := m.mounts[key]; ok {
		return ex.mountPoint, nil
	}
	mp := filepath.Join(m.workspace, ProjectHash(key))
	srv, err := m.mounter.Mount(key, mp, store)
	if err != nil {
		return "", err
	}
	m.mounts[key] = &mount{projectRoot: key, mountPoint: mp, server: srv}
	return mp, nil
}

// Unmount releases projectRoot's mount if present. It is idempotent.
func (m *MountManager) Unmount(projectRoot string) error {
	key := filepath.Clean(projectRoot)

	m.mu.Lock()
	defer m.mu.Unlock()
	ex, ok := m.mounts[key]
	if !ok {
		return nil
	}
	if err := ex.server.Unmount(); err != nil {
		return err
	}
	delete(m.mounts, key)
	return nil
}

// OpenMounts is the number of currently mounted projects, for GET /health.
func (m *MountManager) OpenMounts() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.mounts)
}
