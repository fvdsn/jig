package jig

import (
	"archive/tar"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// A rule (an "instruction" in the agents block) is a markdown file with a
// neutral frontmatter: description, and paths (a glob list, or a
// comma-separated string; applyTo is accepted as a synonym for rules
// written for Copilot or APM). A rule without paths is always on. Each
// harness with per-file rules gets the same body under its own
// frontmatter; harnesses without them get the rules folded into their
// root context file.
type ruleMeta struct {
	description string
	globs       []string
}

func parseRule(content []byte) (ruleMeta, string) {
	fm, body := splitFrontmatter(content)
	meta := ruleMeta{description: fm.get("description")}
	for _, key := range []string{"paths", "applyTo", "globs"} {
		if fm.has(key) {
			meta.globs = splitGlobs(fm.get(key))
			break
		}
	}
	return meta, body
}

// splitGlobs splits a glob list on top-level commas: commas inside brace
// alternation ("**/*.{css,scss}") belong to the glob.
func splitGlobs(value string) []string {
	var globs []string
	depth, start := 0, 0
	flush := func(end int) {
		if glob := strings.TrimSpace(value[start:end]); glob != "" {
			globs = append(globs, glob)
		}
	}
	for i, r := range value {
		switch r {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				flush(i)
				start = i + 1
			}
		}
	}
	flush(len(value))
	return globs
}

// yamlQuote renders a double-quoted YAML scalar.
func yamlQuote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(value) + `"`
}

func yamlList(key string, items []string) string {
	var b strings.Builder
	b.WriteString(key + ":\n")
	for _, item := range items {
		b.WriteString("  - " + yamlQuote(item) + "\n")
	}
	return b.String()
}

// ruleStem is the rule's name: the markdown stem without the
// ".instructions" suffix APM sources carry.
func ruleStem(rel string) (dir string, stem string, ok bool) {
	dir, stem, ok = markdownStem(rel)
	if ok {
		stem = strings.TrimSuffix(stem, ".instructions")
	}
	return dir, stem, ok
}

func withFrontmatter(header string, body string) []byte {
	if header == "" {
		return []byte(body)
	}
	return []byte("---\n" + header + "---\n" + body)
}

// claudeRuleTransform: .claude/rules/<name>.md with a paths list, or no
// frontmatter for an always-on rule.
func claudeRuleTransform(rel string, content []byte) (string, []byte) {
	dir, stem, ok := ruleStem(rel)
	if !ok {
		return rel, content
	}
	meta, body := parseRule(content)
	header := ""
	if len(meta.globs) > 0 {
		header = yamlList("paths", meta.globs)
	}
	return dir + stem + ".md", withFrontmatter(header, body)
}

// copilotRuleTransform: .github/instructions/<name>.instructions.md with
// applyTo (Copilot splits the comma list itself); "**" for always on.
func copilotRuleTransform(rel string, content []byte) (string, []byte) {
	dir, stem, ok := ruleStem(rel)
	if !ok {
		return rel, content
	}
	meta, body := parseRule(content)
	header := ""
	if meta.description != "" {
		header += "description: " + yamlQuote(meta.description) + "\n"
	}
	applyTo := "**"
	if len(meta.globs) > 0 {
		applyTo = strings.Join(meta.globs, ",")
	}
	header += "applyTo: " + yamlQuote(applyTo) + "\n"
	return dir + stem + ".instructions.md", withFrontmatter(header, body)
}

// cursorRuleTransform: .cursor/rules/<name>.mdc with description, globs
// as one comma-joined scalar, and alwaysApply.
func cursorRuleTransform(rel string, content []byte) (string, []byte) {
	dir, stem, ok := ruleStem(rel)
	if !ok {
		return rel, content
	}
	meta, body := parseRule(content)
	description := meta.description
	if description == "" {
		description = firstSentence(body)
	}
	header := ""
	if description != "" {
		header += "description: " + yamlQuote(description) + "\n"
	}
	if len(meta.globs) > 0 {
		header += "globs: " + yamlQuote(strings.Join(meta.globs, ",")) + "\nalwaysApply: false\n"
	} else {
		header += "alwaysApply: true\n"
	}
	return dir + stem + ".mdc", withFrontmatter(header, body)
}

// windsurfRuleTransform: .windsurf/rules/<name>.md with trigger glob and
// globs, or trigger always_on.
func windsurfRuleTransform(rel string, content []byte) (string, []byte) {
	dir, stem, ok := ruleStem(rel)
	if !ok {
		return rel, content
	}
	meta, body := parseRule(content)
	header := "trigger: always_on\n"
	if len(meta.globs) > 0 {
		header = "trigger: glob\n" + yamlList("globs", meta.globs)
	}
	return dir + stem + ".md", withFrontmatter(header, body)
}

// kiroRuleTransform: .kiro/steering/<name>.md with inclusion fileMatch and
// fileMatchPattern, or inclusion always.
func kiroRuleTransform(rel string, content []byte) (string, []byte) {
	dir, stem, ok := ruleStem(rel)
	if !ok {
		return rel, content
	}
	meta, body := parseRule(content)
	header := "inclusion: always\n"
	if len(meta.globs) > 0 {
		header = "inclusion: fileMatch\n" + yamlList("fileMatchPattern", meta.globs)
	}
	return dir + stem + ".md", withFrontmatter(header, body)
}

