// Package fusefs is WywyCode's host-side FUSE passthrough filesystem.
//
// It exposes a project directory through a mounted workspace and
// enforces the host rwx policy on every namespace-changing operation.
// The backing store is the real project directory, so the developer
// and the tool see the exact same files.
package fusefs

import (
	"context"
	"fmt"
	"path"
	"strings"
	"syscall"

	"github.com/WywySenarios/wc-fuse/policy"
	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

// umask is applied to modes passed to Create and Mkdir so
// tool-created files never gain group/other write.
const umask = 0o022

// Option adjusts the mount options.
type Option func(*fuse.MountOptions)

// WithAllowOther sets the allow_other mount option.
func WithAllowOther(v bool) Option {
	return func(o *fuse.MountOptions) { o.AllowOther = v }
}

// WithDebug sets the FUSE debug flag.
func WithDebug(v bool) Option {
	return func(o *fuse.MountOptions) { o.Debug = v }
}

// Mount starts a policy-enforcing passthrough filesystem backing on
// target at mountpoint. Stop it with the returned Server's Unmount.
//
// allow_other is on by default: the pod runs as a different uid than
// the daemon, so the mount must be reachable by other users.
func Mount(target, mountpoint string, store *policy.Store, opts ...Option) (*fuse.Server, error) {
	root, err := fs.NewLoopbackRoot(target)
	if err != nil {
		return nil, err
	}
	lb, ok := root.(*fs.LoopbackNode)
	if !ok {
		return nil, fmt.Errorf("fusefs: unexpected loopback root %T", root)
	}
	// Resolve paths relative to the mounted root. Once wrapped, the
	// loopback's own root node is no longer the tree root.
	lb.RootData.RootNode = nil

	mopts := fuse.MountOptions{AllowOther: true, Name: "wc-fuse"}
	for _, o := range opts {
		o(&mopts)
	}
	return fs.Mount(mountpoint, &policyNode{LoopbackNode: lb, store: store}, &fs.Options{MountOptions: mopts})
}

// policyNode wraps a loopback node and vetoes operations the host
// policy forbids. Operations it does not override are promoted from
// the embedded loopback node.
type policyNode struct {
	*fs.LoopbackNode
	store *policy.Store
}

var _ fs.NodeWrapChilder = (*policyNode)(nil)

// WrapChild wraps every child created under a policy node, so the
// policy applies across the whole tree.
func (n *policyNode) WrapChild(_ context.Context, ops fs.InodeEmbedder) fs.InodeEmbedder {
	lb, ok := ops.(*fs.LoopbackNode)
	if !ok {
		return ops
	}
	return &policyNode{LoopbackNode: lb, store: n.store}
}

// rel is this node's path relative to the workspace root.
func (n *policyNode) rel() string { return n.Path(n.Root()) }

func (n *policyNode) child(name string) string { return path.Join(n.rel(), name) }

func (n *policyNode) allow(rel string, m policy.Mode) syscall.Errno {
	if n.store.Current().Allowed(rel, m) {
		return 0
	}
	return syscall.EACCES
}

var _ fs.NodeOpener = (*policyNode)(nil)

func (n *policyNode) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	m := policy.Read
	if flags&(syscall.O_WRONLY|syscall.O_RDWR|syscall.O_TRUNC|syscall.O_APPEND) != 0 {
		m = policy.Write
	}
	if e := n.allow(n.rel(), m); e != 0 {
		return nil, 0, e
	}
	return n.LoopbackNode.Open(ctx, flags)
}

var _ fs.NodeCreater = (*policyNode)(nil)

func (n *policyNode) Create(ctx context.Context, name string, flags, mode uint32, out *fuse.EntryOut) (*fs.Inode, fs.FileHandle, uint32, syscall.Errno) {
	if e := n.allow(n.child(name), policy.Write); e != 0 {
		return nil, nil, 0, e
	}
	return n.LoopbackNode.Create(ctx, name, flags, mode&^umask, out)
}

var _ fs.NodeMkdirer = (*policyNode)(nil)

func (n *policyNode) Mkdir(ctx context.Context, name string, mode uint32, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	if e := n.allow(n.child(name), policy.Write); e != 0 {
		return nil, e
	}
	return n.LoopbackNode.Mkdir(ctx, name, mode&^umask, out)
}

var _ fs.NodeRmdirer = (*policyNode)(nil)

func (n *policyNode) Rmdir(ctx context.Context, name string) syscall.Errno {
	if e := n.allow(n.child(name), policy.Write); e != 0 {
		return e
	}
	return n.LoopbackNode.Rmdir(ctx, name)
}

var _ fs.NodeUnlinker = (*policyNode)(nil)

func (n *policyNode) Unlink(ctx context.Context, name string) syscall.Errno {
	if e := n.allow(n.child(name), policy.Write); e != 0 {
		return e
	}
	return n.LoopbackNode.Unlink(ctx, name)
}

var _ fs.NodeRenamer = (*policyNode)(nil)

func (n *policyNode) Rename(ctx context.Context, name string, newParent fs.InodeEmbedder, newName string, flags uint32) syscall.Errno {
	dp := newParent.EmbeddedInode()
	dst := path.Join(dp.Path(dp.Root()), newName)
	if e := n.allow(n.child(name), policy.Write); e != 0 {
		return e
	}
	if e := n.allow(dst, policy.Write); e != 0 {
		return e
	}
	return n.LoopbackNode.Rename(ctx, name, newParent, newName, flags)
}

var _ fs.NodeSymlinker = (*policyNode)(nil)

func (n *policyNode) Symlink(ctx context.Context, target, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	if e := n.allow(n.child(name), policy.Write); e != 0 {
		return nil, e
	}
	if !contained(n.rel(), target) {
		return nil, syscall.EACCES
	}
	return n.LoopbackNode.Symlink(ctx, target, name, out)
}

var _ fs.NodeLinker = (*policyNode)(nil)

func (n *policyNode) Link(ctx context.Context, target fs.InodeEmbedder, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	if e := n.allow(n.child(name), policy.Write); e != 0 {
		return nil, e
	}
	return n.LoopbackNode.Link(ctx, target, name, out)
}

var _ fs.NodeReadlinker = (*policyNode)(nil)

func (n *policyNode) Readlink(ctx context.Context) ([]byte, syscall.Errno) {
	if e := n.allow(n.rel(), policy.Read); e != 0 {
		return nil, e
	}
	return n.LoopbackNode.Readlink(ctx)
}

var _ fs.NodeSetattrer = (*policyNode)(nil)

func (n *policyNode) Setattr(ctx context.Context, f fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	// Metadata changes are denied for tools by default.
	if _, ok := in.GetMode(); ok {
		return syscall.EPERM
	}
	if _, ok := in.GetUID(); ok {
		return syscall.EPERM
	}
	if _, ok := in.GetGID(); ok {
		return syscall.EPERM
	}
	if _, ok := in.GetSize(); ok {
		if e := n.allow(n.rel(), policy.Write); e != 0 {
			return e
		}
	}
	return n.LoopbackNode.Setattr(ctx, f, in, out)
}

// contained reports whether a symlink target, resolved against dir,
// stays inside the workspace root. Absolute targets are rejected: they
// name a path outside the workspace namespace (fail closed).
func contained(dir, target string) bool {
	if strings.HasPrefix(target, "/") {
		return false
	}
	joined := path.Clean(path.Join(dir, target))
	return joined != ".." && !strings.HasPrefix(joined, "../")
}
