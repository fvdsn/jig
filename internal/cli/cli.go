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

func Run(args []string, out io.Writer, _ io.Writer) error {
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
	case "info":
		return cmdInfo(args[1:], out)
	case "deps":
		return cmdDeps(args[1:], out)
	case "clone":
		return cmdClone(args[1:], out)
	case "sync":
		return cmdSync(args[1:], out)
	case "pull":
		return cmdPull(args[1:], out)
	case "fetch":
		return cmdFetch(args[1:], out)
	case "checkout":
		return cmdCheckout(args[1:], out)
	case "rm":
		return cmdRemove(args[1:], out)
	case "status":
		return cmdStatus(args[1:], out)
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

// checkPruneScope rejects --prune combined with a selection: stale entries
// are not in the schema, so they have no tags and no defined paths to
// select on. Pruning is a whole-workspace operation.
func checkPruneScope(parsed parsedArgs) error {
	if parsed.Flags["--prune"] && (len(parsed.Positionals) > 0 || parsed.Values["--tags"] != "") {
		return errors.New("--prune applies to the whole workspace; it cannot be combined with a path or --tags")
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
		[]string{"list [path] [--archived] [--tags a,b]"},
		[]string{"List groups, repositories, and files defined in the schema."}},
	{"info",
		[]string{"info <path> [--archived] [--tags a,b]"},
		[]string{"Show repository, file, or group metadata."}},
	{"deps",
		[]string{"deps <path> [--with-optional-deps] [--archived] [--tags a,b]"},
		[]string{"Show expanded recursive dependencies for repositories matching a path."}},
	{"clone",
		[]string{"clone [path] [--no-deps] [--with-optional-deps] [--archived] [--tags a,b]"},
		[]string{"Clone/materialize all entries, or repositories/files matching a path. --no-deps skips dependencies."}},
	{"sync",
		[]string{"sync [path] [--no-deps] [--with-optional-deps] [--archived] [--prune] [--tags a,b]"},
		[]string{
			"Clone missing repos, move renamed repos/files, update origins/files, and refresh local state.",
			"--prune deletes entries removed from the schema; dirty/unpushed repos and modified files are kept.",
		}},
	{"pull",
		[]string{"pull [path] [--archived] [--tags a,b]"},
		[]string{"Run git pull --ff-only in installed repositories matching a path or group."}},
	{"fetch",
		[]string{"fetch [path] [--archived] [--tags a,b]"},
		[]string{"Run git fetch in installed repositories matching a path or group."}},
	{"checkout",
		[]string{"checkout [-b] <branch> [path] [--archived] [--tags a,b]"},
		[]string{"Switch installed repositories to a branch; -b creates it. Repos where the switch would lose local changes are skipped."}},
	{"rm",
		[]string{"rm <path>... [-r|--recursive] [-f|--force]"},
		[]string{"Uninstall repositories or files: delete the checkout and stop tracking it. -r removes groups, -f overrides dirty/unpushed checks."}},
	{"status",
		[]string{"status [path] [--all] [--archived] [--tags a,b]"},
		[]string{"Show the state of installed entries; repos never installed are only counted unless --all is given."}},
	{"update",
		[]string{
			"update",
			"update --sync [path] [--no-deps] [--with-optional-deps] [--archived] [--prune] [--tags a,b]",
		},
		[]string{
			"Fast-forward the schema checkout (.jig/source) from its remote without changing local checkouts.",
			"With --sync, then sync the workspace.",
		}},
	{"cache",
		[]string{
			"cache",
			"cache clean [--unused <days>]",
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
	fmt.Fprintln(out, "Paths identify repositories, files, or groups using slash paths such as services/checkout or platform.")
	fmt.Fprintln(out, "--tags a,b keeps only entries carrying all the listed tags; tags on groups are inherited by their children.")
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
	parsed, err := parseArgs(args, map[string]flagKind{"--archived": boolFlag, "--tags": valueFlag})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 1 {
		return usageError("list")
	}
	return jig.List(jig.ListOptions{
		Path:            optionalPath(parsed.Positionals),
		IncludeArchived: parsed.Flags["--archived"],
		Tags:            parseTags(parsed.Values["--tags"]),
	}, out)
}

func cmdInfo(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, map[string]flagKind{"--archived": boolFlag, "--tags": valueFlag})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) != 1 {
		return usageError("info")
	}
	return jig.Info(jig.InfoOptions{
		Path:            parsed.Positionals[0],
		IncludeArchived: parsed.Flags["--archived"],
		Tags:            parseTags(parsed.Values["--tags"]),
	}, out)
}

func cmdDeps(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, map[string]flagKind{"--with-optional-deps": boolFlag, "--archived": boolFlag, "--tags": valueFlag})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) != 1 {
		return usageError("deps")
	}
	return jig.Dependencies(jig.DependenciesOptions{
		Path:            parsed.Positionals[0],
		IncludeOptional: parsed.Flags["--with-optional-deps"],
		IncludeArchived: parsed.Flags["--archived"],
		Tags:            parseTags(parsed.Values["--tags"]),
	}, out)
}

