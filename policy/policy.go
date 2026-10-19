// Package policy is WywyCode's host-side filesystem policy engine.
package policy

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Mode is a requested filesystem operation.
type Mode int

const (
	// Read is a read access.
	Read Mode = iota
	// Write is a write access.
	Write
	// Execute is a directory traversal / execute access.
	Execute
)

func (m Mode) String() string {
	switch m {
	case Read:
		return "read"
	case Write:
		return "write"
	case Execute:
		return "execute"
	default:
		panic(fmt.Sprintf("policy: invalid Mode %d", int(m)))
	}
}

// Default permission bits.
const (
	defaultRead    = false
	defaultWrite   = false
	defaultExecute = false
)

// Entry is one per-path rwx rule.
type Entry struct {
	Path    string `yaml:"path"`
	Read    *bool  `yaml:"read"`
	Write   *bool  `yaml:"write"`
	Execute *bool  `yaml:"execute"`
}

func (e Entry) allows(m Mode) bool {
	switch m {
	case Read:
		if e.Read != nil {
			return *e.Read
		}
		return defaultRead
	case Write:
		if e.Write != nil {
			return *e.Write
		}
		return defaultWrite
	case Execute:
		if e.Execute != nil {
			return *e.Execute
		}
		return defaultExecute
	default:
		return false
	}
}

// Policy is a loaded filesystem policy.
type Policy struct {
	targetHome   string
	entries      []Entry
	projectRoots []string
}

// TargetHome is the in-pod workspace root the mount is exposed at.
func (p *Policy) TargetHome() string { return p.targetHome }

// ProjectRoots returns the allowed project roots, in file order.
func (p *Policy) ProjectRoots() []string {
	out := make([]string, len(p.projectRoots))
	copy(out, p.projectRoots)
	return out
}

// Load reads fs.yaml and project-roots.yaml from dir.
// We shut the FUSE closed if configuration is missing.
func Load(dir string) (*Policy, error) {
	fsPath := filepath.Join(dir, "fs.yaml")
	raw, err := os.ReadFile(fsPath)
	if err != nil {
		return nil, fmt.Errorf("policy: read %s: %w", fsPath, err)
	}
	var fsCfg struct {
		TargetHome  string `yaml:"target_home"`
		Permissions []struct {
			Path    *string `yaml:"path"`
			Read    *bool   `yaml:"read"`
			Write   *bool   `yaml:"write"`
			Execute *bool   `yaml:"execute"`
		} `yaml:"permissions"`
	}
	if err := yaml.Unmarshal(raw, &fsCfg); err != nil {
		return nil, fmt.Errorf("policy: parse %s: %w", fsPath, err)
	}

	entries := make([]Entry, 0, len(fsCfg.Permissions))
	for i, e := range fsCfg.Permissions {
		if e.Path == nil {
			return nil, fmt.Errorf("policy: permissions[%d] is missing the required %q key (use path: %q for an explicit catch-all)", i, "path", "")
		}
		if err := validateRulePath(*e.Path); err != nil {
			return nil, fmt.Errorf("policy: permissions[%d].path %q: %w", i, *e.Path, err)
		}
		entries = append(entries, Entry{Path: *e.Path, Read: e.Read, Write: e.Write, Execute: e.Execute})
	}

	p := &Policy{targetHome: fsCfg.TargetHome, entries: entries}

	prPath := filepath.Join(dir, "project-roots.yaml")
	raw, err = os.ReadFile(prPath)
	if err != nil {
		if os.IsNotExist(err) {
			return p, nil
		}
		return nil, fmt.Errorf("policy: read %s: %w", prPath, err)
	}
	var prCfg struct {
		ProjectRoots []string `yaml:"project_roots"`
	}
	if err := yaml.Unmarshal(raw, &prCfg); err != nil {
		return nil, fmt.Errorf("policy: parse %s: %w", prPath, err)
	}
	for _, r := range prCfg.ProjectRoots {
		if !filepath.IsAbs(r) {
			return nil, fmt.Errorf("policy: project root %q must be an absolute path", r)
		}
	}
	p.projectRoots = prCfg.ProjectRoots
	return p, nil
}

// Allowed reports whether an operation relative to the workspace root is permitted.
func (p *Policy) Allowed(rel string, m Mode) bool {
	rel = normalizeWorkspaceRelativePath(rel)
	for _, e := range p.entries {
		ep := normalizeWorkspaceRelativePath(e.Path)
		if ep == "" || rel == ep || strings.HasPrefix(rel, ep+"/") {
			return e.allows(m) // first match wins
		}
	}
	return Entry{}.allows(m)
}

// AllowsProjectRoot reports whether root is at or beneath one of the configured project roots.
func (p *Policy) AllowsProjectRoot(root string) bool {
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) {
		return false
	}
	sep := string(os.PathSeparator)
	for _, allowed := range p.projectRoots {
		allowed = filepath.Clean(allowed)
		if root == allowed || strings.HasPrefix(root, allowed+sep) {
			return true
		}
	}
	return false
}

// The input path must be a relative path.
func normalizeWorkspaceRelativePath(p string) string {
	p = strings.TrimSpace(p)
	if filepath.IsAbs(p) {
		panic(fmt.Sprintf("%q must be a relative path", p))
	}
	p = path.Clean(p)
	if p == "." {
		return ""
	}
	return p
}

// validateRulePath checks a raw rule path.
// The empty string is the explicit catch-all and is allowed. No other path may resolve to an empty string.
// No path may be absolute or escape the root.
func validateRulePath(raw string) error {
	if raw == "" {
		return nil
	}
	if normalizeWorkspaceRelativePath(raw) == "" {
		return fmt.Errorf("resolves to the empty path. use an explicit %q for a catch-all", "")
	}
	if !safeRel(raw) {
		return fmt.Errorf("must be relative and free of %q", "..")
	}
	return nil
}

// safeRel rejects entry paths that are absolute or escape the root.
func safeRel(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" {
		return true
	}
	if strings.HasPrefix(p, "/") {
		return false
	}
	for _, seg := range strings.Split(path.Clean(p), "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}
