package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
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
	mountPoint string
	server     MountServer
	sessions   map[string]struct{} // session IDs holding the mount; guarded by the root mounting lock
}

// MountManager describes live mounts.
type MountManager struct {
	mounter   Mounter
	workspace string

	mu     sync.Mutex             // MountManager lock
	mounts map[string]*mount      // keyed by (cleaned) project root
	locks  map[string]*sync.Mutex // mounting locks, keyed by (cleaned) project root
}

// NewMountManager returns a manager mounting under workspace.
func NewMountManager(mounter Mounter, workspace string) *MountManager {
	return &MountManager{
		mounter:   mounter,
		workspace: workspace,
		mounts:    map[string]*mount{},
		locks:     map[string]*sync.Mutex{},
	}
}

// lockFor returns the mounting lock for key and creates it if it does not already exist.
// It will try to obtain the mounter manager lock for reads and writes.
func (m *MountManager) lockFor(key string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	lock, ok := m.locks[key]
	if !ok {
		lock = &sync.Mutex{}
		m.locks[key] = lock
	}
	return lock
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

// Mount mounts projectRoot's workspace if it is not already mounted and returns its mount point.
// This method is effectively idempotent.
func (m *MountManager) Mount(sessionID, projectRoot string, store *policy.Store) (string, error) {
	key := filepath.Clean(projectRoot)

	lock := m.lockFor(key)
	lock.Lock()
	defer lock.Unlock()

	m.mu.Lock()
	existing, ok := m.mounts[key]
	m.mu.Unlock()
	if ok {
		existing.sessions[sessionID] = struct{}{}
		return existing.mountPoint, nil
	}

	mp := m.Mountpoint(key)
	if err := os.MkdirAll(mp, 0o711); err != nil {
		return "", fmt.Errorf("create mount point %s: %w", mp, err)
	}
	srv, err := m.mounter.Mount(key, mp, store)
	if err != nil {
		return "", err
	}

	m.mu.Lock()
	m.mounts[key] = &mount{
		mountPoint: mp,
		server:     srv,
		sessions:   map[string]struct{}{sessionID: {}},
	}
	m.mu.Unlock()
	return mp, nil
}

// Unmount releases sessionID's hold on projectRoot.
// It will also tear down the mount if no one is holding onto the projectRoot anymore.
// An empty sessionID will be interpretted as a client panic and will force teardown regardless of the remaining holders.
// This method is idempotent (i.e. Unmount on a non-existent mount will not do anything).
func (m *MountManager) Unmount(sessionID, projectRoot string) error {
	key := filepath.Clean(projectRoot)

	// A missing lock entry means Mount was never called for this root;
	// return without creating one.
	m.mu.Lock()
	lock, ok := m.locks[key]
	m.mu.Unlock()
	if !ok {
		return nil
	}

	lock.Lock()
	defer lock.Unlock()

	m.mu.Lock()
	existing, ok := m.mounts[key]
	m.mu.Unlock()
	if !ok {
		return nil
	}

	if sessionID != "" {
		if _, held := existing.sessions[sessionID]; !held {
			return nil
		}
		if len(existing.sessions) > 1 {
			delete(existing.sessions, sessionID)
			return nil
		}
	}

	if err := existing.server.Unmount(); err != nil {
		return err
	}

	m.mu.Lock()
	delete(m.mounts, key)
	m.mu.Unlock()
	return nil
}

// Get the number of unique mounts currently open.
func (m *MountManager) OpenMounts() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.mounts)
}