func cmdClone(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, map[string]flagKind{"--with-optional-deps": boolFlag, "--no-deps": boolFlag, "--archived": boolFlag, "--tags": valueFlag})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 1 {
		return usageError("clone")
	}
	if err := checkDepsFlags(parsed); err != nil {
		return err
	}
	return jig.Clone(jig.CloneOptions{
		Path:            optionalPath(parsed.Positionals),
		IncludeOptional: parsed.Flags["--with-optional-deps"],
		IncludeArchived: parsed.Flags["--archived"],
		SkipDeps:        parsed.Flags["--no-deps"],
		Tags:            parseTags(parsed.Values["--tags"]),
	}, out)
}

func cmdSync(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, map[string]flagKind{"--with-optional-deps": boolFlag, "--no-deps": boolFlag, "--prune": boolFlag, "--archived": boolFlag, "--tags": valueFlag})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 1 {
		return usageError("sync")
	}
	if err := checkDepsFlags(parsed); err != nil {
		return err
	}
	if err := checkPruneScope(parsed); err != nil {
		return err
	}
	return jig.Sync(jig.SyncOptions{
		Path:            optionalPath(parsed.Positionals),
		IncludeOptional: parsed.Flags["--with-optional-deps"],
		IncludeArchived: parsed.Flags["--archived"],
		SkipDeps:        parsed.Flags["--no-deps"],
		Prune:           parsed.Flags["--prune"],
		Tags:            parseTags(parsed.Values["--tags"]),
	}, out)
}

func cmdPull(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, map[string]flagKind{"--archived": boolFlag, "--tags": valueFlag})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 1 {
		return usageError("pull")
	}
	return jig.Pull(jig.PullOptions{
		Path:            optionalPath(parsed.Positionals),
		IncludeArchived: parsed.Flags["--archived"],
		Tags:            parseTags(parsed.Values["--tags"]),
	}, out)
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

func cmdFetch(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, map[string]flagKind{"--archived": boolFlag, "--tags": valueFlag})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 1 {
		return usageError("fetch")
	}
	return jig.Fetch(jig.FetchOptions{
		Path:            optionalPath(parsed.Positionals),
		IncludeArchived: parsed.Flags["--archived"],
		Tags:            parseTags(parsed.Values["--tags"]),
	}, out)
}

func cmdCheckout(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, map[string]flagKind{"-b": boolFlag, "--archived": boolFlag, "--tags": valueFlag})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) < 1 || len(parsed.Positionals) > 2 {
		return usageError("checkout")
	}
	return jig.Checkout(jig.CheckoutOptions{
		Branch:          parsed.Positionals[0],
		Path:            optionalPath(parsed.Positionals[1:]),
		Create:          parsed.Flags["-b"],
		IncludeArchived: parsed.Flags["--archived"],
		Tags:            parseTags(parsed.Values["--tags"]),
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
	parsed, err := parseArgs(args, map[string]flagKind{"--archived": boolFlag, "--all": boolFlag, "--tags": valueFlag})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 1 {
		return usageError("status")
	}
	return jig.Status(jig.StatusOptions{
		Path:            optionalPath(parsed.Positionals),
		IncludeArchived: parsed.Flags["--archived"],
		All:             parsed.Flags["--all"],
		Tags:            parseTags(parsed.Values["--tags"]),
	}, out)
}

func cmdUpdate(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, map[string]flagKind{"--sync": boolFlag, "--with-optional-deps": boolFlag, "--no-deps": boolFlag, "--prune": boolFlag, "--archived": boolFlag, "--tags": valueFlag})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 1 || len(parsed.Positionals) == 1 && !parsed.Flags["--sync"] {
		return usageError("update")
	}
	if !parsed.Flags["--sync"] && (parsed.Flags["--no-deps"] || parsed.Flags["--with-optional-deps"] || parsed.Flags["--archived"] || parsed.Flags["--prune"]) {
		return errors.New("--no-deps, --with-optional-deps, --archived, and --prune require --sync")
	}
	if err := checkDepsFlags(parsed); err != nil {
		return err
	}
	if err := checkPruneScope(parsed); err != nil {
		return err
	}
	return jig.Update(jig.UpdateOptions{
		Sync:            parsed.Flags["--sync"],
		Path:            optionalPath(parsed.Positionals),
		IncludeOptional: parsed.Flags["--with-optional-deps"],
		IncludeArchived: parsed.Flags["--archived"],
		SkipDeps:        parsed.Flags["--no-deps"],
		Prune:           parsed.Flags["--prune"],
		Tags:            parseTags(parsed.Values["--tags"]),
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
