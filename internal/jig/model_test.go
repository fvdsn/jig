package jig

import (
	"reflect"
	"testing"
)

func TestFlattenDefinitionWithSlashShorthandAndFile(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "platform/auth": {
      "$repo": {
        "id": "auth-service",
        "git": "git@example.com:auth.git"
      }
    },
    "scripts": {
      "dev.sh": {
        "$file": {
          "id": "dev-script",
          "src": "git:git@example.com:config.git#scripts/dev.sh",
          "executable": true
        }
      }
    }
  }
}`)

	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}
	if entry, ok := model.entry("platform/auth", EntryRepo); !ok || entry.Identity != "auth-service" {
		t.Fatal("missing platform/auth repo")
	}
	if entry, ok := model.entry("scripts/dev.sh", EntryFile); !ok || entry.Identity != "dev-script" {
		t.Fatal("missing scripts/dev.sh file")
	}
}

func TestFlattenDefinitionAssignsGroupIdentities(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "platform": {
      "$group": { "id": "platform-group" }
    },
    "services": {
      "$group": {}
    }
  }
}`)
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}
	platform, ok := model.entry("platform", EntryGroup)
	if !ok || platform.Identity != "platform-group" {
		t.Fatalf("platform group = %#v", platform)
	}
	services, ok := model.entry("services", EntryGroup)
	if !ok || services.Identity != "services" {
		t.Fatalf("services group = %#v", services)
	}
}

func TestGroupArchivedIsInheritedByReposAndFiles(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "legacy": {
      "$group": { "archived": true },
      "service": {
        "$repo": { "git": "git@example.com:service.git" }
      },
      "README.md": {
        "$file": { "src": "git:git@example.com:config.git#README.md" }
      }
    }
  }
}`)
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}
	group, _ := model.entry("legacy", EntryGroup)
	repo, _ := model.entry("legacy/service", EntryRepo)
	file, _ := model.entry("legacy/README.md", EntryFile)
	if !group.Group.Archived {
		t.Fatal("group is not archived")
	}
	if !repo.Repo.Archived {
		t.Fatal("repo did not inherit archived")
	}
	if !file.File.Archived {
		t.Fatal("file did not inherit archived")
	}
}

func TestGroupInheritanceAppliesMetadataAndDependencies(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "shared/config": {
      "$repo": { "git": "git@example.com:config.git" }
    },
    "platform": {
      "$group": {
        "description": "Platform services",
        "web": "https://github.com/acme/platform",
        "dependsOn": [{ "path": "shared/config", "reason": "shared config" }]
      },
      "auth": {
        "$repo": {
          "id": "auth-service",
          "git": "git@example.com:auth.git"
        }
      }
    }
  }
}`)
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := model.entry("platform/auth", EntryRepo)
	if entry.Repo.Description != "Platform services" {
		t.Fatalf("description = %q", entry.Repo.Description)
	}
	if entry.Repo.Web != "https://github.com/acme/platform" {
		t.Fatalf("web = %q", entry.Repo.Web)
	}
	if len(entry.Repo.DependsOn) != 1 || entry.Repo.DependsOn[0].Path != "shared/config" {
		t.Fatalf("dependsOn = %#v", entry.Repo.DependsOn)
	}

	plan, err := resolvePlan(&model, []string{"platform/auth"}, planOptions{IncludeRoots: false})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Repos, []string{"shared/config"}) {
		t.Fatalf("deps = %#v", plan.Repos)
	}
}

func TestGroupOnlyWhenIsInheritedByReposAndFiles(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "platform/auth": {
      "$repo": { "git": "git@example.com:auth.git" }
    },
    ".agents/skills": {
      "$group": {
        "onlyWhen": { "path": "platform" }
      },
      "platform": {
        "$file": {
          "id": "platform-skill",
          "src": "git:git@example.com:config.git#agents/skills/platform.md"
        }
      }
    }
  }
}`)
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := model.entry(".agents/skills/platform", EntryFile)
	if len(entry.Conditions) != 1 || entry.Conditions[0].Path != "platform" {
		t.Fatalf("conditions = %#v", entry.Conditions)
	}

	plan, err := resolvePlan(&model, []string{"platform/auth"}, planOptions{IncludeRoots: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Files, []string{".agents/skills/platform"}) {
		t.Fatalf("files = %#v", plan.Files)
	}
}
