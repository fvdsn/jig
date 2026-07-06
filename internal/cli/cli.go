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

// versionString reports the module version embedded by go install, falling
// back to the VCS revision for local builds.
func versionString() string {
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

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "Jig is a workspace CLI for managing many related Git repositories and generated files from a shared schema.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  jig <command> [args]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Commands:")
	fmt.Fprintln(out, "  init [git-url-or-file [workspace-dir]] [--path <path>] [--clone [path]] [--no-deps] [--with-optional-deps] [--archived]")
	fmt.Fprintln(out, "      Initialize a workspace: clone the schema repository into .jig/source, optionally cloning a path.")
	fmt.Fprintln(out, "      With no arguments, start a fresh workspace here with a starter schema in .jig/source.")
	fmt.Fprintln(out, "  validate [schema-file]")
	fmt.Fprintln(out, "      Validate the current workspace schema, or a schema file given by path.")
	fmt.Fprintln(out, "  list [path] [--archived] [--tags a,b]")
	fmt.Fprintln(out, "      List groups, repositories, and files defined in the schema.")
	fmt.Fprintln(out, "  info <path> [--archived] [--tags a,b]")
	fmt.Fprintln(out, "      Show repository, file, or group metadata.")
	fmt.Fprintln(out, "  deps <path> [--with-optional-deps] [--archived] [--tags a,b]")
	fmt.Fprintln(out, "      Show expanded recursive dependencies for repositories matching a path.")
	fmt.Fprintln(out, "  clone [path] [--no-deps] [--with-optional-deps] [--archived] [--tags a,b]")
	fmt.Fprintln(out, "      Clone/materialize all entries, or repositories/files matching a path. --no-deps skips dependencies.")
	fmt.Fprintln(out, "  sync [path] [--no-deps] [--with-optional-deps] [--archived] [--prune] [--tags a,b]")
	fmt.Fprintln(out, "      Clone missing repos, move renamed repos/files, update origins/files, and refresh local state.")
	fmt.Fprintln(out, "      --prune deletes entries removed from the schema; dirty/unpushed repos and modified files are kept.")
	fmt.Fprintln(out, "  pull [path] [--archived] [--tags a,b]")
	fmt.Fprintln(out, "      Run git pull --ff-only in installed repositories matching a path or group.")
	fmt.Fprintln(out, "  fetch [path] [--archived] [--tags a,b]")
	fmt.Fprintln(out, "      Run git fetch in installed repositories matching a path or group.")
	fmt.Fprintln(out, "  checkout [-b] <branch> [path] [--archived] [--tags a,b]")
	fmt.Fprintln(out, "      Switch installed repositories to a branch; -b creates it. Repos where the switch would lose local changes are skipped.")
	fmt.Fprintln(out, "  rm <path>... [-r|--recursive] [-f|--force]")
	fmt.Fprintln(out, "      Uninstall repositories or files: delete the checkout and stop tracking it. -r removes groups, -f overrides dirty/unpushed checks.")
	fmt.Fprintln(out, "  status [path] [--all] [--archived] [--tags a,b]")
	fmt.Fprintln(out, "      Show the state of installed entries; repos never installed are only counted unless --all is given.")
	fmt.Fprintln(out, "  update")
	fmt.Fprintln(out, "      Fast-forward the schema checkout (.jig/source) from its remote without changing local checkouts.")
	fmt.Fprintln(out, "  update --sync [path] [--no-deps] [--with-optional-deps] [--archived] [--prune] [--tags a,b]")
	fmt.Fprintln(out, "      Update the schema, then sync the workspace.")
	fmt.Fprintln(out, "  cache")
	fmt.Fprintln(out, "      Show the clone cache location, mirror count, and size.")
	fmt.Fprintln(out, "  cache clean [--unused <days>]")
	fmt.Fprintln(out, "      Remove cached mirrors, optionally only those unused for at least <days> days.")
	fmt.Fprintln(out, "  version")
	fmt.Fprintln(out, "      Print the jig version.")
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
		return errors.New("usage: jig init [git-url-or-file [workspace-dir]] [--path <path>] [--clone [path]] [--no-deps] [--with-optional-deps] [--archived] [--tags a,b]")
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
		return errors.New("usage: jig validate [schema-file]")
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
		return errors.New("usage: jig list [path] [--archived] [--tags a,b]")
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
		return errors.New("usage: jig info <path> [--archived] [--tags a,b]")
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
		return errors.New("usage: jig deps <path> [--with-optional-deps] [--archived] [--tags a,b]")
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
		return errors.New("usage: jig clone [path] [--no-deps] [--with-optional-deps] [--archived] [--tags a,b]")
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
		return errors.New("usage: jig sync [path] [--no-deps] [--with-optional-deps] [--archived] [--prune] [--tags a,b]")
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
		return errors.New("usage: jig pull [path] [--archived] [--tags a,b]")
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
		return errors.New("usage: jig cache | jig cache clean [--unused <days>]")
	}
	parsed, err := parseArgs(args[1:], map[string]flagKind{"--unused": valueFlag})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 0 {
		return errors.New("usage: jig cache clean [--unused <days>]")
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
		return errors.New("usage: jig fetch [path] [--archived] [--tags a,b]")
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
		return errors.New("usage: jig checkout [-b] <branch> [path] [--archived] [--tags a,b]")
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
		return errors.New("usage: jig rm <path>... [-r|--recursive] [-f|--force]")
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
		return errors.New("usage: jig status [path] [--all] [--archived] [--tags a,b]")
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
		return errors.New("usage: jig update | jig update --sync [path] [--no-deps] [--with-optional-deps] [--archived] [--prune]")
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
