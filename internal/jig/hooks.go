package jig

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Hook is one entry of the agents block's hooks list: a command run by the
// harness on a lifecycle event, in a harness-neutral vocabulary that jig
// maps onto each harness's own events and document shape (see
// hookFormats). Only command hooks exist.
type Hook struct {
	Event    string     `json:"event"`
	Matcher  string     `json:"matcher,omitempty"` // tool-name pattern, on the tool events
	Command  string     `json:"command"`
	Timeout  int        `json:"timeout,omitempty"` // seconds
	OnlyWhen *Condition `json:"onlyWhen,omitempty"`
}

// hookEvents is the neutral vocabulary, in the order hooks are reported.
var hookEvents = []string{"sessionStart", "beforeToolUse", "afterToolUse", "beforePrompt", "stop"}

func toolEvent(event string) bool { return event == "beforeToolUse" || event == "afterToolUse" }

// claudeHookEvents maps the Claude hook file's events onto the neutral
// vocabulary; a plugin's hooks/hooks.json is written in that shape.
var claudeHookEvents = map[string]string{
	"SessionStart": "sessionStart", "PreToolUse": "beforeToolUse", "PostToolUse": "afterToolUse",
	"UserPromptSubmit": "beforePrompt", "Stop": "stop",
}

// hookFormat is how one harness document carries hooks. nested is the
// Claude schema (a matcher group holding handlers), flat puts the command
// on the element itself. events maps the neutral events onto the
// harness's; one with no entry has no equivalent and is skipped. tools
// translates the tool names of a matcher; nil passes the matcher through.
type hookFormat struct {
	nested     bool
	events     map[string]string
	tools      map[string]string
	noMatcher  bool   // the format has no tool matcher: a hook with one is skipped
	msTimeout  bool   // timeouts in milliseconds
	timeoutKey string // "" for timeout
	commandKey string // "" for command
	version    bool   // the document carries "version": 1
}

var cursorTools = map[string]string{
	"Bash": "Shell", "Edit": "Write", "Write": "Write", "MultiEdit": "Write", "Read": "Read",
	"Grep": "Grep", "Task": "Task", "WebFetch": "WebFetch", "WebSearch": "WebSearch",
}

var geminiTools = map[string]string{
	"Bash": "run_shell_command", "Edit": "replace", "MultiEdit": "replace", "Write": "write_file", "Read": "read_file",
	"Grep": "search_file_content", "Glob": "glob", "WebFetch": "web_fetch", "WebSearch": "google_web_search",
}

var claudeHookFormat = hookFormat{
	nested: true,
	events: map[string]string{"sessionStart": "SessionStart", "beforeToolUse": "PreToolUse", "afterToolUse": "PostToolUse", "beforePrompt": "UserPromptSubmit", "stop": "Stop"},
}

// hookFormats is the hooks row of the matrix, by the format name of each
// document in managedDocuments. Windsurf has no generic tool events and
// is rendered by windsurfHook.
var hookFormats = map[string]hookFormat{
	"claude": claudeHookFormat,
	"codex":  claudeHookFormat,
	"gemini": {
		nested:    true,
		events:    map[string]string{"sessionStart": "SessionStart", "beforeToolUse": "BeforeTool", "afterToolUse": "AfterTool", "beforePrompt": "BeforeAgent", "stop": "AfterAgent"},
		tools:     geminiTools,
		msTimeout: true,
	},
	"cursor": {
		events:  map[string]string{"sessionStart": "sessionStart", "beforeToolUse": "preToolUse", "afterToolUse": "postToolUse", "beforePrompt": "beforeSubmitPrompt", "stop": "stop"},
		tools:   cursorTools,
		version: true,
	},
	"copilot": {
		events:     map[string]string{"sessionStart": "sessionStart", "beforeToolUse": "preToolUse", "afterToolUse": "postToolUse", "beforePrompt": "userPromptSubmitted", "stop": "agentStop"},
		noMatcher:  true,
		timeoutKey: "timeoutSec",
		commandKey: "bash",
		version:    true,
	},
	"windsurf": {},
}

