package cli

import (
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/fvdsn/jig/internal/jig"
)

// Run executes one command line, writing its output to out. Progress lines
// and the diff pager go to the process stderr and terminal directly.
func Run(args []string, out io.Writer) error {
	if len(args) == 0 {
		printUsage(out)
		return nil
	}

	if args[0] == "help" && len(args) > 1 {
		return printCommandHelp(out, args[1])
	}
	if _, known := commandDocFor(args[0]); known && wantsHelp(args[1:]) {
		return printCommandHelp(out, args[0])
	}

	switch args[0] {
	case "init":
		return cmdInit(args[1:], out)
	case "validate":
		return cmdValidate(args[1:], out)
	case "list":
		return cmdList(args[1:], out)
	case "tags":
		return cmdTags(args[1:], out)
	case "info":
		return cmdInfo(args[1:], out)
	case "deps":
		return cmdDeps(args[1:], out)
	case "graph":
		return cmdGraph(args[1:], out)
	case "clone":
		return cmdClone(args[1:], out)
	case "sync":
		return cmdSync(args[1:], out)
	case "setup":
		return cmdBare("setup", jig.Setup, args[1:], out)
	case "fmt":
		return cmdBare("fmt", jig.Fmt, args[1:], out)
	case "lint":
		return cmdBare("lint", jig.Lint, args[1:], out)
	case "test":
		return cmdBare("test", jig.Test, args[1:], out)
	case "pull":
		return cmdBare("pull", jig.Pull, args[1:], out)
	case "fetch":
		return cmdBare("fetch", jig.Fetch, args[1:], out)
	case "push":
		return cmdPush(args[1:], out)
	case "checkout":
		return cmdCheckout(args[1:], out)
	case "rm":
		return cmdRemove(args[1:], out)
	case "status":
		return cmdStatus(args[1:], out)
	case "diff":
		return cmdDiff(args[1:], out)
	case "update":
		return cmdUpdate(args[1:], out)
	case "cache":
		return cmdCache(args[1:], out)
	case "version", "--version":
		fmt.Fprintln(out, versionString())
		return nil
	case "help", "--help", "-h":
		printUsage(out)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// buildVersion is stamped by the release workflow via -ldflags; go install
// builds leave it empty and rely on the module version instead.
var buildVersion string

// versionString reports the release version, the module version embedded by
// go install, or the VCS revision for local builds.
func versionString() string {
	if buildVersion != "" {
		return "jig " + buildVersion
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "jig (unknown version)"
	}
	version := info.Main.Version
	if version != "" && version != "(devel)" {
		return "jig " + version
	}
	revision := ""
	modified := ""
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			if setting.Value == "true" {
				modified = ", modified"
			}
		}
	}
	if revision == "" {
		return "jig (devel)"
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	return fmt.Sprintf("jig (devel, %s%s)", revision, modified)
}

type parsedArgs struct {
	Positionals []string
	Values      map[string]string
	Flags       map[string]bool
}

type flagKind int

const (
	boolFlag flagKind = iota
	valueFlag
	optionalValueFlag // set as a flag; also takes a value when the next arg is not a flag
)

func parseArgs(args []string, flags map[string]flagKind) (parsedArgs, error) {
	parsed := parsedArgs{Values: map[string]string{}, Flags: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		kind, known := flags[arg]
		if !known {
			if strings.HasPrefix(arg, "-") {
				return parsed, fmt.Errorf("unknown flag %s", arg)
			}
			parsed.Positionals = append(parsed.Positionals, arg)
			continue
		}
		switch kind {
		case valueFlag:
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return parsed, fmt.Errorf("%s requires a value", arg)
			}
			parsed.Values[arg] = args[i+1]
			i++
		case optionalValueFlag:
			parsed.Flags[arg] = true
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				parsed.Values[arg] = args[i+1]
				i++
			}
		default:
			parsed.Flags[arg] = true
		}
	}
	return parsed, nil
}

