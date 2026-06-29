package main

import (
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPathMatchesSegmentBoundary(t *testing.T) {
	tests := []struct {
		path string
		repo string
		want bool
	}{
		{"platform", "platform.auth", true},
		{"platform", "platform", true},
		{"platform", "platforming.api", false},
		{"platform.auth", "platform.auth", true},
		{"platform.auth", "platform.auth.extra", true},
	}
	for _, test := range tests {
		if got := pathMatches(test.path, test.repo); got != test.want {
			t.Fatalf("pathMatches(%q, %q) = %v, want %v", test.path, test.repo, got, test.want)
		}
	}
}

func TestResolveDependenciesRecursiveAndOptional(t *testing.T) {
	def := &Definition{Version: 1, Repos: map[string]Repo{
		"services.checkout": {
			Git: "git@example.com:checkout.git",
			DependsOn: []Dependency{
				{Path: "platform"},
				{Path: "observability", Optional: true},
			},
		},
		"platform.auth": {
			Git:       "git@example.com:auth.git",
			DependsOn: []Dependency{{Path: "shared.crypto"}},
		},
		"platform.billing": {Git: "git@example.com:billing.git"},
		"shared.crypto":    {Git: "git@example.com:crypto.git"},
		"observability.tracing": {
			Git: "git@example.com:tracing.git",
		},
	}}

	got, err := resolveDependencies(def, "services.checkout", false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"platform.auth", "platform.billing", "shared.crypto"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deps without optional = %#v, want %#v", got, want)
	}

	got, err = resolveDependencies(def, "services.checkout", true)
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"observability.tracing", "platform.auth", "platform.billing", "shared.crypto"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deps with optional = %#v, want %#v", got, want)
	}
}

func TestResolveDependenciesDoesNotIncludeRootInCycle(t *testing.T) {
	def := &Definition{Version: 1, Repos: map[string]Repo{
		"service.a": {
			Git:       "git@example.com:a.git",
			DependsOn: []Dependency{{Path: "service.b"}},
		},
		"service.b": {
			Git:       "git@example.com:b.git",
			DependsOn: []Dependency{{Path: "service.a"}},
		},
	}}

	got, err := resolveDependencies(def, "service.a", false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"service.b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deps with cycle = %#v, want %#v", got, want)
	}
}

func TestResolveRepoSetForGroupDeduplicatesDependencies(t *testing.T) {
	def := &Definition{Version: 1, Repos: map[string]Repo{
		"services.checkout": {
			Git:       "git@example.com:checkout.git",
			DependsOn: []Dependency{{Path: "platform.auth"}},
		},
		"services.cart": {
			Git:       "git@example.com:cart.git",
			DependsOn: []Dependency{{Path: "platform.auth"}},
		},
		"platform.auth": {
			ID:  "auth-service",
			Git: "git@example.com:auth.git",
		},
	}}

	roots := matchingRepos(def, "services")
	got, err := resolveRepoSet(def, roots, false, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"platform.auth"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("group deps = %#v, want %#v", got, want)
	}

	got, err = resolveRepoSet(def, roots, false, true)
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"platform.auth", "services.cart", "services.checkout"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("group clone set = %#v, want %#v", got, want)
	}
}

func TestResolveRepoSetForSyncIncludesInstalledOptionalDependency(t *testing.T) {
	root := t.TempDir()
	def := &Definition{Version: 1, Repos: map[string]Repo{
		"services.checkout": {
			Git:       "git@example.com:checkout.git",
			DependsOn: []Dependency{{Path: "observability.tracing", Optional: true}},
		},
		"observability.tracing": {
			ID:  "tracing-service",
			Git: "git@example.com:tracing.git",
		},
	}}
	state := State{Version: 1, Repos: map[string]StateRepo{
		"tracing-service": {Path: "observability/tracing", Git: "git@example.com:tracing.git"},
	}}

	repoDir := filepath.Join(root, "observability", "tracing")
	if err := exec.Command("git", "init", repoDir).Run(); err != nil {
		t.Fatal(err)
	}

	got, err := resolveRepoSetForSync(root, def, &state, []string{"services.checkout"}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"observability.tracing", "services.checkout"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sync set = %#v, want %#v", got, want)
	}
}

func TestResolveRepoSetForSyncSkipsMissingOptionalDependency(t *testing.T) {
	root := t.TempDir()
	def := &Definition{Version: 1, Repos: map[string]Repo{
		"services.checkout": {
			Git:       "git@example.com:checkout.git",
			DependsOn: []Dependency{{Path: "observability.tracing", Optional: true}},
		},
		"observability.tracing": {
			ID:  "tracing-service",
			Git: "git@example.com:tracing.git",
		},
	}}
	state := State{Version: 1, Repos: map[string]StateRepo{}}

	got, err := resolveRepoSetForSync(root, def, &state, []string{"services.checkout"}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"services.checkout"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sync set = %#v, want %#v", got, want)
	}
}

func TestValidateDefinitionDuplicateIdentity(t *testing.T) {
	def := &Definition{Version: 1, Repos: map[string]Repo{
		"platform.auth":         {ID: "auth-service", Git: "git@example.com:auth.git"},
		"identity.auth-service": {ID: "auth-service", Git: "git@example.com:auth2.git"},
	}}

	result := validateDefinition(def)
	if len(result.Errors) == 0 {
		t.Fatal("expected validation error")
	}
}

func TestValidateDefinitionDependencyPathMustResolve(t *testing.T) {
	def := &Definition{Version: 1, Repos: map[string]Repo{
		"services.checkout": {
			Git:       "git@example.com:checkout.git",
			DependsOn: []Dependency{{Path: "missing"}},
		},
	}}

	result := validateDefinition(def)
	if len(result.Errors) == 0 {
		t.Fatal("expected validation error")
	}
}

func TestRepoLocalPath(t *testing.T) {
	if got := repoLocalPath("org.sub.repo"); got != "org/sub/repo" {
		t.Fatalf("repoLocalPath = %q", got)
	}
}
