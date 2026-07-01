package jig

import (
	"encoding/json"
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

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