// windsurfKinds maps a tool name onto Windsurf's tool kind, which names
// the event: pre_<kind> and post_<kind>.
var windsurfKinds = map[string]string{
	"Bash": "run_command", "Edit": "write_code", "Write": "write_code", "MultiEdit": "write_code", "Read": "read_code",
}

var windsurfEvents = map[string]string{"beforePrompt": "pre_user_prompt", "stop": "post_cascade_response"}

// hookElement is one rendered element of a harness's hooks document:
// the event array it belongs to and its value.
type hookElement struct {
	event string
	value *jsonObject
}

var toolName = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// splitMatcher reads a matcher as tool-name alternatives; ok is false
// when it is a pattern, which only harnesses matching on their own names
// can take verbatim.
func splitMatcher(matcher string) (names []string, ok bool) {
	for _, name := range strings.Split(matcher, "|") {
		if !toolName.MatchString(name) {
			return nil, false
		}
		names = append(names, name)
	}
	return names, true
}

// translateMatcher rewrites the tool names of a matcher for a harness,
// leaving names it has no entry for, and patterns, as written.
func translateMatcher(matcher string, tools map[string]string) string {
	if matcher == "" || tools == nil {
		return matcher
	}
	names, ok := splitMatcher(matcher)
	if !ok {
		return matcher
	}
	var out []string
	seen := map[string]bool{}
	for _, name := range names {
		if t, found := tools[name]; found {
			name = t
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return strings.Join(out, "|")
}

// renderHook renders one hook for a format: the elements to write, or
// the reason the harness cannot take it.
func renderHook(format string, harness string, hook Hook) ([]hookElement, string) {
	if format == "windsurf" {
		return windsurfHook(harness, hook)
	}
	f := hookFormats[format]
	event, ok := f.events[hook.Event]
	if !ok {
		return nil, fmt.Sprintf("%s has no %s event", harness, hook.Event)
	}
	if hook.Matcher != "" && f.noMatcher {
		return nil, fmt.Sprintf("%s hooks have no tool matcher", harness)
	}
	handler := newJSONObject()
	if f.nested || f.commandKey != "" {
		handler.set("type", "command")
	}
	commandKey := f.commandKey
	if commandKey == "" {
		commandKey = "command"
	}
	handler.set(commandKey, hook.Command)
	if hook.Timeout > 0 {
		timeoutKey := f.timeoutKey
		if timeoutKey == "" {
			timeoutKey = "timeout"
		}
		timeout := hook.Timeout
		if f.msTimeout {
			timeout *= 1000
		}
		handler.set(timeoutKey, json.Number(fmt.Sprint(timeout)))
	}
	matcher := translateMatcher(hook.Matcher, f.tools)
	if !f.nested {
		if matcher != "" {
			handler.set("matcher", matcher)
		}
		return []hookElement{{event, handler}}, ""
	}
	group := newJSONObject()
	if matcher != "" {
		group.set("matcher", matcher)
	}
	group.set("hooks", []any{handler})
	return []hookElement{{event, group}}, ""
}

// windsurfHook renders for Windsurf, whose tool events are per tool kind
// (pre_run_command, post_write_code, ...): a tool hook lands on the kinds
// its matcher names, or on every kind without a matcher.
func windsurfHook(harness string, hook Hook) ([]hookElement, string) {
	element := func(event string) hookElement {
		o := newJSONObject()
		o.set("command", hook.Command)
		return hookElement{event, o}
	}
	if event, ok := windsurfEvents[hook.Event]; ok {
		return []hookElement{element(event)}, ""
	}
	if !toolEvent(hook.Event) {
		return nil, fmt.Sprintf("%s has no %s event", harness, hook.Event)
	}
	prefix := "pre_"
	if hook.Event == "afterToolUse" {
		prefix = "post_"
	}
	kinds := []string{"run_command", "write_code", "mcp_tool_use"}
	if prefix == "pre_" {
		kinds = append(kinds, "read_code")
	}
	if hook.Matcher != "" {
		names, ok := splitMatcher(hook.Matcher)
		if !ok {
			return nil, fmt.Sprintf("%s cannot express matcher %q", harness, hook.Matcher)
		}
		wanted := map[string]bool{}
		for _, name := range names {
			kind, found := windsurfKinds[name]
			if strings.HasPrefix(name, "mcp__") {
				kind, found = "mcp_tool_use", true
			}
			if !found || (kind == "read_code" && prefix == "post_") {
				return nil, fmt.Sprintf("%s cannot express matcher %q", harness, hook.Matcher)
			}
			wanted[kind] = true
		}
		var kept []string
		for _, kind := range kinds {
			if wanted[kind] {
				kept = append(kept, kind)
			}
		}
		kinds = kept
	}
	var elements []hookElement
	for _, kind := range kinds {
		elements = append(elements, element(prefix+kind))
	}
	return elements, ""
}

// hookIdentity names an element of a hooks array by what it does: its
// matcher and command, in any of the formats. Ownership of array elements
// goes by this identity, the way a server's goes by its name.
func hookIdentity(value any) string {
	object, ok := value.(*jsonObject)
	if !ok {
		return ""
	}
	str := func(o *jsonObject, key string) string {
		v, _ := o.get(key)
		s, _ := v.(string)
		return s
	}
	matcher := str(object, "matcher")
	command := str(object, "command")
	if command == "" {
		command = str(object, "bash")
	}
	if handlers, ok := object.get("hooks"); ok && command == "" {
		if list, ok := handlers.([]any); ok && len(list) > 0 {
			if first, ok := list[0].(*jsonObject); ok {
				command = str(first, "command")
				if command == "" {
					command = str(first, "bash")
				}
			}
		}
	}
	return sha256Hex([]byte(matcher + "\x00" + command))[:12]
}

// validateHooks checks the hooks list: known events, a command, a matcher
// on the tool events only, satisfiable conditions.
func validateHooks(result *validationResult, model *Model, hooks []Hook) {
	known := map[string]bool{}
	for _, event := range hookEvents {
		known[event] = true
	}
	for i, hook := range hooks {
		label := fmt.Sprintf("hook %d", i+1)
		if !known[hook.Event] {
			result.Errors = append(result.Errors, fmt.Sprintf("%s has an unknown event %q: use %s", label, hook.Event, strings.Join(hookEvents, ", ")))
		} else if hook.Matcher != "" && !toolEvent(hook.Event) {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: a matcher applies to beforeToolUse and afterToolUse only, not %s", label, hook.Event))
		}
		if hook.Command == "" {
			result.Errors = append(result.Errors, label+" has no command")
		}
		if hook.Timeout < 0 {
			result.Errors = append(result.Errors, label+" has a negative timeout")
		}
		if hook.OnlyWhen != nil {
			validateCondition(result, model, label, *hook.OnlyWhen)
		}
	}
}

var pluginRootRef = regexp.MustCompile(`\$\{?CLAUDE_PLUGIN_ROOT\}?`)

// parsePluginHooks reads a plugin's hooks/hooks.json, in the Claude hook
// file shape, into neutral hooks, with ${CLAUDE_PLUGIN_ROOT} pointing at
// the plugin's copy in the store. Events and handler kinds without an
// equivalent are reported as skipped.
func parsePluginHooks(data []byte, storePath string, label string) ([]Hook, []string, error) {
	var doc struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("%s: invalid hooks/hooks.json: %s", label, err)
	}
	events := make([]string, 0, len(doc.Hooks))
	for event := range doc.Hooks {
		events = append(events, event)
	}
	sort.Strings(events)
	var hooks []Hook
	var skipped []string
	for _, event := range events {
		neutral, ok := claudeHookEvents[event]
		if !ok {
			skipped = append(skipped, fmt.Sprintf("%s %s hooks: no neutral event", label, event))
			continue
		}
		for _, group := range doc.Hooks[event] {
			for _, handler := range group.Hooks {
				if handler.Type != "" && handler.Type != "command" || handler.Command == "" {
					skipped = append(skipped, fmt.Sprintf("%s %s hook: only command hooks are supported", label, event))
					continue
				}
				matcher := group.Matcher
				if !toolEvent(neutral) || matcher == "*" || matcher == ".*" {
					matcher = ""
				}
				hooks = append(hooks, Hook{
					Event:   neutral,
					Matcher: matcher,
					Command: pluginRootRef.ReplaceAllLiteralString(handler.Command, storePath),
					Timeout: handler.Timeout,
				})
			}
		}
	}
	return hooks, skipped, nil
}
