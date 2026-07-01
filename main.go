package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer, _ io.Writer) error {
	if len(args) == 0 {
		printUsage(out)
		return nil
	}

	switch args[0] {
	case "init":
		return cmdInit(args[1:], out)
	case "validate":
		return cmdValidate(out)
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

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "Jig is a workspace CLI for managing many related Git repositories and generated files from a shared .jig.json definition.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  jig <command> [args]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Commands:")
	fmt.Fprintln(out, "  init <git-url-or-file> [workspace-dir] [--path <path>] [--clone [path]] [--with-optional-deps] [--archived]")
	fmt.Fprintln(out, "      Initialize a workspace from a Git-hosted or local Jig definition, optionally cloning a path.")
	fmt.Fprintln(out, "  validate")
	fmt.Fprintln(out, "      Validate the current workspace .jig.json file.")
	fmt.Fprintln(out, "  list [path] [--archived]")
	fmt.Fprintln(out, "      List repositories and files defined in .jig.json.")
	fmt.Fprintln(out, "  info <path> [--archived]")
	fmt.Fprintln(out, "      Show repository, file, or group metadata.")
	fmt.Fprintln(out, "  deps <path> [--with-optional-deps] [--archived]")
	fmt.Fprintln(out, "      Show expanded recursive dependencies for repositories matching a path.")
	fmt.Fprintln(out, "  clone [path] [--with-optional-deps] [--archived]")
	fmt.Fprintln(out, "      Clone/materialize all entries, or repositories/files matching a path.")
	fmt.Fprintln(out, "  sync [path] [--with-optional-deps] [--archived]")
	fmt.Fprintln(out, "      Clone missing repos, move renamed repos/files, update origins/files, and refresh local state.")
	fmt.Fprintln(out, "  pull [path] [--archived]")
	fmt.Fprintln(out, "      Run git pull in installed repositories matching a path or group.")
	fmt.Fprintln(out, "  status [path] [--archived]")
	fmt.Fprintln(out, "      Show installed, missing, moved, dirty, stale, modified, and remote-changed entries.")
	fmt.Fprintln(out, "  update")
	fmt.Fprintln(out, "      Update .jig.json from its configured source without changing local checkouts.")
	fmt.Fprintln(out, "  update --sync [path] [--with-optional-deps] [--archived]")
	fmt.Fprintln(out, "      Update .jig.json, then sync the workspace.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Paths identify repositories, files, or groups using slash paths such as services/checkout or platform.")
}

func cmdInit(args []string, out io.Writer) error {
	parsed, err := parseInitArgs(args)
	if err != nil {
		return err
	}
	if len(parsed.Positionals) == 0 || len(parsed.Positionals) > 2 {
		return errors.New("usage: jig init <git-url-or-file> [workspace-dir] [--path <path>] [--clone [path]] [--with-optional-deps] [--archived]")
	}
	if !parsed.Flags["--clone"] && (parsed.Flags["--with-optional-deps"] || parsed.Flags["--archived"]) {
		return errors.New("--with-optional-deps and --archived require --clone")
	}

	options := initOptions{
		SourceArg:       parsed.Positionals[0],
		WorkspaceDir:    ".",
		DefinitionPath:  definitionFile,
		Clone:           parsed.Flags["--clone"],
		ClonePath:       parsed.Values["--clone"],
		IncludeOptional: parsed.Flags["--with-optional-deps"],
		IncludeArchived: parsed.Flags["--archived"],
	}
	if len(parsed.Positionals) == 2 {
		options.WorkspaceDir = parsed.Positionals[1]
	}
	if parsed.Values["--path"] != "" {
		options.DefinitionPath = parsed.Values["--path"]
	}
	return initWorkspace(options, out)
}

func cmdValidate(out io.Writer) error {
	return validateWorkspace(out)
}

func cmdList(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, nil, map[string]bool{"--archived": true})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 1 {
		return errors.New("usage: jig list [path] [--archived]")
	}
	return listWorkspace(optionalPath(parsed.Positionals), parsed.Flags["--archived"], out)
}

func cmdInfo(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, nil, map[string]bool{"--archived": true})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) != 1 {
		return errors.New("usage: jig info <path> [--archived]")
	}
	return infoWorkspace(parsed.Positionals[0], parsed.Flags["--archived"], out)
}

