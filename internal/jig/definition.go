package jig

import (
	"encoding/json"
	"os"
)

type Definition struct {
	Version int                        `json:"version"`
	Source  *Source                    `json:"source,omitempty"`
	Tree    map[string]json.RawMessage `json:"tree"`
	Repos   map[string]Repo            `json:"repos,omitempty"` // legacy input is rejected by validation; kept so old files parse clearly.
	Extra   map[string]json.RawMessage `json:"-"`
}

type Source struct {
	Type string `json:"type"`
	URL  string `json:"url"`
	Ref  string `json:"ref,omitempty"`
	Path string `json:"path,omitempty"`
}

type Repo struct {
	ID          string       `json:"id,omitempty"`
	Git         string       `json:"git"`
	Web         string       `json:"web,omitempty"`
	Description string       `json:"description,omitempty"`
	Archived    bool         `json:"archived,omitempty"`
	DependsOn   []Dependency `json:"dependsOn,omitempty"`
	OnlyWhen    *Condition   `json:"onlyWhen,omitempty"`
}

type File struct {
	ID          string     `json:"id,omitempty"`
	Src         string     `json:"src"`
	Link        string     `json:"link,omitempty"`
	Description string     `json:"description,omitempty"`
	Executable  bool       `json:"executable,omitempty"`
	Archived    bool       `json:"archived,omitempty"`
	OnlyWhen    *Condition `json:"onlyWhen,omitempty"`
}

type Group struct {
	ID          string       `json:"id,omitempty"`
	Description string       `json:"description,omitempty"`
	Web         string       `json:"web,omitempty"`
	Archived    bool         `json:"archived,omitempty"`
	DependsOn   []Dependency `json:"dependsOn,omitempty"`
	OnlyWhen    *Condition   `json:"onlyWhen,omitempty"`
}

type Dependency struct {
	Path     string `json:"path"`
	Optional bool   `json:"optional,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type Condition struct {
	Path   string `json:"path"`
	Reason string `json:"reason,omitempty"`
}

func loadDefinition(path string) (*Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var def Definition
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	return &def, nil
}
