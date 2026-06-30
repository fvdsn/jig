package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func testDefinition(t *testing.T, body string) *Definition {
	t.Helper()
	var def Definition
	if err := json.Unmarshal([]byte(body), &def); err != nil {
		t.Fatal(err)
	}
	return &def
}

func TestPathMatchesSegmentBoundary(t *testing.T) {
	tests := []struct {
		path string
		item string
		want bool
	}{
		{"platform", "platform/auth", true},
		{"platform", "platform", true},
		{"platform", "platforming/api", false},
		{"platform/auth", "platform/auth", true},
		{"platform/auth", "platform/auth/extra", true},
	}
	for _, test := range tests {
		if got := pathMatches(test.path, test.item); got != test.want {
			t.Fatalf("pathMatches(%q, %q) = %v, want %v", test.path, test.item, got, test.want)
		}
	}
}

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
	roots := matchingRepos(&model, "services")

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

func TestValidateSafePathRejectsUnsafePaths(t *testing.T) {
	for _, path := range []string{"", ".", "..", "../outside", "foo/../bar", "~/file", "/tmp/file", "foo//bar"} {
		if err := validateSafePath(path); err == nil {
			t.Fatalf("expected %q to be invalid", path)
		}
	}
	if err := validateSafePath(".agents/skills/platform"); err != nil {
		t.Fatal(err)
	}
}

func TestParseFileSrc(t *testing.T) {
	parsed, err := parseFileSrc("git:git@example.com:config.git#scripts/dev.sh")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.GitURL != "git@example.com:config.git" || parsed.Path != "scripts/dev.sh" {
		t.Fatalf("unexpected parsed src: %#v", parsed)
	}
	if _, err := parseFileSrc("git:git@example.com:config.git#../bad"); err == nil {
		t.Fatal("expected invalid source path")
	}
}

func TestFileLinkValidationAndOrdering(t *testing.T) {
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
	result := validateDefinition(def)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected validation errors: %#v", result.Errors)
	}
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := resolvePlan(&model, []string{}, planOptions{})
	if err != nil {
		t.Fatal(err)
	}
	active := map[string]bool{"scripts/dev.sh": true, "bin/dev": true}
	ordered := orderFilesForApply(&model, active)
	want := []string{"scripts/dev.sh", "bin/dev"}
	if !reflect.DeepEqual(ordered, want) {
		t.Fatalf("ordered files = %#v, want %#v", ordered, want)
	}
	_ = plan
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

func TestEnsureLinkFileCreatesRelativeSymlink(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "scripts", "dev.sh")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("script"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := emptyState()
	model := Model{Repos: map[string]RepoEntry{}, Groups: map[string]GroupEntry{}, Files: map[string]FileEntry{
		"scripts/dev.sh": {Path: "scripts/dev.sh", Identity: "dev-script", File: File{Src: "git:git@example.com:config.git#scripts/dev.sh"}},
		"bin/dev":        {Path: "bin/dev", Identity: "dev-command", File: File{Link: "scripts/dev.sh"}},
	}}

	if err := ensureFile(ioDiscard{}, root, &model, &state, "bin/dev", true); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(root, "bin", "dev"))
	if err != nil {
		t.Fatal(err)
	}
	if target != "../scripts/dev.sh" {
		t.Fatalf("symlink target = %q", target)
	}
	if state.Files["dev-command"].Link != "scripts/dev.sh" {
		t.Fatalf("state = %#v", state.Files["dev-command"])
	}
}

func TestEnsureFilePreservesLocalModification(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "scripts", "dev.sh")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := State{Version: 1, Repos: map[string]StateRepo{}, Files: map[string]StateFile{
		"dev-script": {Path: "scripts/dev.sh", Src: "git:git@example.com:config.git#scripts/dev.sh", SHA256: sha256Hex([]byte("original"))},
	}}
	model := Model{Repos: map[string]RepoEntry{}, Files: map[string]FileEntry{
		"scripts/dev.sh": {Path: "scripts/dev.sh", Identity: "dev-script", File: File{Src: "git:git@example.com:config.git#scripts/dev.sh"}},
	}}

	err := ensureFile(ioDiscard{}, root, &model, &state, "scripts/dev.sh", true)
	if err == nil || err.Error() != "locally modified" {
		t.Fatalf("expected locally modified error, got %v", err)
	}
}

func TestInstalledRepoIdentitySetUsesGitRepos(t *testing.T) {
	root := t.TempDir()
	model := Model{Repos: map[string]RepoEntry{
		"observability/tracing": {Path: "observability/tracing", Identity: "tracing-service", Repo: Repo{Git: "git@example.com:tracing.git"}},
	}, Files: map[string]FileEntry{}}
	state := State{Version: 1, Repos: map[string]StateRepo{
		"tracing-service": {Path: "observability/tracing", Git: "git@example.com:tracing.git"},
	}, Files: map[string]StateFile{}}

	repoDir := filepath.Join(root, "observability", "tracing")
	if err := exec.Command("git", "init", repoDir).Run(); err != nil {
		t.Fatal(err)
	}

	got := installedRepoIdentitySet(root, &model, &state)
	if !got["tracing-service"] {
		t.Fatalf("expected tracing-service to be installed: %#v", got)
	}
}

func TestInitFromLocalFileDoesNotSetSource(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.jig.json")
	workspacePath := filepath.Join(root, "workspace")
	if err := os.WriteFile(sourcePath, []byte(`{
  "version": 1,
  "tree": {
    "platform/auth": {
      "$repo": { "id": "auth-service", "git": "git@example.com:auth.git" }
    }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"init", sourcePath, workspacePath}, &out, ioDiscard{}); err != nil {
		t.Fatal(err)
	}

	def, err := loadDefinition(filepath.Join(workspacePath, definitionFile))
	if err != nil {
		t.Fatal(err)
	}
	if def.Source != nil {
		t.Fatalf("expected local init not to set source, got %#v", def.Source)
	}
	state, err := loadState(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Repos) != 0 || len(state.Files) != 0 {
		t.Fatalf("expected empty state, got %#v", state)
	}
}

func TestInitFromLocalFileRejectsPathFlag(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.jig.json")
	if err := os.WriteFile(sourcePath, []byte(`{"version":1,"tree":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := run([]string{"init", sourcePath, filepath.Join(root, "workspace"), "--path", "nested/.jig.json"}, ioDiscard{}, ioDiscard{})
	if err == nil || err.Error() != "--path can only be used with Git sources" {
		t.Fatalf("expected --path error, got %v", err)
	}
}

func TestParseInitArgsCloneWithoutPath(t *testing.T) {
	parsed, err := parseInitArgs([]string{"git@example.com:config.git", "--clone", "--with-optional-deps"})
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.Flags["--clone"] {
		t.Fatal("expected clone flag")
	}
	if parsed.Values["--clone"] != "" {
		t.Fatalf("expected empty clone path, got %q", parsed.Values["--clone"])
	}
	if !parsed.Flags["--with-optional-deps"] {
		t.Fatal("expected optional deps flag")
	}
}

func TestParseInitArgsCloneWithPath(t *testing.T) {
	parsed, err := parseInitArgs([]string{"git@example.com:config.git", "--clone", "services/checkout"})
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.Flags["--clone"] || parsed.Values["--clone"] != "services/checkout" {
		t.Fatalf("unexpected clone parse: %#v", parsed)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
