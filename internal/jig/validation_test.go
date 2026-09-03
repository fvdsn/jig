package jig

import (
	"strings"
	"testing"
)

func TestValidateDefinitionDuplicateIdentity(t *testing.T) {
	def := testDefinition(t, `{
  "version": 3,
  "tree": {
    "platform/auth": {
      "$repo": { "id": "auth-service", "git": "git@example.com:auth.git" }
    },
    "identity/auth-service": {
      "$repo": { "id": "auth-service", "git": "git@example.com:auth2.git" }
    }
  }
}`)

	result := validateDefinition(def)
	if len(result.Errors) == 0 {
		t.Fatal("expected validation error")
	}
}

func TestValidateDefinitionDuplicateGroupIdentity(t *testing.T) {
	def := testDefinition(t, `{
  "version": 3,
  "tree": {
    "platform": {
      "$group": { "id": "shared-group" }
    },
    "services": {
      "$group": { "id": "shared-group" }
    }
  }
}`)

	result := validateDefinition(def)
	if len(result.Errors) == 0 {
		t.Fatal("expected duplicate group identity validation error")
	}
}

func TestValidateDefinitionDependencyPathMustResolve(t *testing.T) {
	def := testDefinition(t, `{
  "version": 3,
  "tree": {
    "services/checkout": {
      "$repo": {
        "git": "git@example.com:checkout.git",
        "dependsOn": [{ "path": "missing" }]
      }
    }
  }
}`)

	result := validateDefinition(def)
	if len(result.Errors) == 0 {
		t.Fatal("expected validation error")
	}
}

func TestFileLinkRequiresDefinedTarget(t *testing.T) {
	def := testDefinition(t, `{
  "version": 3,
  "tree": {
    "bin/dev": {
      "$file": {
        "id": "dev-command",
        "link": {"path": "scripts/dev.sh"}
      }
    }
  }
}`)
	result := validateDefinition(def)
	if len(result.Errors) == 0 {
		t.Fatal("expected validation error")
	}
}

// A schema without a tree is reported once, by the flattening step, not
// again by a separate pre-check.
func TestValidateReportsMissingTreeOnce(t *testing.T) {
	result := validateDefinition(testDefinition(t, `{"version": 3}`))
	count := 0
	for _, msg := range result.Errors {
		if msg == "missing tree" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("missing tree reported %d times in %#v, want once", count, result.Errors)
	}
}

// A dependency cycle between repositories is a warning (setup still runs,
// in a best-effort order); a link cycle between files is an error, since
// no link could ever resolve.
func TestValidateDetectsCycles(t *testing.T) {
	result := validateDefinition(testDefinition(t, `{
  "version": 3,
  "tree": {
    "a": { "$repo": { "git": "git@example.com:a.git", "dependsOn": [{ "path": "b" }] } },
    "b": { "$repo": { "git": "git@example.com:b.git", "dependsOn": [{ "path": "a" }] } },
    "x.md": { "$file": { "link": { "path": "y.md" } } },
    "y.md": { "$file": { "link": { "path": "x.md" } } }
  }
}`))
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "dependency cycle detected: a -> b -> a") {
		t.Fatalf("warnings = %#v, want one dependency cycle", result.Warnings)
	}
	found := false
	for _, msg := range result.Errors {
		if strings.HasPrefix(msg, "file link cycle detected:") {
			found = true
		}
	}
	if !found {
		t.Fatalf("errors = %#v, want a file link cycle", result.Errors)
	}
}
