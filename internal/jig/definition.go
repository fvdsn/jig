package jig

import (
	"encoding/json"
	"errors"
	"os"
)

// SrcList accepts either a single source string or a list of sources.
type SrcList []string

func (s *SrcList) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*s = SrcList{single}
		return nil
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return errors.New("src must be a string or a list of strings")
	}
	*s = SrcList(list)
	return nil
}

type Definition struct {
	Version int                        `json:"version"`
	Source  *Source                    `json:"source,omitempty"`
	Tree    map[string]json.RawMessage `json:"tree"`
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
	Tags        []string     `json:"tags,omitempty"`
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
	Tags        []string   `json:"tags,omitempty"`
	OnlyWhen    *Condition `json:"onlyWhen,omitempty"`
}

// Dir materializes a whole subtree of a source repository into the
// workspace. Executable bits come from the git tree, so there is no
// executable field, and dirs cannot be link targets.
type Dir struct {
	ID          string     `json:"id,omitempty"`
	Src         SrcList    `json:"src"` // one or more sources, merged in order; first wins on conflicts
	Description string     `json:"description,omitempty"`
	Archived    bool       `json:"archived,omitempty"`
	Tags        []string   `json:"tags,omitempty"`
	OnlyWhen    *Condition `json:"onlyWhen,omitempty"`
}

type Group struct {
	ID          string       `json:"id,omitempty"`
	Description string       `json:"description,omitempty"`
	Web         string       `json:"web,omitempty"`
	Archived    bool         `json:"archived,omitempty"`
	Tags        []string     `json:"tags,omitempty"`
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
