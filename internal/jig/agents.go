package jig

import (
	"encoding/json"
	"fmt"
	"sort"
)

// AgentsBlock is the schema's top-level "agents" object: the agent context
// the workspace provides, declared once without any harness path. At load
// time it expands into $dir and $file entries at the canonical locations
// plus an alias per configured harness that reads elsewhere (see
// expandAgents), so everything downstream sees ordinary tree entries.
//
// Only skills and context are implemented so far; the other primitives of
// the spec (instructions, subagents, commands, mcp, hooks) are refused
// rather than ignored, so a schema never silently loses them.
type AgentsBlock struct {
	Skills  SrcList // skill packs merged into the canonical skills directory
	Context SrcList // sections concatenated into the root context file
}

// agentsPlannedFields are spec'd primitives this jig does not implement
// yet; they get a dedicated error instead of the unknown-field one.
var agentsPlannedFields = []string{"instructions", "subagents", "commands", "mcp", "hooks"}

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
		switch key {
		case "skills":
			if err := json.Unmarshal(fields[key], &block.Skills); err != nil {
				return fmt.Errorf("invalid agents.skills: %s", err)
			}
		case "context":
			if err := json.Unmarshal(fields[key], &block.Context); err != nil {
				return fmt.Errorf("invalid agents.context: %s", err)
			}
		default:
			for _, planned := range agentsPlannedFields {
				if key == planned {
					return fmt.Errorf("agents.%s is not supported by this jig yet; upgrade jig or declare it with $file and $dir entries", key)
				}
			}
			return fmt.Errorf("unknown agents field %q", key)
		}
	}
	return nil
}

// Canonical locations and fixed identities of the derived entries.
const (
	agentsSkillsPath      = ".agents/skills"
	agentsSkillsIdentity  = "agents-skills"
	agentsContextPath     = "AGENTS.md"
	agentsContextIdentity = "agents-context"
)

// harnessAlias is one cell of the materialization matrix: where a harness
// reads a primitive when it is not the canonical location, and how the
// alias is made. A harness absent from a primitive's map reads the
// canonical location.
type harnessAlias struct {
	path           string
	copy           bool // a real directory for a harness that does not follow symlinks
	readsCanonical bool // the harness reads the canonical location too; the alias is in addition
}

// skillAliases and contextAliases are the skills and context rows of the
// materialization matrix. Claude Code's symlink handling has regressed
// repeatedly, so its skills alias is a copy; the rest are links. Copilot
// reads AGENTS.md as well as its own instructions file.
var skillAliases = map[string]harnessAlias{
	"claude": {path: ".claude/skills", copy: true},
	"kiro":   {path: ".kiro/skills", copy: true},
}

var contextAliases = map[string]harnessAlias{
	"claude":  {path: "CLAUDE.md"},
	"copilot": {path: ".github/copilot-instructions.md", readsCanonical: true},
	"gemini":  {path: "GEMINI.md"},
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
	add := func(entry Entry) error {
		if _, declared := model.Entries[entry.Path]; declared {
			return fmt.Errorf("the agents block derives %s, which the tree also declares; remove one of them", entry.Path)
		}
		model.Entries[entry.Path] = entry
		return nil
	}
	harnesses = sortHarnesses(harnesses)

	if len(block.Skills) > 0 {
		canonical := canonicalRead(harnesses, skillAliases)
		if canonical {
			if err := add(Entry{Path: agentsSkillsPath, Identity: agentsSkillsIdentity, Kind: EntryDir, Dir: &Dir{
				ID:          agentsSkillsIdentity,
				Src:         block.Skills,
				Description: "Skills from the agents block",
				skills:      true,
			}}); err != nil {
				return err
			}
		}
		for _, harness := range harnesses {
			alias, ok := skillAliases[harness]
			if !ok {
				continue
			}
			dir := &Dir{ID: agentsSkillsIdentity + "-" + harness, Description: "Skills from the agents block, as " + harness + " reads them"}
			switch {
			case !canonical:
				dir.Src, dir.skills = block.Skills, true
			case alias.copy:
				dir.Copy = &Ref{ID: agentsSkillsIdentity}
			default:
				dir.Link = &Ref{ID: agentsSkillsIdentity}
			}
			if err := add(Entry{Path: alias.path, Identity: dir.ID, Kind: EntryDir, Dir: dir}); err != nil {
				return err
			}
		}
	}

	if len(block.Context) > 0 {
		canonical := canonicalRead(harnesses, contextAliases)
		if canonical {
			if err := add(Entry{Path: agentsContextPath, Identity: agentsContextIdentity, Kind: EntryFile, File: &File{
				ID:          agentsContextIdentity,
				Src:         block.Context,
				Description: "Root context from the agents block",
			}}); err != nil {
				return err
			}
		}
		for _, harness := range harnesses {
			alias, ok := contextAliases[harness]
			if !ok {
				continue
			}
			file := &File{ID: agentsContextIdentity + "-" + harness, Description: "Root context from the agents block, as " + harness + " reads it"}
			switch {
			case !canonical:
				file.Src = block.Context
			case alias.copy:
				file.Copy = &Ref{ID: agentsContextIdentity}
			default:
				file.Link = &Ref{ID: agentsContextIdentity}
			}
			if err := add(Entry{Path: alias.path, Identity: file.ID, Kind: EntryFile, File: file}); err != nil {
				return err
			}
		}
	}
	return nil
}