// selectorFlags are the entry selectors every scoped command accepts,
// mapped onto jig.Selector by selectorFrom.
var selectorFlags = map[string]flagKind{"--archived": boolFlag, "--tags": valueFlag, "--id": valueFlag}

// withSelectorFlags returns a command's flag map: the shared selectors plus
// its own.
func withSelectorFlags(own map[string]flagKind) map[string]flagKind {
	flags := map[string]flagKind{}
	for name, kind := range selectorFlags {
		flags[name] = kind
	}
	for name, kind := range own {
		flags[name] = kind
	}
	return flags
}

// selectorFrom builds the selection from the path positionals (at most one)
// and the selector flags, enforcing their exclusivity: --id selects one
// entry by identity and cannot be combined with a path or --tags.
func selectorFrom(name string, parsed parsedArgs, pathPositionals []string) (jig.Selector, error) {
	if len(pathPositionals) > 1 {
		return jig.Selector{}, usageError(name)
	}
	if parsed.Values["--id"] != "" && (len(pathPositionals) > 0 || parsed.Values["--tags"] != "") {
		return jig.Selector{}, usageError(name)
	}
	return jig.Selector{
		Path:            optionalPath(pathPositionals),
		ID:              parsed.Values["--id"],
		IncludeArchived: parsed.Flags["--archived"],
		Tags:            parseTags(parsed.Values["--tags"]),
	}, nil
}

// commandFlags is every command's flag map, so the usage strings in
// commandDocs can be checked against what the parser accepts. The schema
// commands take the selectors; the commands that act on checkouts take
// none, since their scope is the directory they run in.
var commandFlags = map[string]map[string]flagKind{
	"list":     withSelectorFlags(map[string]flagKind{"--meta": valueFlag}),
	"tags":     withSelectorFlags(nil),
	"info":     withSelectorFlags(nil),
	"deps":     withSelectorFlags(map[string]flagKind{"--reverse": boolFlag, "--with-optional-deps": boolFlag}),
	"graph":    withSelectorFlags(nil),
	"clone":    withSelectorFlags(map[string]flagKind{"--with-optional-deps": boolFlag, "--no-deps": boolFlag}),
	"sync":     {"--no-update": boolFlag, "--with-optional-deps": boolFlag, "--no-deps": boolFlag, "--prune": boolFlag},
	"setup":    {},
	"fmt":      {},
	"lint":     {},
	"test":     {},
	"pull":     {},
	"fetch":    {},
	"push":     {"-u": boolFlag},
	"checkout": {"-b": boolFlag, "--default": boolFlag},
	"status":   {"--all": boolFlag},
	"diff":     {"--stat": boolFlag},
}

var initFlags = map[string]flagKind{
	"--path":               valueFlag,
	"--clone":              optionalValueFlag,
	"--no-deps":            boolFlag,
	"--with-optional-deps": boolFlag,
	"--archived":           boolFlag,
	"--tags":               valueFlag,
}

// checkDepsFlags rejects the contradictory dependency selectors.
func checkDepsFlags(parsed parsedArgs) error {
	if parsed.Flags["--no-deps"] && parsed.Flags["--with-optional-deps"] {
		return errors.New("--no-deps and --with-optional-deps are mutually exclusive")
	}
	return nil
}

// commandDoc is the single source of truth for a command's usage and
// description: the overview, per-command help, and usage errors all derive
// from it so they cannot drift apart.
type commandDoc struct {
	name        string
	usages      []string
	description []string
}

