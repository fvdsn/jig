package jig

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// AgentsBlock is the schema's top-level "agents" object: the agent context
// the workspace provides, declared once without any harness path. At load
// time it expands into $dir and $file entries at the locations the
// machine's harnesses read (see expandAgents), so everything downstream
// sees ordinary tree entries.
//
// Skills, context, subagents, and commands are implemented; the other
// primitives of the spec (instructions, mcp, hooks) are refused rather
// than ignored, so a schema never silently loses them.
type AgentsBlock struct {
	Skills    SrcList // skill packs merged into the skills directory
	Context   SrcList // sections concatenated into the root context file
	Subagents SrcList // subagent files, or directories of them
	Commands  SrcList // command files, or directories of them
}

// agentsPlannedFields are spec'd primitives this jig does not implement
// yet; they get a dedicated error instead of the unknown-field one.
var agentsPlannedFields = []string{"instructions", "mcp", "hooks"}

func (block *AgentsBlock) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("agents must be an object")
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		var target *SrcList
		switch key {
		case "skills":
			target = &block.Skills
		case "context":
			target = &block.Context
		case "subagents":
			target = &block.Subagents
		case "commands":
			target = &block.Commands
		default:
			for _, planned := range agentsPlannedFields {
				if key == planned {
					return fmt.Errorf("agents.%s is not supported by this jig yet; upgrade jig or declare it with $file and $dir entries", key)
				}
			}
			return fmt.Errorf("unknown agents field %q", key)
		}
		if err := json.Unmarshal(fields[key], target); err != nil {
			return fmt.Errorf("invalid agents.%s: %s", key, err)
		}
	}
	return nil
}

// harnessAlias is one cell of the materialization matrix: where a harness
// reads a primitive when it is not the canonical location, and how the
// alias is made. A harness absent from a primitive's map reads the
// canonical location; one marked notRead has no such primitive.
type harnessAlias struct {
	path           string
	copy           bool         // a real directory for a harness that does not follow symlinks
	readsCanonical bool         // the harness reads the canonical location too; the alias is in addition
	notRead        bool         // the harness has no such primitive; nothing is derived for it
	transform      dirTransform // per-file transform into the harness's own format
}

// agentsPrimitive is one row of the materialization matrix: the canonical
// location and identity of a primitive, and its per-harness aliases.
type agentsPrimitive struct {
	name        string
	kind        EntryKind
	path        string
	identity    string
	description string
	skills      bool // dir: a source whose root is a skill lands under its own name
	items       bool // dir: a source may be a single file, landing under its basename
	aliases     map[string]harnessAlias
}

// The matrix. The canonical locations follow the harnesses' convergence on
// .agents/ and AGENTS.md. Claude Code's symlink handling has regressed
// repeatedly, so its skills alias is a copy; the rest are links. Copilot
// reads AGENTS.md as well as its own instructions file. Subagents and
// commands have no shared location yet: every harness reads its own
// directory, and Codex subagents and Gemini commands are TOML.
var agentsPrimitives = []agentsPrimitive{
	{
		name: "skills", kind: EntryDir, path: ".agents/skills", identity: "agents-skills",
		description: "Skills from the agents block", skills: true,
		aliases: map[string]harnessAlias{
			"claude": {path: ".claude/skills", copy: true},
			"kiro":   {path: ".kiro/skills", copy: true},
		},
	},
	{
		name: "context", kind: EntryFile, path: "AGENTS.md", identity: "agents-context",
		description: "Root context from the agents block",
		aliases: map[string]harnessAlias{
			"claude":  {path: "CLAUDE.md"},
			"copilot": {path: ".github/copilot-instructions.md", readsCanonical: true},
			"gemini":  {path: "GEMINI.md"},
		},
	},
	{
		name: "subagents", kind: EntryDir, path: ".agents/agents", identity: "agents-subagents",
		description: "Subagents from the agents block", items: true,
		aliases: map[string]harnessAlias{
			"claude":   {path: ".claude/agents", copy: true},
			"codex":    {path: ".codex/agents", copy: true, transform: codexAgentTransform},
			"copilot":  {path: ".github/agents", copy: true, transform: copilotAgentTransform},
			"cursor":   {path: ".cursor/agents", copy: true},
			"opencode": {path: ".opencode/agents", copy: true},
			"gemini":   {notRead: true},
			"windsurf": {notRead: true},
			"kiro":     {path: ".kiro/agents", copy: true},
		},
	},
	{
		name: "commands", kind: EntryDir, path: ".agents/commands", identity: "agents-commands",
		description: "Commands from the agents block", items: true,
		aliases: map[string]harnessAlias{
			"claude":   {path: ".claude/commands", copy: true},
			"codex":    {notRead: true},
			"copilot":  {notRead: true},
			"cursor":   {path: ".cursor/commands", copy: true},
			"opencode": {path: ".opencode/commands", copy: true},
			"gemini":   {path: ".gemini/commands", copy: true, transform: geminiCommandTransform},
			"windsurf": {path: ".windsurf/workflows", copy: true},
			"kiro":     {notRead: true},
		},
	},
}

