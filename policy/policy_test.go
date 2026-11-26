package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func load(t *testing.T, fsYAML, rootsYAML string) *Policy {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "fs.yaml", fsYAML)
	if rootsYAML != "" {
		write(t, dir, "project-roots.yaml", rootsYAML)
	}
	p, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return p
}

func TestAllowedNoRuleDeniesAll(t *testing.T) {
	p := load(t, "target_home: /home/wc/workspace\npermissions: []\n", "")
	for _, m := range []Mode{Read, Write, Execute} {
		if p.Allowed("anything/at/all", m) {
			t.Errorf("%s must be denied when no rule matches (perms 000)", m)
		}
	}
}

func TestAllowedFirstMatchWins(t *testing.T) {
	p := load(t, `target_home: /home/wc/workspace
permissions:
  - path: src
    write: false
  - path: ""
    write: true
`, "")
	if p.Allowed("src/a.go", Write) {
		t.Error("src/a.go should hit the first (src) rule and be refused")
	}
	if !p.Allowed("lib/a.go", Write) {
		t.Error("lib/a.go should fall through to the catch-all and be allowed")
	}
}

func TestAllowedFirstMatchCatchAllShadowsLater(t *testing.T) {
	p := load(t, `target_home: /home/wc/workspace
permissions:
  - path: ""
    write: true
  - path: src
    write: false
`, "")
	if !p.Allowed("src/a.go", Write) {
		t.Error("an early catch-all must shadow a later specific rule (first match wins)")
	}
}

func TestAllowedEntryMatch(t *testing.T) {
	p := load(t, `target_home: /home/wc/workspace
permissions:
  - path: work
    write: true
    execute: true
`, "")
	if !p.Allowed("work/x", Write) {
		t.Error("work/x should be writable")
	}
	if !p.Allowed("work/x/", Write) {
		t.Error("a trailing slash should normalize")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("Allowed on an absolute path must panic")
			}
		}()
		p.Allowed("/work/x", Write)
	}()
	if p.Allowed("workshop/x", Write) {
		t.Error("workshop must not match the work prefix")
	}
	if !p.Allowed("work/x", Execute) {
		t.Error("work/x execute should be allowed")
	}
}

func TestAllowsProjectRoot(t *testing.T) {
	p := load(t, "target_home: /w\npermissions: []\n", "project_roots:\n  - /srv/wc/projects\n")
	cases := map[string]bool{
		"/srv/wc/projects":    true,
		"/srv/wc/projects/wc": true,
		"/srv/wc/projectsfoo": false,
		"/srv/wc":             false,
		"/etc":                false,
		"relative/path":       false,
	}
	for path, want := range cases {
		if got := p.AllowsProjectRoot(path); got != want {
			t.Errorf("AllowsProjectRoot(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestLoadMissingProjectRootsDeniesAll(t *testing.T) {
	p := load(t, "target_home: /w\npermissions: []\n", "")
	if p.AllowsProjectRoot("/anything") {
		t.Error("a missing project-roots file must deny every root (fail closed)")
	}
}

func TestLoadRequiresFSYAML(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(dir); err == nil {
		t.Error("Load without fs.yaml should fail")
	}
}

func TestLoadRejectsEscapingEntry(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "fs.yaml", "target_home: /w\npermissions:\n  - path: ../etc\n")
	if _, err := Load(dir); err == nil {
		t.Error("Load should reject an entry path containing ..")
	}
}

func TestLoadRequiresPathKey(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "fs.yaml", "target_home: /w\npermissions:\n  - read: true\n")
	if _, err := Load(dir); err == nil {
		t.Error("an entry without a path key must fail to load (only an explicit path: \"\" is a catch-all)")
	}
}

func TestLoadRejectsImplicitCatchAll(t *testing.T) {
	for _, raw := range []string{`"."`, `"   "`} {
		dir := t.TempDir()
		write(t, dir, "fs.yaml", "target_home: /w\npermissions:\n  - path: "+raw+"\n")
		if _, err := Load(dir); err == nil {
			t.Errorf("path: %s must be rejected; only an explicit path: \"\" is a catch-all", raw)
		}
	}
}

func TestLoadAllowsExplicitCatchAll(t *testing.T) {
	p := load(t, "target_home: /w\npermissions:\n  - path: \"\"\n    read: true\n", "")
	if !p.Allowed("any/path/at/all", Read) {
		t.Error("an explicit path: \"\" must match every path")
	}
}

func TestLoadRejectsRelativeProjectRoot(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "fs.yaml", "target_home: /w\npermissions: []\n")
	write(t, dir, "project-roots.yaml", "project_roots:\n  - relative\n")
	if _, err := Load(dir); err == nil {
		t.Error("Load should reject a relative project root")
	}
}