var commandDocs = []commandDoc{
	{"init",
		[]string{"init [git-url-or-file [workspace-dir]] [--path <path>] [--clone [path]] [--no-deps] [--with-optional-deps] [--archived] [--tags a,b]"},
		[]string{
			"Initialize a workspace: clone the schema repository into .jig/source, optionally cloning a path.",
			"With no arguments, start a fresh workspace here with a starter schema in .jig/source.",
		}},
	{"validate",
		[]string{"validate [schema-file]"},
		[]string{"Validate the current workspace schema, or a schema file given by path."}},
	{"list",
		[]string{"list [path] [--archived] [--tags a,b] [--meta key[=value]] [--id x]"},
		[]string{"List groups, repositories, files, and dirs defined in the schema."}},
	{"tags",
		[]string{"tags [path] [--archived] [--tags a,b] [--id x]"},
		[]string{"List the tags carried by entries matching a path, with entry counts."}},
	{"info",
		[]string{"info <path|--id x> [--archived] [--tags a,b]"},
		[]string{"Show repository, file, or group metadata."}},
	{"deps",
		[]string{"deps <path|--id x> [--reverse] [--with-optional-deps] [--archived] [--tags a,b]"},
		[]string{"Show expanded recursive dependencies for repositories matching a path; --reverse shows the direct dependents instead."}},
	{"graph",
		[]string{"graph [path] [--archived] [--tags a,b] [--id x]"},
		[]string{"Print the repository dependency graph as a Mermaid flowchart; group dependencies point at subgraphs, optional edges are dashed."}},
	{"clone",
		[]string{"clone [path] [--no-deps] [--with-optional-deps] [--archived] [--tags a,b] [--id x]"},
		[]string{"Clone/materialize all entries, or repositories/files matching a path. --no-deps skips dependencies."}},
	{"sync",
		[]string{"sync [--no-update] [--no-deps] [--with-optional-deps] [--prune]"},
		[]string{
			"Update the schema, then converge the installed entries under the current directory: clone missing dependencies, move renamed repos/files, update origins/files, and refresh local state.",
			"--no-update applies the current schema without fetching; --prune deletes entries removed from the schema (dirty/unpushed repos and modified files are kept).",
		}},
	{"setup",
		[]string{"setup"},
		[]string{"Run each repository's schema-declared setup command in dependency order, making fresh checkouts usable."}},
	{"fmt",
		[]string{"fmt"},
		[]string{"Run each repository's schema-declared fmt command in the installed repositories, in parallel."}},
	{"lint",
		[]string{"lint"},
		[]string{"Run each repository's schema-declared lint command in the installed repositories, in parallel."}},
	{"test",
		[]string{"test"},
		[]string{"Run each repository's schema-declared test command in the installed repositories, in parallel."}},
	{"pull",
		[]string{"pull"},
		[]string{"Run git pull --ff-only in the installed repositories."}},
	{"fetch",
		[]string{"fetch"},
		[]string{"Run git fetch in the installed repositories."}},
	{"push",
		[]string{"push [-u]"},
		[]string{"Run git push (never forced) in the installed repositories; -u sets the upstream on branches that have none."}},
	{"checkout",
		[]string{
			"checkout [-b] <branch>",
			"checkout --default",
		},
		[]string{"Switch the installed repositories to a branch; -b creates it, --default switches each repo to its remote's default branch. Repos where the switch would lose local changes are skipped."}},
	{"rm",
		[]string{"rm <path>... [-r|--recursive] [-f|--force]"},
		[]string{"Uninstall repositories, files, or dirs: delete the checkout and stop tracking it. -r removes groups, -f overrides dirty/unpushed checks."}},
	{"status",
		[]string{"status [--all]"},
		[]string{"Show the state of the installed entries; repos never installed are only counted unless --all is given."}},
	{"diff",
		[]string{"diff [--stat]"},
		[]string{
			"Show uncommitted changes (staged and unstaged) in the installed repositories as one workspace-wide unified diff with workspace-relative paths.",
			"--stat prints a one-line summary per dirty repository instead of the patch.",
		}},
	{"update",
		[]string{"update"},
		[]string{
			"Fast-forward the schema checkout (.jig/source) from its remote without changing local checkouts.",
			"Review the reported changes, then apply them with jig sync.",
		}},
	{"cache",
		[]string{
			"cache",
			"cache clean [--unused <days>[d]]",
		},
		[]string{
			"Show the clone cache location, mirror count, and size.",
			"cache clean removes cached mirrors, optionally only those unused for at least <days> days.",
		}},
	{"version",
		[]string{"version"},
		[]string{"Print the jig version."}},
}