func cmdDeps(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, nil, map[string]bool{"--with-optional-deps": true, "--archived": true})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) != 1 {
		return errors.New("usage: jig deps <path> [--with-optional-deps] [--archived]")
	}
	return dependenciesWorkspace(parsed.Positionals[0], parsed.Flags["--with-optional-deps"], parsed.Flags["--archived"], out)
}

func cmdClone(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, nil, map[string]bool{"--with-optional-deps": true, "--archived": true})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 1 {
		return errors.New("usage: jig clone [path] [--with-optional-deps] [--archived]")
	}
	return cloneWorkspace(optionalPath(parsed.Positionals), parsed.Flags["--with-optional-deps"], parsed.Flags["--archived"], out)
}

func cmdSync(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, nil, map[string]bool{"--with-optional-deps": true, "--archived": true})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 1 {
		return errors.New("usage: jig sync [path] [--with-optional-deps] [--archived]")
	}
	return syncCurrentWorkspace(optionalPath(parsed.Positionals), parsed.Flags["--with-optional-deps"], parsed.Flags["--archived"], out)
}

func cmdPull(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, nil, map[string]bool{"--archived": true})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 1 {
		return errors.New("usage: jig pull [path] [--archived]")
	}
	return pullWorkspace(optionalPath(parsed.Positionals), parsed.Flags["--archived"], out)
}

func cmdStatus(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, nil, map[string]bool{"--archived": true})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 1 {
		return errors.New("usage: jig status [path] [--archived]")
	}
	return statusWorkspace(optionalPath(parsed.Positionals), parsed.Flags["--archived"], out)
}

func cmdUpdate(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, nil, map[string]bool{"--sync": true, "--with-optional-deps": true, "--archived": true})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 1 || len(parsed.Positionals) == 1 && !parsed.Flags["--sync"] {
		return errors.New("usage: jig update | jig update --sync [path] [--with-optional-deps] [--archived]")
	}
	if !parsed.Flags["--sync"] && (parsed.Flags["--with-optional-deps"] || parsed.Flags["--archived"]) {
		return errors.New("--with-optional-deps and --archived require --sync")
	}
	return updateWorkspace(updateOptions{
		Sync:            parsed.Flags["--sync"],
		Path:            optionalPath(parsed.Positionals),
		IncludeOptional: parsed.Flags["--with-optional-deps"],
		IncludeArchived: parsed.Flags["--archived"],
	}, out)
}

func optionalPath(positionals []string) string {
	if len(positionals) == 0 {
		return ""
	}
	return positionals[0]
}

type parsedArgs struct {
	Positionals []string
	Values      map[string]string
	Flags       map[string]bool
}

func parseArgs(args []string, valueFlags map[string]bool, boolFlags map[string]bool) (parsedArgs, error) {
	parsed := parsedArgs{Values: map[string]string{}, Flags: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if valueFlags != nil && valueFlags[arg] {
			if i+1 >= len(args) {
				return parsed, fmt.Errorf("%s requires a value", arg)
			}
			parsed.Values[arg] = args[i+1]
			i++
			continue
		}
		if boolFlags != nil && boolFlags[arg] {
			parsed.Flags[arg] = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return parsed, fmt.Errorf("unknown flag %s", arg)
		}
		parsed.Positionals = append(parsed.Positionals, arg)
	}
	return parsed, nil
}

func parseInitArgs(args []string) (parsedArgs, error) {
	parsed := parsedArgs{Values: map[string]string{}, Flags: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--path":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return parsed, errors.New("--path requires a value")
			}
			parsed.Values[arg] = args[i+1]
			i++
		case "--clone":
			parsed.Flags[arg] = true
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				parsed.Values[arg] = args[i+1]
				i++
			}
		case "--with-optional-deps":
			parsed.Flags[arg] = true
		case "--archived":
			parsed.Flags[arg] = true
		default:
			if strings.HasPrefix(arg, "-") {
				return parsed, fmt.Errorf("unknown flag %s", arg)
			}
			parsed.Positionals = append(parsed.Positionals, arg)
		}
	}
	return parsed, nil
}