// firstSentence derives a description from a body's first text line, for
// a harness that wants one.
func firstSentence(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "#-*> "))
		if line == "" {
			continue
		}
		if i := strings.Index(line, ". "); i >= 0 {
			line = line[:i]
		}
		return strings.TrimSuffix(line, ".")
	}
	return ""
}

// ruleFile is one rule read from the instruction sources, for folding.
type ruleFile struct {
	rel     string
	content []byte
}

// renderFold appends the rules to a root context file for a harness that
// has no per-file rules: one section per rule, its scope stated as text
// since the harness cannot act on it.
func renderFold(rules []ruleFile) []byte {
	if len(rules) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("## Instructions\n")
	for _, rule := range rules {
		meta, body := parseRule(rule.content)
		_, stem, _ := ruleStem(rule.rel)
		title := meta.description
		if title == "" {
			title = stem
		}
		b.WriteString("\n### " + title + "\n\n")
		if len(meta.globs) > 0 {
			quoted := make([]string, len(meta.globs))
			for i, glob := range meta.globs {
				quoted[i] = "`" + glob + "`"
			}
			b.WriteString("Applies to: " + strings.Join(quoted, ", ") + "\n\n")
		}
		b.WriteString(strings.TrimSpace(body) + "\n")
	}
	return []byte(b.String())
}

// readRuleSources resolves the instruction sources of a folded root file
// and returns their markdown files, in path order with later sources
// winning on a conflicting path, plus the tree or content ids that
// identify what was read.
func (m *materializer) readRuleSources(srcs SrcList) ([]ruleFile, []string, resolvedSources, error) {
	files := map[string][]byte{}
	var ids []string
	addTree := func(entries []ruleFile) {
		for _, entry := range entries {
			if _, _, ok := ruleStem(entry.rel); ok {
				files[entry.rel] = entry.content
			}
		}
	}
	resolved, err := m.resolveSources(srcs, func(source SrcEntry) (string, error) {
		switch {
		case source.File != "":
			localAbs, err := expandLocalSource(source.File)
			if err != nil {
				return "", definitionError{err}
			}
			content, err := os.ReadFile(localAbs)
			if err != nil {
				return "", err
			}
			addTree([]ruleFile{{rel: filepath.Base(localAbs), content: content}})
			ids = append(ids, sha256Hex(content))
			return "file:" + source.File, nil
		case source.Dir != "":
			localAbs, err := expandLocalSource(source.Dir)
			if err != nil {
				return "", definitionError{err}
			}
			digest, err := localTreeDigest(localAbs)
			if err != nil {
				return "", err
			}
			entries, err := readLocalTree(localAbs)
			if err != nil {
				return "", err
			}
			addTree(entries)
			ids = append(ids, digest)
			return "dir:" + source.Dir, nil
		}
		parsed, err := parseItemSrc(source.Src)
		if err != nil {
			return "", definitionError{err}
		}
		mirror, err := m.fetcher.mirror(parsed.GitURL)
		if err != nil {
			return "", err
		}
		srcPath, err := resolveSrcPath(mirror, parsed)
		if err != nil {
			return "", definitionError{err}
		}
		ref := "HEAD^{tree}"
		if srcPath != "" {
			ref = "HEAD:" + srcPath
		}
		oidOut, err := git(mirror, "rev-parse", ref)
		if err != nil {
			return "", fmt.Errorf("not found: %s", shortError(err))
		}
		oid := strings.TrimSpace(oidOut)
		objType, err := git(mirror, "cat-file", "-t", oid)
		if err != nil {
			return "", err
		}
		switch strings.TrimSpace(objType) {
		case "blob":
			content, err := git(mirror, "cat-file", "-p", oid)
			if err != nil {
				return "", err
			}
			addTree([]ruleFile{{rel: path.Base(srcPath), content: []byte(content)}})
		case "tree":
			entries, err := readTree(mirror, oid)
			if err != nil {
				return "", err
			}
			addTree(entries)
		default:
			return "", fmt.Errorf("%s is neither a file nor a directory in the source repository", srcPath)
		}
		ids = append(ids, oid)
		return source.Src, nil
	})
	if err != nil {
		return nil, nil, resolved, err
	}
	rels := make([]string, 0, len(files))
	for rel := range files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	rules := make([]ruleFile, 0, len(rels))
	for _, rel := range rels {
		rules = append(rules, ruleFile{rel: rel, content: files[rel]})
	}
	return rules, ids, resolved, nil
}

// readTree reads every regular file of a git tree from the mirror.
func readTree(mirror string, treeOID string) ([]ruleFile, error) {
	cmd := exec.Command("git", "archive", treeOID)
	cmd.Dir = mirror
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var entries []ruleFile
	reader := tar.NewReader(stdout)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil, err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil, err
		}
		entries = append(entries, ruleFile{rel: filepath.ToSlash(filepath.Clean(header.Name)), content: content})
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("git archive: %s", err)
	}
	return entries, nil
}

// readLocalTree reads every regular file under a local directory.
func readLocalTree(localAbs string) ([]ruleFile, error) {
	var entries []ruleFile
	err := filepath.WalkDir(localAbs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(localAbs, p)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		entries = append(entries, ruleFile{rel: filepath.ToSlash(rel), content: content})
		return nil
	})
	return entries, err
}