// Canonical locations of the first two primitives, referenced by name
// elsewhere.
const (
	agentsSkillsPath      = ".agents/skills"
	agentsSkillsIdentity  = "agents-skills"
	agentsContextPath     = "AGENTS.md"
	agentsContextIdentity = "agents-context"
)

func (p agentsPrimitive) sources(block *AgentsBlock) SrcList {
	switch p.name {
	case "skills":
		return block.Skills
	case "context":
		return block.Context
	case "subagents":
		return block.Subagents
	case "commands":
		return block.Commands
	}
	return nil
}

// canonicalRead reports whether the canonical location of a primitive is
// read on this machine: by a listed harness that has no alias or reads
// both, or by whatever comes along when no harness is listed at all.
func canonicalRead(harnesses []string, aliases map[string]harnessAlias) bool {
	if len(harnesses) == 0 {
		return true
	}
	for _, harness := range harnesses {
		if alias, ok := aliases[harness]; !ok || alias.readsCanonical {
			return true
		}
	}
	return false
}

// expandAgents derives the block's tree entries into the model, one per
// location something on this machine reads: the canonical entry when a
// listed harness reads it (or none is listed), and for each listed harness
// that reads elsewhere either an alias of the canonical entry or, when the
// canonical entry is not derived, the real entry at the harness's own
// path. A Claude-only machine thus gets .claude/skills and CLAUDE.md and
// nothing else, with no duplicate for its search tools to find.
//
// A derived path the tree also declares is an error: the explicit form and
// the block cannot both own a path. Identities are fixed per path so the
// tree can link to them, --id can name them, and a changed harness list
// converges in place.
func expandAgents(block *AgentsBlock, harnesses []string, model *Model) error {
	if block == nil {
		return nil
	}
	harnesses = sortHarnesses(harnesses)
	for _, primitive := range agentsPrimitives {
		sources := primitive.sources(block)
		if len(sources) == 0 {
			continue
		}
		canonical := canonicalRead(harnesses, primitive.aliases)
		if canonical {
			if err := addDerived(model, primitive.entry(primitive.path, primitive.identity, primitive.description, sources, nil, harnessAlias{})); err != nil {
				return err
			}
		}
		for _, harness := range harnesses {
			alias, ok := primitive.aliases[harness]
			if !ok || alias.notRead {
				continue
			}
			var target *Ref
			if canonical {
				target = &Ref{ID: primitive.identity}
			}
			identity := primitive.identity + "-" + harness
			description := fmt.Sprintf("%s, as %s reads them", primitive.description, harness)
			if primitive.kind == EntryFile {
				description = fmt.Sprintf("%s, as %s reads it", primitive.description, harness)
			}
			if err := addDerived(model, primitive.entry(alias.path, identity, description, sources, target, alias)); err != nil {
				return err
			}
		}
	}
	return nil
}

