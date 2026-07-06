package jig

import (
	"reflect"
	"testing"
)

func TestResolveDependenciesRecursiveAndOptional(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "services/checkout": {
      "$repo": {
        "git": "git@example.com:checkout.git",
        "dependsOn": [
          { "path": "platform" },
          { "path": "observability", "optional": true }
        ]
      }
    },
    "platform/auth": {
      "$repo": {
        "git": "git@example.com:auth.git",
        "dependsOn": [{ "path": "shared/crypto" }]
      }
    },
    "platform/billing": {
      "$repo": { "git": "git@example.com:billing.git" }
    },
    "shared/crypto": {
      "$repo": { "git": "git@example.com:crypto.git" }
    },
    "observability/tracing": {
      "$repo": { "git": "git@example.com:tracing.git" }
    }
  }
}`)
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := resolvePlan(&model, []string{"services/checkout"}, planOptions{IncludeRoots: false})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"platform/auth", "platform/billing", "shared/crypto"}
	if !reflect.DeepEqual(plan.Repos, want) {
		t.Fatalf("deps without optional = %#v, want %#v", plan.Repos, want)
	}

	plan, err = resolvePlan(&model, []string{"services/checkout"}, planOptions{IncludeRoots: false, IncludeOptional: true})
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"observability/tracing", "platform/auth", "platform/billing", "shared/crypto"}
	if !reflect.DeepEqual(plan.Repos, want) {
		t.Fatalf("deps with optional = %#v, want %#v", plan.Repos, want)
	}
}

func TestResolveDependenciesDoesNotIncludeRootInCycle(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "service/a": {
      "$repo": {
        "git": "git@example.com:a.git",
        "dependsOn": [{ "path": "service/b" }]
      }
    },
    "service/b": {
      "$repo": {
        "git": "git@example.com:b.git",
        "dependsOn": [{ "path": "service/a" }]
      }
    }
  }
}`)
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := resolvePlan(&model, []string{"service/a"}, planOptions{IncludeRoots: false})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"service/b"}
	if !reflect.DeepEqual(plan.Repos, want) {
		t.Fatalf("deps with cycle = %#v, want %#v", plan.Repos, want)
	}
}

func TestResolvePlanForGroupDeduplicatesDependencies(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "services/checkout": {
      "$repo": {
        "git": "git@example.com:checkout.git",
        "dependsOn": [{ "path": "platform/auth" }]
      }
    },
    "services/cart": {
      "$repo": {
        "git": "git@example.com:cart.git",
        "dependsOn": [{ "path": "platform/auth" }]
      }
    },
    "platform/auth": {
      "$repo": {
        "id": "auth-service",
        "git": "git@example.com:auth.git"
      }
    }
  }
}`)
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := model.Select(NodeQuery{Path: "services", IncludeArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	roots := selection.repoPaths()

	plan, err := resolvePlan(&model, roots, planOptions{IncludeRoots: false})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"platform/auth"}
	if !reflect.DeepEqual(plan.Repos, want) {
		t.Fatalf("group deps = %#v, want %#v", plan.Repos, want)
	}

	plan, err = resolvePlan(&model, roots, planOptions{IncludeRoots: true})
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"platform/auth", "services/cart", "services/checkout"}
	if !reflect.DeepEqual(plan.Repos, want) {
		t.Fatalf("group clone set = %#v, want %#v", plan.Repos, want)
	}
}

func TestCloneAllRootsIncludeAllRepositories(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "platform/auth": {
      "$repo": { "git": "git@example.com:auth.git" }
    },
    "services/checkout": {
      "$repo": { "git": "git@example.com:checkout.git" }
    }
  }
}`)
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}
	roots := sortedRepoPaths(&model)
	plan, err := resolvePlan(&model, roots, planOptions{IncludeRoots: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"platform/auth", "services/checkout"}
	if !reflect.DeepEqual(plan.Repos, want) {
		t.Fatalf("clone-all repos = %#v, want %#v", plan.Repos, want)
	}
}

