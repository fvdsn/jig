package jig

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	yaml "github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// A schema file is JSON or YAML, by extension, with the same structure in
// both. YAML is read into the JSON the same schema would be, then decoded
// by the JSON path, so the definition types and every rule and message
// downstream know one format. The YAML subset is strict: one document,
// no anchors, aliases, merge keys, or local tags, no duplicate keys; each
// is refused by name rather than resolved into a tree the author did not
// write.

// isYAMLSchema reports whether a schema path names a YAML file.
func isYAMLSchema(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return true
	}
	return false
}

// yamlToJSON converts a YAML schema to the equivalent JSON document.
func yamlToJSON(data []byte) ([]byte, error) {
	file, err := parser.ParseBytes(data, 0)
	if err != nil {
		return nil, fmt.Errorf("invalid YAML: %s", err)
	}
	if len(file.Docs) > 1 {
		return nil, errors.New("invalid YAML: multi-document files are not supported; keep one document")
	}
	if len(file.Docs) == 0 || file.Docs[0].Body == nil {
		return nil, errors.New("invalid YAML: the document is empty")
	}
	check := &yamlSubsetVisitor{}
	ast.Walk(check, file.Docs[0])
	if check.err != nil {
		return nil, check.err
	}
	out, err := yaml.YAMLToJSON(data)
	if err != nil {
		return nil, fmt.Errorf("invalid YAML: %s", err)
	}
	return out, nil
}

// yamlSubsetVisitor refuses the YAML constructs outside the subset.
type yamlSubsetVisitor struct {
	err error
}

func (v *yamlSubsetVisitor) Visit(node ast.Node) ast.Visitor {
	if v.err != nil || node == nil {
		return nil
	}
	refuse := func(what string, hint string) {
		line := 0
		if token := node.GetToken(); token != nil && token.Position != nil {
			line = token.Position.Line
		}
		v.err = fmt.Errorf("invalid YAML: line %d: %s is not supported; %s", line, what, hint)
	}
	switch n := node.(type) {
	case *ast.AnchorNode:
		refuse("an anchor (&"+n.Name.String()+")", "write the value out where it is used")
	case *ast.AliasNode:
		refuse("an alias (*"+n.Value.String()+")", "write the value out where it is used")
	case *ast.MergeKeyNode:
		refuse("a merge key (<<)", "write the merged keys out")
	case *ast.TagNode:
		if n.Start != nil && !strings.HasPrefix(n.Start.Value, "!!") {
			refuse("the tag "+n.Start.Value, "only the standard !! tags are read")
		}
	}
	if v.err != nil {
		return nil
	}
	return v
}

// yamlDecodeHint explains a JSON decoding error that YAML typing causes:
// an unquoted scalar read as a number or boolean where a string is wanted.
func yamlDecodeHint(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "cannot unmarshal number") || strings.Contains(msg, "cannot unmarshal bool") {
		return fmt.Errorf("%s (YAML reads an unquoted scalar as a number or boolean; quote it to make it a string)", msg)
	}
	return err
}
