package jig

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
)

// SrcEntry is one source of a $file or $dir entry. An optional onlyWhen
// gates just this source's contribution: its tree within a dir merge, or its
// content within a file concatenation.
type SrcEntry struct {
	Src      string     `json:"src"`
	OnlyWhen *Condition `json:"onlyWhen,omitempty"`
}

// SrcList accepts a single source string, or a list whose elements are
// strings or {src, onlyWhen} objects.
type SrcList []SrcEntry

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
			list = append(list, SrcEntry{Src: str})
			continue
		}
		var source SrcEntry
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
	ID          string            `json:"id,omitempty"`
	Git         string            `json:"git"`
	Web         string            `json:"web,omitempty"`
	Description string            `json:"description,omitempty"`
	Setup       string            `json:"setup,omitempty"` // lifecycle commands, run in the checkout by the
	Fmt         string            `json:"fmt,omitempty"`   // matching jig verb and never automatically; the
	Lint        string            `json:"lint,omitempty"`  // vocabulary is fixed so a mixed-technology fleet
	Test        string            `json:"test,omitempty"`  // standardizes on the verb, not the tooling
	Archived    bool              `json:"archived,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"` // user-defined metadata, opaque to jig
	DependsOn   []Dependency      `json:"dependsOn,omitempty"`
	OnlyWhen    *Condition        `json:"onlyWhen,omitempty"`
}

type File struct {
	ID          string            `json:"id,omitempty"`
	Src         SrcList           `json:"src,omitempty"`  // one or more sources, concatenated in order
	Link        *Ref              `json:"link,omitempty"` // single-target: id or exact path of another $file
	linkPath    string            // Link resolved to the target's tree path after flattening
	Copy        *Ref              `json:"copy,omitempty"` // single-target: materialize another $file's sources as a real file
	copyPath    string            // Copy resolved to the target's tree path after flattening
	Description string            `json:"description,omitempty"`
	Executable  bool              `json:"executable,omitempty"`
	Archived    bool              `json:"archived,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"` // user-defined metadata, opaque to jig
	OnlyWhen    *Condition        `json:"onlyWhen,omitempty"`
}

// Dir materializes a whole subtree of a source repository into the
// workspace, or symlinks to another $dir entry. Executable bits come from
// the git tree, so there is no executable field.
type Dir struct {
	ID          string            `json:"id,omitempty"`
	Src         SrcList           `json:"src,omitempty"`  // one or more sources, merged in order; first wins on conflicts
	Link        *Ref              `json:"link,omitempty"` // single-target: id or exact path of another $dir to symlink to
	linkPath    string            // Link resolved to the target's tree path after flattening
	Copy        *Ref              `json:"copy,omitempty"` // single-target: materialize another $dir's sources as a real directory
	copyPath    string            // Copy resolved to the target's tree path after flattening
	Description string            `json:"description,omitempty"`
	Archived    bool              `json:"archived,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"` // user-defined metadata, opaque to jig
	OnlyWhen    *Condition        `json:"onlyWhen,omitempty"`
}

// targetPath returns the resolved tree path this entry aliases, whether as a
// symlink (link) or a materialized copy (copy); empty for source entries.
// Planning treats both alias kinds alike: the alias is active only when its
// target is, and targets are applied before their aliases.
func (f *File) targetPath() string {
	if f.linkPath != "" {
		return f.linkPath
	}
	return f.copyPath
}

func (d *Dir) targetPath() string {
	if d.linkPath != "" {
		return d.linkPath
	}
	return d.copyPath
}

type Group struct {
	ID          string            `json:"id,omitempty"`
	Description string            `json:"description,omitempty"`
	Web         string            `json:"web,omitempty"`
	Setup       string            `json:"setup,omitempty"` // lifecycle commands inherited by descendant
	Fmt         string            `json:"fmt,omitempty"`   // repositories that do not declare their own;
	Lint        string            `json:"lint,omitempty"`  // nearest ancestor wins
	Test        string            `json:"test,omitempty"`
	Archived    bool              `json:"archived,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"` // user-defined metadata, inherited per key by descendants
	DependsOn   []Dependency      `json:"dependsOn,omitempty"`
	OnlyWhen    *Condition        `json:"onlyWhen,omitempty"`
}

// Ref is a schema reference: exactly one of ID, Path, and Tags selects the
// target entries. Path is exact, or the recursive subtree strictly below it
// with a trailing "/*". Tags select every repository carrying all of them.
type Ref struct {
	ID   string   `json:"id,omitempty"`
	Path string   `json:"path,omitempty"`
	Tags []string `json:"tags,omitempty"`
}

// selectorCount reports how many selector fields are set; valid refs have
// exactly one.
func (ref Ref) selectorCount() int {
	count := 0
	if ref.ID != "" {
		count++
	}
	if ref.Path != "" {
		count++
	}
	if len(ref.Tags) > 0 {
		count++
	}
	return count
}

// subtreePath splits the "/*" subtree marker off a path selector: "a/*"
// yields ("a", true) and bare "*" yields ("", true), meaning the whole tree.
func subtreePath(path string) (string, bool) {
	if path == "*" {
		return "", true
	}
	if base, ok := strings.CutSuffix(path, "/*"); ok {
		return base, true
	}
	return path, false
}

// describeRef renders a reference for messages and info output, e.g.
// "id auth-service", "platform/*", or "tags api,go".
func describeRef(ref Ref) string {
	switch {
	case ref.ID != "":
		return "id " + ref.ID
	case ref.Path != "":
		return ref.Path
	case len(ref.Tags) > 0:
		return "tags " + strings.Join(ref.Tags, ",")
	default:
		return "(empty reference)"
	}
}

type Dependency struct {
	Ref
	Optional bool   `json:"optional,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Condition holds when some active or installed repository is selected by
// its reference.
type Condition struct {
	Ref
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