func commandDocFor(name string) (commandDoc, bool) {
	for _, doc := range commandDocs {
		if doc.name == name {
			return doc, true
		}
	}
	return commandDoc{}, false
}

// usageError derives a command's usage error from its doc.
func usageError(name string) error {
	doc, ok := commandDocFor(name)
	if !ok {
		return fmt.Errorf("unknown command %q", name)
	}
	forms := make([]string, len(doc.usages))
	for i, usage := range doc.usages {
		forms[i] = "jig " + usage
	}
	return errors.New("usage: " + strings.Join(forms, " | "))
}

// printCommandHelp prints one command's usage and description.
func printCommandHelp(out io.Writer, name string) error {
	doc, ok := commandDocFor(name)
	if !ok {
		return fmt.Errorf("unknown command %q", name)
	}
	for _, usage := range doc.usages {
		fmt.Fprintln(out, "usage: jig "+usage)
	}
	for _, line := range doc.description {
		fmt.Fprintln(out, "  "+line)
	}
	return nil
}

// wantsHelp reports whether the command arguments ask for help.
func wantsHelp(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "Jig is a workspace CLI for managing many related Git repositories and generated files from a shared schema.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  jig <command> [args]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Commands:")
	for _, doc := range commandDocs {
		for _, usage := range doc.usages {
			fmt.Fprintln(out, "  "+usage)
		}
		for _, line := range doc.description {
			fmt.Fprintln(out, "      "+line)
		}
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "The schema commands (list, tags, info, deps, graph, clone) select entries that may not be on disk yet:")
	fmt.Fprintln(out, "paths identify repositories, files, or groups using slash paths such as services/checkout or platform;")
	fmt.Fprintln(out, "--tags a,b keeps only entries carrying all the listed tags; tags on groups are inherited by their children;")
	fmt.Fprintln(out, "--meta key[=value] keeps only entries whose meta carries the key (and, when given, the exact value).")
	fmt.Fprintln(out, "The other commands act on the installed entries under the current directory; cd to narrow their scope.")
}

func cmdInit(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, initFlags)
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 2 {
		return usageError("init")
	}
	// A bare init starts a fresh workspace in the current directory and
	// clones immediately, so the starter schema materializes right away.
	bare := len(parsed.Positionals) == 0
	if !bare && !parsed.Flags["--clone"] && (parsed.Flags["--no-deps"] || parsed.Flags["--with-optional-deps"] || parsed.Flags["--archived"] || parsed.Values["--tags"] != "") {
		return errors.New("--no-deps, --with-optional-deps, --archived, and --tags require --clone")
	}
	if err := checkDepsFlags(parsed); err != nil {
		return err
	}

	options := jig.InitOptions{
		WorkspaceDir:    ".",
		SchemaPath:      parsed.Values["--path"],
		Clone:           parsed.Flags["--clone"] || bare,
		ClonePath:       parsed.Values["--clone"],
		IncludeOptional: parsed.Flags["--with-optional-deps"],
		IncludeArchived: parsed.Flags["--archived"],
		SkipDeps:        parsed.Flags["--no-deps"],
		Tags:            parseTags(parsed.Values["--tags"]),
	}
	if !bare {
		options.SourceArg = parsed.Positionals[0]
	}
	if len(parsed.Positionals) == 2 {
		options.WorkspaceDir = parsed.Positionals[1]
	}
	return jig.Init(options, out)
}

func cmdValidate(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, nil)
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 1 {
		return usageError("validate")
	}
	return jig.Validate(jig.ValidateOptions{
		File: optionalPath(parsed.Positionals),
	}, out)
}