// entry builds one derived entry: the real entry with the sources attached
// when target is nil, otherwise an alias of the target (a copy where the
// alias says so, a link otherwise).
func (p agentsPrimitive) entry(entryPath string, identity string, description string, sources SrcList, target *Ref, alias harnessAlias) Entry {
	entry := Entry{Path: entryPath, Identity: identity, Kind: p.kind}
	switch p.kind {
	case EntryDir:
		dir := &Dir{ID: identity, Description: description, skills: p.skills, items: p.items, transform: alias.transform}
		switch {
		case target == nil:
			dir.Src = sources
		case alias.copy:
			dir.Copy = target
		default:
			dir.Link = target
		}
		entry.Dir = dir
	case EntryFile:
		file := &File{ID: identity, Description: description}
		switch {
		case target == nil:
			file.Src = sources
		case alias.copy:
			file.Copy = target
		default:
			file.Link = target
		}
		entry.File = file
	}
	return entry
}

func addDerived(model *Model, entry Entry) error {
	if _, declared := model.Entries[entry.Path]; declared {
		return fmt.Errorf("the agents block derives %s, which the tree also declares; remove one of them", entry.Path)
	}
	model.Entries[entry.Path] = entry
	return nil
}

// dirTransform converts one file of a derived directory into a harness's
// own format: it returns the path to write and the content. Files that do
// not need converting (anything that is not markdown) pass through.
type dirTransform func(rel string, content []byte) (string, []byte)

// markdownStem splits a markdown path into its directory, stem, and
// whether it is markdown at all. A ".agent.md" or ".prompt.md" suffix is
// part of the extension, so the stem is the bare name.
func markdownStem(rel string) (dir string, stem string, ok bool) {
	base := path.Base(rel)
	stem, ok = strings.CutSuffix(base, ".md")
	if !ok {
		return "", "", false
	}
	stem = strings.TrimSuffix(strings.TrimSuffix(stem, ".agent"), ".prompt")
	dir = path.Dir(rel)
	if dir == "." {
		dir = ""
	} else {
		dir += "/"
	}
	return dir, stem, true
}

// copilotAgentTransform names a subagent file the way Copilot discovers
// it, <name>.agent.md, leaving the content alone.
func copilotAgentTransform(rel string, content []byte) (string, []byte) {
	dir, stem, ok := markdownStem(rel)
	if !ok {
		return rel, content
	}
	return dir + stem + ".agent.md", content
}

// codexAgentTransform writes a subagent as Codex's TOML: name,
// description, and native model settings from the frontmatter, the body as
// developer_instructions. Other frontmatter (tools, user-invocable) has no
// Codex equivalent and is dropped, as APM does.
func codexAgentTransform(rel string, content []byte) (string, []byte) {
	dir, stem, ok := markdownStem(rel)
	if !ok {
		return rel, content
	}
	fm, body := splitFrontmatter(content)
	name := stem
	if fm.get("name") != "" {
		name = fm.get("name")
	}
	fields := []tomlField{{"name", name}, {"description", fm.get("description")}}
	for _, key := range []string{"model", "model_reasoning_effort"} {
		if fm.has(key) {
			fields = append(fields, tomlField{key, fm.get(key)})
		}
	}
	fields = append(fields, tomlField{"developer_instructions", strings.TrimSpace(body)})
	return dir + stem + ".toml", tomlDocument(fields)
}

var positionalArgument = regexp.MustCompile(`(^|[^\d$])\$\d+`)

// geminiCommandTransform writes a command as Gemini CLI's TOML: the
// description from the frontmatter and the body as the prompt, with
// $ARGUMENTS rewritten to Gemini's {{args}}. A prompt using positional
// $1-style arguments gets the whole argument string prepended, since
// Gemini has no positional form.
func geminiCommandTransform(rel string, content []byte) (string, []byte) {
	dir, stem, ok := markdownStem(rel)
	if !ok {
		return rel, content
	}
	fm, body := splitFrontmatter(content)
	prompt := strings.ReplaceAll(strings.TrimSpace(body), "$ARGUMENTS", "{{args}}")
	if positionalArgument.MatchString(prompt) {
		prompt = "Arguments: {{args}}\n\n" + prompt
	}
	var fields []tomlField
	if fm.get("description") != "" {
		fields = append(fields, tomlField{"description", fm.get("description")})
	}
	fields = append(fields, tomlField{"prompt", prompt})
	return dir + stem + ".toml", tomlDocument(fields)
}
