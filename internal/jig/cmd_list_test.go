package jig

import (
	"bytes"
	"os"
	"testing"
)

func TestListSupportsPathAndArchivedFlag(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspace(t, root, `{
  "version": 1,
  "tree": {
    "services": {
      "$group": {
        "id": "service-group",
        "description": "Services"
      }
    },
    "services/current": {
      "$repo": { "git": "git@example.com:current.git" }
    },
    "services/old": {
      "$repo": {
        "git": "git@example.com:old.git",
        "archived": true
      }
    },
    "platform/auth": {
      "$repo": { "git": "git@example.com:auth.git" }
    },
    "services/scripts/current.sh": {
      "$file": { "src": "git:git@example.com:config.git#scripts/current.sh" }
    },
    "services/scripts/old.sh": {
      "$file": {
        "src": "git:git@example.com:config.git#scripts/old.sh",
        "archived": true
      }
    }
  }
}`)

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatal(err)
		}
	}()

	var out bytes.Buffer
	if err := List(ListOptions{Path: "services/", Width: -1}, &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	// Children inherit the group description through flat slash keys.
	want := "group services\tServices\n" +
		"repo  services/current\tServices\n" +
		"file  services/scripts/current.sh\tServices\n"
	if got != want {
		t.Fatalf("list output:\n%s\nwant:\n%s", got, want)
	}

	out.Reset()
	if err := List(ListOptions{Path: "services/", IncludeArchived: true, Width: -1}, &out); err != nil {
		t.Fatal(err)
	}
	got = out.String()
	want = "group services\tServices\n" +
		"repo  services/current\tServices\n" +
		"repo  services/old\tServices\n" +
		"file  services/scripts/current.sh\tServices\n" +
		"file  services/scripts/old.sh\tServices\n"
	if got != want {
		t.Fatalf("archived list output:\n%s\nwant:\n%s", got, want)
	}
}

func TestListTruncatesDescriptionsForTerminals(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspace(t, root, `{
  "version": 1,
  "tree": {
    "svc/a": {
      "$repo": { "git": "git@example.com:a.git", "description": "This is a very long description that would wrap around the terminal and make the listing unreadable for everyone" }
    },
    "svc/blob-storage": {
      "$repo": { "git": "git@example.com:b.git", "description": "Short" }
    }
  }
}`)

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatal(err)
		}
	}()

	var out bytes.Buffer
	if err := List(ListOptions{Width: 60}, &out); err != nil {
		t.Fatal(err)
	}
	want := "repo  svc/a             This is a very long description tha…\n" +
		"repo  svc/blob-storage  Short\n"
	if got := out.String(); got != want {
		t.Fatalf("list output:\n%q\nwant:\n%q", got, want)
	}
}
