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

func testRepoEntry(path, identity string, repo Repo) Entry {
	return Entry{Path: path, Identity: identity, Kind: EntryRepo, Repo: &repo}
}

func testFileEntry(path, identity string, file File) Entry {
	return Entry{Path: path, Identity: identity, Kind: EntryFile, File: &file}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
