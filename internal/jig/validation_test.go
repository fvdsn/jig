package jig

import "testing"

func TestValidateDefinitionDuplicateIdentity(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
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
  "version": 1,
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
  "version": 1,
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
  "version": 1,
  "tree": {
    "bin/dev": {
      "$file": {
        "id": "dev-command",
        "link": "scripts/dev.sh"
      }
    }
  }
}`)
	result := validateDefinition(def)
	if len(result.Errors) == 0 {
		t.Fatal("expected validation error")
	}
}
