package jig

import (
	"strings"
)

// tomlField is one key of a flat TOML document, in output order.
type tomlField struct {
	key   string
	value string
}

// tomlDocument renders a flat document of string fields, the shape the
// TOML-based harness formats (Codex subagents, Gemini commands) take.
// Jig only ever writes these documents, so no parser is needed.
func tomlDocument(fields []tomlField) []byte {
	var b strings.Builder
	for _, field := range fields {
		b.WriteString(field.key)
		b.WriteString(" = ")
		b.WriteString(tomlString(field.value))
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// tomlString quotes a value: a basic string on one line, or a multi-line
// basic string when the value spans lines, so prose stays readable in the
// generated file.
func tomlString(value string) string {
	if !strings.Contains(value, "\n") {
		return `"` + escapeTOML(value, false) + `"`
	}
	return "\"\"\"\n" + escapeTOML(value, true) + "\"\"\""
}

// escapeTOML escapes a string's content for a basic string; multi-line
// strings keep their newlines and only need """ sequences broken up.
func escapeTOML(value string, multiline bool) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r == '\n' && multiline:
			b.WriteByte('\n')
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteByte('\t')
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			b.WriteString(`\u00`)
			b.WriteByte("0123456789abcdef"[r>>4])
			b.WriteByte("0123456789abcdef"[r&0xf])
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
