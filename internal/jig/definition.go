package jig

import (
	"encoding/json"
	"errors"
	"os"
)

// DirSource is one source of a $dir entry. An optional onlyWhen gates just
// this source's tree within the merge.
type DirSource struct {
	Src      string     `json:"src"`
	OnlyWhen *Condition `json:"onlyWhen,omitempty"`
}

// SrcList accepts a single source string, or a list whose elements are
// strings or {src, onlyWhen} objects.
type SrcList []DirSource

func (s *SrcList) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*s = SrcList{{Src: single}}
		return nil
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(data, &raws); err != nil {
		return errors.New("src must be a string or a list of sources")
	}
	list := make(SrcList, 0, len(raws))
	for _, raw := range raws {
		var str string
		if err := json.Unmarshal(raw, &str); err == nil {
			list = append(list, DirSource{Src: str})
			continue
		}
		var source DirSource
		if err := json.Unmarshal(raw, &source); err != nil {
			return errors.New("src entries must be strings or {src, onlyWhen} objects")
		}
		list = append(list, source)
	}
	*s = list
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
// workspace, or symlinks to another $dir entry. Executable bits come from
// the git tree, so there is no executable field.
type Dir struct {
	ID          string     `json:"id,omitempty"`
	Src         SrcList    `json:"src,omitempty"`  // one or more sources, merged in order; first wins on conflicts
	Link        string     `json:"link,omitempty"` // symlink to another $dir entry instead of materializing
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
