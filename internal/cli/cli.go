package cli

import (
	"errors"
	"fmt"
	"io"
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
	case "status":
		return cmdStatus(args[1:], out)
	case "update":
		return cmdUpdate(args[1:], out)
	case "help", "--help", "-h":
		printUsage(out)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
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
	"--with-optional-deps": boolFlag,
	"--archived":           boolFlag,
}

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "Jig is a workspace CLI for managing many related Git repositories and generated files from a shared schema.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  jig <command> [args]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Commands:")
	fmt.Fprintln(out, "  init <git-url-or-file> [workspace-dir] [--path <path>] [--clone [path]] [--with-optional-deps] [--archived]")
	fmt.Fprintln(out, "      Initialize a workspace: clone the schema repository into .jig/source, optionally cloning a path.")
	fmt.Fprintln(out, "  validate [schema-file]")
	fmt.Fprintln(out, "      Validate the current workspace schema, or a schema file given by path.")
	fmt.Fprintln(out, "  list [path] [--archived]")
	fmt.Fprintln(out, "      List groups, repositories, and files defined in the schema.")
	fmt.Fprintln(out, "  info <path> [--archived]")
	fmt.Fprintln(out, "      Show repository, file, or group metadata.")
	fmt.Fprintln(out, "  deps <path> [--with-optional-deps] [--archived]")
	fmt.Fprintln(out, "      Show expanded recursive dependencies for repositories matching a path.")
	fmt.Fprintln(out, "  clone [path] [--with-optional-deps] [--archived] [--refresh]")
	fmt.Fprintln(out, "      Clone/materialize all entries, or repositories/files matching a path.")
	fmt.Fprintln(out, "  sync [path] [--with-optional-deps] [--archived] [--refresh]")
	fmt.Fprintln(out, "      Clone missing repos, move renamed repos/files, update origins/files, and refresh local state.")
	fmt.Fprintln(out, "  pull [path] [--archived]")
	fmt.Fprintln(out, "      Run git pull in installed repositories matching a path or group.")
	fmt.Fprintln(out, "  status [path] [--archived]")
	fmt.Fprintln(out, "      Show installed, missing, moved, dirty, stale, modified, and remote-changed entries.")
	fmt.Fprintln(out, "  update")
	fmt.Fprintln(out, "      Fast-forward the schema checkout (.jig/source) from its remote without changing local checkouts.")
	fmt.Fprintln(out, "  update --sync [path] [--with-optional-deps] [--archived] [--refresh]")
	fmt.Fprintln(out, "      Update the schema, then sync the workspace.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Paths identify repositories, files, or groups using slash paths such as services/checkout or platform.")
}

func cmdInit(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, initFlags)
	if err != nil {
		return err
	}
	if len(parsed.Positionals) == 0 || len(parsed.Positionals) > 2 {
		return errors.New("usage: jig init <git-url-or-file> [workspace-dir] [--path <path>] [--clone [path]] [--with-optional-deps] [--archived]")
	}
	if !parsed.Flags["--clone"] && (parsed.Flags["--with-optional-deps"] || parsed.Flags["--archived"]) {
		return errors.New("--with-optional-deps and --archived require --clone")
	}

	options := jig.InitOptions{
		SourceArg:       parsed.Positionals[0],
		WorkspaceDir:    ".",
		SchemaPath:      parsed.Values["--path"],
		Clone:           parsed.Flags["--clone"],
		ClonePath:       parsed.Values["--clone"],
		IncludeOptional: parsed.Flags["--with-optional-deps"],
		IncludeArchived: parsed.Flags["--archived"],
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
	parsed, err := parseArgs(args, map[string]flagKind{"--archived": boolFlag})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 1 {
		return errors.New("usage: jig list [path] [--archived]")
	}
	return jig.List(jig.ListOptions{
		Path:            optionalPath(parsed.Positionals),
		IncludeArchived: parsed.Flags["--archived"],
	}, out)
}

func cmdInfo(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, map[string]flagKind{"--archived": boolFlag})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) != 1 {
		return errors.New("usage: jig info <path> [--archived]")
	}
	return jig.Info(jig.InfoOptions{
		Path:            parsed.Positionals[0],
		IncludeArchived: parsed.Flags["--archived"],
	}, out)
}

func cmdDeps(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, map[string]flagKind{"--with-optional-deps": boolFlag, "--archived": boolFlag})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) != 1 {
		return errors.New("usage: jig deps <path> [--with-optional-deps] [--archived]")
	}
	return jig.Dependencies(jig.DependenciesOptions{
		Path:            parsed.Positionals[0],
		IncludeOptional: parsed.Flags["--with-optional-deps"],
		IncludeArchived: parsed.Flags["--archived"],
	}, out)
}

func cmdClone(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, map[string]flagKind{"--with-optional-deps": boolFlag, "--archived": boolFlag, "--refresh": boolFlag})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 1 {
		return errors.New("usage: jig clone [path] [--with-optional-deps] [--archived] [--refresh]")
	}
	return jig.Clone(jig.CloneOptions{
		Path:            optionalPath(parsed.Positionals),
		IncludeOptional: parsed.Flags["--with-optional-deps"],
		IncludeArchived: parsed.Flags["--archived"],
		Refresh:         parsed.Flags["--refresh"],
	}, out)
}

func cmdSync(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, map[string]flagKind{"--with-optional-deps": boolFlag, "--archived": boolFlag, "--refresh": boolFlag})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 1 {
		return errors.New("usage: jig sync [path] [--with-optional-deps] [--archived] [--refresh]")
	}
	return jig.Sync(jig.SyncOptions{
		Path:            optionalPath(parsed.Positionals),
		IncludeOptional: parsed.Flags["--with-optional-deps"],
		IncludeArchived: parsed.Flags["--archived"],
		Refresh:         parsed.Flags["--refresh"],
	}, out)
}

func cmdPull(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, map[string]flagKind{"--archived": boolFlag})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 1 {
		return errors.New("usage: jig pull [path] [--archived]")
	}
	return jig.Pull(jig.PullOptions{
		Path:            optionalPath(parsed.Positionals),
		IncludeArchived: parsed.Flags["--archived"],
	}, out)
}

func cmdStatus(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, map[string]flagKind{"--archived": boolFlag})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 1 {
		return errors.New("usage: jig status [path] [--archived]")
	}
	return jig.Status(jig.StatusOptions{
		Path:            optionalPath(parsed.Positionals),
		IncludeArchived: parsed.Flags["--archived"],
	}, out)
}

func cmdUpdate(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, map[string]flagKind{"--sync": boolFlag, "--with-optional-deps": boolFlag, "--archived": boolFlag, "--refresh": boolFlag})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 1 || len(parsed.Positionals) == 1 && !parsed.Flags["--sync"] {
		return errors.New("usage: jig update | jig update --sync [path] [--with-optional-deps] [--archived] [--refresh]")
	}
	if !parsed.Flags["--sync"] && (parsed.Flags["--with-optional-deps"] || parsed.Flags["--archived"] || parsed.Flags["--refresh"]) {
		return errors.New("--with-optional-deps, --archived, and --refresh require --sync")
	}
	return jig.Update(jig.UpdateOptions{
		Sync:            parsed.Flags["--sync"],
		Path:            optionalPath(parsed.Positionals),
		IncludeOptional: parsed.Flags["--with-optional-deps"],
		IncludeArchived: parsed.Flags["--archived"],
		Refresh:         parsed.Flags["--refresh"],
	}, out)
}

func optionalPath(positionals []string) string {
	if len(positionals) == 0 {
		return ""
	}
	return positionals[0]
}
