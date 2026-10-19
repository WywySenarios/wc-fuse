package fusefs

import "testing"

func TestContained(t *testing.T) {
	cases := []struct {
		dir, target string
		want        bool
	}{
		{"", "file", true},
		{"", "dir/file", true},
		{"dir", "../other", true}, // still inside root
		{"", "../escape", false},
		{"dir", "../../escape", false},
		{"dir", "/etc/passwd", false}, // absolute target leaves the namespace
		{"a/b", "../c/d", true},       // a/c/d
		{"a/b", "../../../../etc/x", false},
	}
	for _, c := range cases {
		if got := contained(c.dir, c.target); got != c.want {
			t.Errorf("contained(%q, %q) = %v, want %v", c.dir, c.target, got, c.want)
		}
	}
}