func cmdList(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, commandFlags["list"])
	if err != nil {
		return err
	}
	selector, err := selectorFrom("list", parsed, parsed.Positionals)
	if err != nil {
		return err
	}
	return jig.List(jig.ListOptions{
		Selector: selector,
		Meta:     parseMetaFilter(parsed.Values["--meta"]),
	}, out)
}

func cmdTags(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, commandFlags["tags"])
	if err != nil {
		return err
	}
	selector, err := selectorFrom("tags", parsed, parsed.Positionals)
	if err != nil {
		return err
	}
	return jig.Tags(jig.TagsOptions{Selector: selector}, out)
}

func cmdInfo(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, commandFlags["info"])
	if err != nil {
		return err
	}
	if len(parsed.Positionals) == 0 && parsed.Values["--id"] == "" {
		return usageError("info")
	}
	selector, err := selectorFrom("info", parsed, parsed.Positionals)
	if err != nil {
		return err
	}
	return jig.Info(jig.InfoOptions{Selector: selector}, out)
}

func cmdDeps(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, commandFlags["deps"])
	if err != nil {
		return err
	}
	if len(parsed.Positionals) == 0 && parsed.Values["--id"] == "" {
		return usageError("deps")
	}
	selector, err := selectorFrom("deps", parsed, parsed.Positionals)
	if err != nil {
		return err
	}
	return jig.Dependencies(jig.DependenciesOptions{
		Selector:        selector,
		Reverse:         parsed.Flags["--reverse"],
		IncludeOptional: parsed.Flags["--with-optional-deps"],
	}, out)
}

func cmdGraph(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, commandFlags["graph"])
	if err != nil {
		return err
	}
	selector, err := selectorFrom("graph", parsed, parsed.Positionals)
	if err != nil {
		return err
	}
	return jig.Graph(jig.GraphOptions{Selector: selector}, out)
}

func cmdClone(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, commandFlags["clone"])
	if err != nil {
		return err
	}
	selector, err := selectorFrom("clone", parsed, parsed.Positionals)
	if err != nil {
		return err
	}
	if err := checkDepsFlags(parsed); err != nil {
		return err
	}
	return jig.Clone(jig.CloneOptions{
		Selector:        selector,
		IncludeOptional: parsed.Flags["--with-optional-deps"],
		SkipDeps:        parsed.Flags["--no-deps"],
	}, out)
}

func cmdSync(args []string, out io.Writer) error {
	parsed, err := parseBare("sync", args)
	if err != nil {
		return err
	}
	if err := checkDepsFlags(parsed); err != nil {
		return err
	}
	return jig.Sync(jig.SyncOptions{
		IncludeOptional: parsed.Flags["--with-optional-deps"],
		SkipDeps:        parsed.Flags["--no-deps"],
		SkipUpdate:      parsed.Flags["--no-update"],
		Prune:           parsed.Flags["--prune"],
	}, out)
}

// parseBare parses the arguments of a command that acts on the checkouts
// under the current directory: its own flags only, no positionals.
func parseBare(name string, args []string) (parsedArgs, error) {
	parsed, err := parseArgs(args, commandFlags[name])
	if err != nil {
		return parsed, err
	}
	if len(parsed.Positionals) > 0 {
		return parsed, usageError(name)
	}
	return parsed, nil
}

// cmdBare dispatches a command that takes no arguments at all.
func cmdBare(name string, run func(io.Writer) error, args []string, out io.Writer) error {
	if _, err := parseBare(name, args); err != nil {
		return err
	}
	return run(out)
}