func TestResolvePlanSkipsArchivedReposUnlessIncluded(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "services/active": {
      "$repo": {
        "git": "git@example.com:active.git",
        "dependsOn": [{ "path": "platform" }]
      }
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
    "platform/legacy": {
      "$repo": {
        "git": "git@example.com:legacy.git",
        "archived": true
      }
    }
  }
}`)
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}
	roots := sortedRepoPaths(&model)

	plan, err := resolvePlan(&model, roots, planOptions{IncludeRoots: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"platform/auth", "services/active"}
	if !reflect.DeepEqual(plan.Repos, want) {
		t.Fatalf("without archived = %#v, want %#v", plan.Repos, want)
	}

	plan, err = resolvePlan(&model, roots, planOptions{IncludeArchived: true, IncludeRoots: true})
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"platform/auth", "platform/legacy", "services/active", "services/old"}
	if !reflect.DeepEqual(plan.Repos, want) {
		t.Fatalf("with archived = %#v, want %#v", plan.Repos, want)
	}

	plan, err = resolvePlan(&model, roots, planOptions{
		IncludeRoots: true,
		Installed: map[string]bool{
			"services/old":    true,
			"platform/legacy": true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Repos, want) {
		t.Fatalf("with installed archived = %#v, want %#v", plan.Repos, want)
	}

	plan, err = resolvePlan(&model, []string{"services/active"}, planOptions{
		IncludeRoots: true,
		Installed:    map[string]bool{"platform/legacy": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"platform/auth", "platform/legacy", "services/active"}
	if !reflect.DeepEqual(plan.Repos, want) {
		t.Fatalf("with installed archived dependency = %#v, want %#v", plan.Repos, want)
	}
}

func TestArchivedFilesAreSkippedUnlessIncluded(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "scripts/current.sh": {
      "$file": { "src": "git:git@example.com:config.git#scripts/current.sh" }
    },
    "scripts/old.sh": {
      "$file": {
        "src": "git:git@example.com:config.git#scripts/old.sh",
        "archived": true
      }
    },
    "bin/old": {
      "$file": { "link": "scripts/old.sh" }
    }
  }
}`)
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolvePlan(&model, []string{}, planOptions{})
	if err != nil {
		t.Fatal(err)
	}
	resolved = includeExplicitFiles(&model, resolved, sortedFilePaths(&model))
	resolved = excludeArchivedFiles(&model, resolved, nil)
	if !reflect.DeepEqual(resolved.Files, []string{"scripts/current.sh"}) {
		t.Fatalf("without archived files = %#v", resolved.Files)
	}
	resolved = plan{}
	resolved = includeExplicitFiles(&model, resolved, sortedFilePaths(&model))
	if !reflect.DeepEqual(resolved.Files, []string{"scripts/old.sh", "bin/old", "scripts/current.sh"}) {
		t.Fatalf("with archived files = %#v", resolved.Files)
	}
	resolved = excludeArchivedFiles(&model, resolved, map[string]bool{"scripts/old.sh": true})
	if !reflect.DeepEqual(resolved.Files, []string{"scripts/old.sh", "bin/old", "scripts/current.sh"}) {
		t.Fatalf("with installed archived files = %#v", resolved.Files)
	}

	// With no repositories in the schema and nothing selected, only the
	// installed file stays active: state records intent.
	resolved, err = resolvePlan(&model, nil, planOptions{
		InstalledFiles: map[string]bool{"scripts/old.sh": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolved.Files, []string{"scripts/old.sh"}) {
		t.Fatalf("planned installed archived files = %#v", resolved.Files)
	}
}

func TestResolvePlanIncludesInstalledOptionalDependencyForSync(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "services/checkout": {
      "$repo": {
        "git": "git@example.com:checkout.git",
        "dependsOn": [{ "path": "observability/tracing", "optional": true }]
      }
    },
    "observability/tracing": {
      "$repo": {
        "id": "tracing-service",
        "git": "git@example.com:tracing.git"
      }
    }
  }
}`)
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}
	installed := map[string]bool{"tracing-service": true}

	plan, err := resolvePlan(&model, []string{"services/checkout"}, planOptions{IncludeRoots: true, IncludeInstalledOptional: true, Installed: installed})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"observability/tracing", "services/checkout"}
	if !reflect.DeepEqual(plan.Repos, want) {
		t.Fatalf("sync set = %#v, want %#v", plan.Repos, want)
	}
}

func TestResolvePlanActivatesOnlyWhenFileAndRepo(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "platform/auth": {
      "$repo": {
        "id": "auth-service",
        "git": "git@example.com:auth.git"
      }
    },
    "tools/platform-debug": {
      "$repo": {
        "id": "platform-debug-tools",
        "git": "git@example.com:debug.git",
        "onlyWhen": { "path": "platform" }
      }
    },
    ".agents/skills/platform": {
      "$file": {
        "id": "platform-skill",
        "src": "git:git@example.com:config.git#agents/skills/platform.md",
        "onlyWhen": { "path": "platform" }
      }
    }
  }
}`)
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := resolvePlan(&model, []string{"platform/auth"}, planOptions{IncludeRoots: true})
	if err != nil {
		t.Fatal(err)
	}
	wantRepos := []string{"platform/auth", "tools/platform-debug"}
	if !reflect.DeepEqual(plan.Repos, wantRepos) {
		t.Fatalf("onlyWhen repos = %#v, want %#v", plan.Repos, wantRepos)
	}
	wantFiles := []string{".agents/skills/platform"}
	if !reflect.DeepEqual(plan.Files, wantFiles) {
		t.Fatalf("onlyWhen files = %#v, want %#v", plan.Files, wantFiles)
	}
}

func TestIncludeExplicitFilesAddsRequestedFilesAndLinkTargets(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "scripts/dev.sh": {
      "$file": {
        "id": "dev-script",
        "src": "git:git@example.com:config.git#scripts/dev.sh"
      }
    },
    "bin/dev": {
      "$file": {
        "id": "dev-command",
        "link": "scripts/dev.sh"
      }
    }
  }
}`)
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}
	plan := includeExplicitFiles(&model, plan{}, []string{"bin/dev"})
	want := []string{"scripts/dev.sh", "bin/dev"}
	if !reflect.DeepEqual(plan.Files, want) {
		t.Fatalf("files = %#v, want %#v", plan.Files, want)
	}
}
