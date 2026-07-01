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
	if _, ok := model.Repos["platform/auth"]; !ok {
		t.Fatal("missing platform/auth repo")
	}
	if _, ok := model.Files["scripts/dev.sh"]; !ok {
		t.Fatal("missing scripts/dev.sh file")
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
	if !model.Groups["legacy"].Group.Archived {
		t.Fatal("group is not archived")
	}
	if !model.Repos["legacy/service"].Repo.Archived {
		t.Fatal("repo did not inherit archived")
	}
	if !model.Files["legacy/README.md"].File.Archived {
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
	repo := model.Repos["platform/auth"].Repo
	if repo.Description != "Platform services" {
		t.Fatalf("description = %q", repo.Description)
	}
	if repo.Web != "https://github.com/acme/platform" {
		t.Fatalf("web = %q", repo.Web)
	}
	if len(repo.DependsOn) != 1 || repo.DependsOn[0].Path != "shared/config" {
		t.Fatalf("dependsOn = %#v", repo.DependsOn)
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
	entry := model.Files[".agents/skills/platform"]
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