func cmdCache(args []string, out io.Writer) error {
	if len(args) == 0 {
		return jig.CacheInfo(out)
	}
	if args[0] != "clean" {
		return usageError("cache")
	}
	parsed, err := parseArgs(args[1:], map[string]flagKind{"--unused": valueFlag})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 0 {
		return usageError("cache")
	}
	days := -1
	if value := parsed.Values["--unused"]; value != "" {
		days, err = strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err != nil || days < 0 {
			return errors.New("--unused requires a number of days")
		}
	}
	return jig.CacheClean(jig.CacheCleanOptions{UnusedDays: days}, out)
}

func cmdPush(args []string, out io.Writer) error {
	parsed, err := parseBare("push", args)
	if err != nil {
		return err
	}
	return jig.Push(jig.PushOptions{SetUpstream: parsed.Flags["-u"]}, out)
}

func cmdCheckout(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, commandFlags["checkout"])
	if err != nil {
		return err
	}
	// With --default there is no branch positional (and nothing to create).
	branch := ""
	if parsed.Flags["--default"] {
		if parsed.Flags["-b"] || len(parsed.Positionals) > 0 {
			return usageError("checkout")
		}
	} else {
		if len(parsed.Positionals) != 1 {
			return usageError("checkout")
		}
		branch = parsed.Positionals[0]
	}
	return jig.Checkout(jig.CheckoutOptions{
		Branch:  branch,
		Default: parsed.Flags["--default"],
		Create:  parsed.Flags["-b"],
	}, out)
}

func cmdRemove(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, map[string]flagKind{
		"-r": boolFlag, "--recursive": boolFlag,
		"-f": boolFlag, "--force": boolFlag,
	})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) == 0 {
		return usageError("rm")
	}
	return jig.Remove(jig.RemoveOptions{
		Paths:     parsed.Positionals,
		Recursive: parsed.Flags["-r"] || parsed.Flags["--recursive"],
		Force:     parsed.Flags["-f"] || parsed.Flags["--force"],
	}, out)
}

func cmdStatus(args []string, out io.Writer) error {
	parsed, err := parseBare("status", args)
	if err != nil {
		return err
	}
	return jig.Status(jig.StatusOptions{All: parsed.Flags["--all"]}, out)
}

func cmdDiff(args []string, out io.Writer) error {
	parsed, err := parseBare("diff", args)
	if err != nil {
		return err
	}
	return jig.Diff(jig.DiffOptions{Stat: parsed.Flags["--stat"]}, out)
}

func cmdUpdate(args []string, out io.Writer) error {
	// --sync is a legacy alias for jig sync, which now updates the schema
	// itself before applying it; the flag keeps working undocumented.
	parsed, err := parseArgs(args, map[string]flagKind{"--sync": boolFlag, "--with-optional-deps": boolFlag, "--no-deps": boolFlag, "--prune": boolFlag})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 0 {
		return usageError("update")
	}
	if !parsed.Flags["--sync"] && (parsed.Flags["--no-deps"] || parsed.Flags["--with-optional-deps"] || parsed.Flags["--prune"]) {
		return errors.New("--no-deps, --with-optional-deps, and --prune require --sync")
	}
	if err := checkDepsFlags(parsed); err != nil {
		return err
	}
	return jig.Update(jig.UpdateOptions{
		Sync:            parsed.Flags["--sync"],
		IncludeOptional: parsed.Flags["--with-optional-deps"],
		SkipDeps:        parsed.Flags["--no-deps"],
		Prune:           parsed.Flags["--prune"],
	}, out)
}

func optionalPath(positionals []string) string {
	if len(positionals) == 0 {
		return ""
	}
	return positionals[0]
}

func parseTags(value string) []string {
	var tags []string
	for _, tag := range strings.Split(value, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

// parseMetaFilter parses a --meta filter: a bare key matches entries
// carrying the key, key=value additionally requires the exact value.
func parseMetaFilter(value string) jig.MetaFilter {
	if value == "" {
		return jig.MetaFilter{}
	}
	key, val, hasValue := strings.Cut(value, "=")
	return jig.MetaFilter{Key: key, Value: val, HasValue: hasValue}
}
