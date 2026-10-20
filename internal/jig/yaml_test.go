package jig

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The spec's initial schema, in both formats; they must load to the same
// model.
const yamlSchema = `version: 3
tree:
  platform/auth:
    $repo:
      id: auth-service
      git: git@github.com:org/platform-auth.git
      web: https://github.com/org/platform-auth
      description: Authentication service
  services/checkout:
    $repo:
      id: checkout-service
      git: git@github.com:org/checkout.git
      description: Checkout service
      dependsOn:
        - path: platform/*
          reason: checkout uses shared platform services
  tools:
    $group:
      description: Developer tools
      onlyWhen:
        path: platform/*
        reason: Only useful when platform repositories are installed
    platform-debug:
      $repo:
        id: platform-debug-tools
        git: git@github.com:org/platform-debug-tools.git
        tags: [on, yes, 2024-01-01]   # YAML 1.2: these stay strings
  .agents/skills/platform:
    $file:
      id: platform-skill
      src: git@github.com:org/workspace-config.git#agents/skills/platform.md
      description: Agent skill for platform repositories
      onlyWhen: { path: platform/*, reason: Only useful when platform repositories are installed }
`

const jsonSchema = `{
  "version": 3,
  "tree": {
    "platform/auth": {"$repo": {"id": "auth-service", "git": "git@github.com:org/platform-auth.git", "web": "https://github.com/org/platform-auth", "description": "Authentication service"}},
    "services/checkout": {"$repo": {"id": "checkout-service", "git": "git@github.com:org/checkout.git", "description": "Checkout service", "dependsOn": [{"path": "platform/*", "reason": "checkout uses shared platform services"}]}},
    "tools": {
      "$group": {"description": "Developer tools", "onlyWhen": {"path": "platform/*", "reason": "Only useful when platform repositories are installed"}},
      "platform-debug": {"$repo": {"id": "platform-debug-tools", "git": "git@github.com:org/platform-debug-tools.git", "tags": ["on", "yes", "2024-01-01"]}}
    },
    ".agents/skills/platform": {"$file": {"id": "platform-skill", "src": "git@github.com:org/workspace-config.git#agents/skills/platform.md", "description": "Agent skill for platform repositories", "onlyWhen": {"path": "platform/*", "reason": "Only useful when platform repositories are installed"}}}
  }
}`

func TestYAMLSchemaLoadsLikeJSON(t *testing.T) {
	root := t.TempDir()
	write := func(name string, content string) string {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	fromYAML, err := loadDefinition(write("jig.yaml", yamlSchema))
	if err != nil {
		t.Fatal(err)
	}
	fromJSON, err := loadDefinition(write("jig.json", jsonSchema))
	if err != nil {
		t.Fatal(err)
	}
	yamlModel, err := flattenDefinition(fromYAML, nil)
	if err != nil {
		t.Fatal(err)
	}
	jsonModel, err := flattenDefinition(fromJSON, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(yamlModel.Entries, jsonModel.Entries) {
		t.Fatalf("models differ:\nyaml %+v\njson %+v", yamlModel.Entries, jsonModel.Entries)
	}
	if errs := validateDefinition(fromYAML).Errors; len(errs) != 0 {
		t.Fatalf("errors = %v", errs)
	}
	// .yml is YAML too, and the same content with a JSON name is not.
	if _, err := loadDefinition(write("jig.yml", yamlSchema)); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDefinition(write("other.json", yamlSchema)); err == nil {
		t.Fatal("YAML under a JSON name accepted")
	}
	// jig validate takes a YAML file.
	var out bytes.Buffer
	if err := Validate(ValidateOptions{File: filepath.Join(root, "jig.yaml")}, &out); err != nil || !strings.Contains(out.String(), "valid schema") {
		t.Fatalf("validate: %v\n%s", err, out.String())
	}
}

func TestYAMLSchemaSubset(t *testing.T) {
	cases := map[string]string{
		"a: 1\na: 2\n":                                  `mapping key "a" already defined`,
		"base: &b {x: 1}\nother: *b\n":                  "line 1: an anchor (&b) is not supported",
		"base: {x: 1}\nother:\n  <<: {y: 2}\n":          "line 3: a merge key (<<) is not supported",
		"version: !custom 4\n":                          "line 1: the tag !custom is not supported",
		"version: 4\n---\nversion: 5\n":                 "multi-document files are not supported",
		"# nothing here\n":                              "the document is empty",
		"version: [4\n":                                 "invalid YAML",
		"version: 4\ntree:\n  a:\n    $repo: {id: 1}\n": "quote it to make it a string",
	}
	root := t.TempDir()
	for content, want := range cases {
		path := filepath.Join(root, "jig.yaml")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		def, err := loadDefinition(path)
		if err == nil {
			_, err = flattenDefinition(def, nil)
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%q: err = %v, want %q", content, err, want)
		}
	}
	// The standard tags and comments are fine.
	data, err := yamlToJSON([]byte("version: !!int 4 # four\ntree: {}\n"))
	if err != nil || !strings.Contains(string(data), `"version": 4`) {
		t.Fatalf("standard tag: %s %v", data, err)
	}
}

// Init keeps a local YAML file's format in the source checkout, and finds
// jig.yaml at the root of a git source.
func TestInitWithYAMLSchemas(t *testing.T) {
	root := t.TempDir()
	local := filepath.Join(root, "workspace.yml")
	if err := os.WriteFile(local, []byte(yamlSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	fromFile := filepath.Join(root, "from-file")
	if err := Init(InitOptions{SourceArg: local, WorkspaceDir: fromFile}, &out); err != nil {
		t.Fatalf("init from file: %v\n%s", err, out.String())
	}
	if config, _ := loadConfig(fromFile); config.Schema != "jig.yaml" {
		t.Fatalf("schema = %q", config.Schema)
	}
	if !pathExists(filepath.Join(fromFile, sourceDir, "jig.yaml")) {
		t.Fatal("jig.yaml not seeded")
	}

	remote := filepath.Join(root, "schema-repo")
	testFileSource(t, remote, map[string]string{"jig.yaml": yamlSchema})
	fromGit := filepath.Join(root, "from-git")
	out.Reset()
	if err := Init(InitOptions{SourceArg: remote, WorkspaceDir: fromGit}, &out); err != nil {
		t.Fatalf("init from git: %v\n%s", err, out.String())
	}
	if config, _ := loadConfig(fromGit); config.Schema != "jig.yaml" {
		t.Fatalf("schema = %q", config.Schema)
	}
	ws, err := loadWorkspaceAt(fromGit, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ws.Model.entry("services/checkout", EntryRepo); !ok {
		t.Fatal("YAML schema not loaded from the checkout")
	}
}
