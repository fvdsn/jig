package jig

import (
	"strings"
)

// frontmatter is the YAML header of a markdown subagent or command file,
// in the common Claude Code / Copilot form: a flat mapping of scalar
// fields, optionally with simple lists. Jig reads only what the harness
// transforms need (name, description, model settings) and keeps every
// value as the string it was written as; nested YAML is not interpreted.
type frontmatter struct {
	fields map[string]string
	order  []string
}

func (fm frontmatter) get(key string) string {
	return fm.fields[key]
}

func (fm frontmatter) has(key string) bool {
	_, ok := fm.fields[key]
	return ok
}

// splitFrontmatter separates a markdown document into its frontmatter and
// body. A document without a leading "---" line has an empty frontmatter
// and is its own body.
func splitFrontmatter(content []byte) (frontmatter, string) {
	fm := frontmatter{fields: map[string]string{}}
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return fm, text
	}
	header, body, found := strings.Cut(rest, "\n---\n")
	if !found {
		// A closing marker as the very last line, without a trailing
		// newline.
		if trimmed, ok := strings.CutSuffix(rest, "\n---"); ok {
			header, body = trimmed, ""
		} else {
			return fm, text
		}
	}
	parseFrontmatterFields(&fm, header)
	return fm, body
}

// parseFrontmatterFields reads "key: value" lines. A quoted value loses
// its quotes; a block scalar (| or >) collects the indented lines that
// follow; a block sequence of "- item" lines becomes the items joined by
// ", ". Indented continuation lines belong to the previous field.
func parseFrontmatterFields(fm *frontmatter, header string) {
	lines := strings.Split(header, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' || line[0] == '-' {
			continue // continuation handled by the field that owns it
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch value {
		case "|", ">", "|-", ">-":
			var block []string
			for i+1 < len(lines) && (strings.HasPrefix(lines[i+1], " ") || strings.HasPrefix(lines[i+1], "\t") || strings.TrimSpace(lines[i+1]) == "") {
				i++
				block = append(block, strings.TrimLeft(lines[i], " \t"))
			}
			joiner := "\n"
			if strings.HasPrefix(value, ">") {
				joiner = " "
			}
			value = strings.TrimRight(strings.Join(block, joiner), " \n")
		case "":
			var items []string
			for i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "- ") {
				i++
				items = append(items, unquoteYAML(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i]), "- "))))
			}
			value = strings.Join(items, ", ")
		default:
			value = unquoteYAML(value)
		}
		if _, seen := fm.fields[key]; !seen {
			fm.order = append(fm.order, key)
		}
		fm.fields[key] = value
	}
}

// unquoteYAML strips matching single or double quotes from a scalar and
// resolves the escapes a double-quoted scalar may carry.
func unquoteYAML(value string) string {
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		inner := value[1 : len(value)-1]
		inner = strings.ReplaceAll(inner, `\"`, `"`)
		inner = strings.ReplaceAll(inner, `\n`, "\n")
		inner = strings.ReplaceAll(inner, `\\`, `\`)
		return inner
	}
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'")
	}
	return value
}
