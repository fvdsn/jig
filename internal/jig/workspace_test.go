package jig

import "testing"

func TestResolvePath(t *testing.T) {
	cases := []struct {
		subdir string
		arg    string
		want   string
		err    bool
	}{
		{"", "", "", false},
		{"", "platform", "platform", false},
		{"", ".", "", false},
		{"", "..", "", true},
		{"codabox", "", "codabox", false},
		{"codabox", ".", "codabox", false},
		{"codabox", "cbix", "codabox/cbix", false},
		{"codabox", "./cbix", "codabox/cbix", false},
		{"codabox", "..", "", false},
		{"codabox/core", "..", "codabox", false},
		{"codabox/core", "../cbix", "codabox/cbix", false},
		{"codabox", "../..", "", true},
		{"codabox", "/platform", "platform", false},
		{"codabox", "/", "", false},
		{"codabox", "/..", "", true},
		{"codabox", "a/../b", "codabox/b", false},
	}
	for _, c := range cases {
		ws := &Workspace{Subdir: c.subdir}
		got, err := ws.ResolvePath(c.arg)
		if c.err {
			if err == nil {
				t.Errorf("subdir=%q arg=%q: expected error, got %q", c.subdir, c.arg, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("subdir=%q arg=%q = %q, %v; want %q", c.subdir, c.arg, got, err, c.want)
		}
	}
}

func TestWorkspaceSubdir(t *testing.T) {
	cases := []struct {
		root, cwd, want string
	}{
		{"/ws", "/ws", ""},
		{"/ws", "/ws/codabox", "codabox"},
		{"/ws", "/ws/codabox/core", "codabox/core"},
		{"/ws", "/ws/.jig", ""},
		{"/ws", "/ws/.jig/source", ""},
	}
	for _, c := range cases {
		got, err := workspaceSubdir(c.root, c.cwd)
		if err != nil || got != c.want {
			t.Errorf("workspaceSubdir(%q, %q) = %q, %v; want %q", c.root, c.cwd, got, err, c.want)
		}
	}
}
