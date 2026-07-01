package jig

import (
	"bytes"
	"strings"
	"testing"
)

func TestDefinitionChangesTrackGroupsByIdentity(t *testing.T) {
	oldModel, err := flattenDefinition(testDefinition(t, `{
  "version": 1,
  "tree": {
    "platform": {
      "$group": { "id": "shared-group" }
    }
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	newModel, err := flattenDefinition(testDefinition(t, `{
  "version": 1,
  "tree": {
    "services": {
      "$group": { "id": "shared-group" }
    }
  }
}`))
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	printDefinitionChanges(&out, &oldModel, &newModel)
	if !strings.Contains(out.String(), "group-moved:\n  shared-group: platform -> services\n") {
		t.Fatalf("group changes:\n%s", out.String())
	}
}
