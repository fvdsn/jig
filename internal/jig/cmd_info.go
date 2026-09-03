package jig

import (
	"fmt"
	"io"
	"strings"
)

type InfoOptions struct {
	Path            string
	Id              string // selects one entry by identity instead of a path
	IncludeArchived bool
	Tags            []string
}

func Info(options InfoOptions, out io.Writer) error {
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	selection, err := ws.Select(NodeQuery{Path: options.Path, Id: options.Id, IncludeArchived: options.IncludeArchived, Tags: options.Tags})
	if err != nil {
		return err
	}
	path := selection.Path
	for _, kind := range []EntryKind{EntryRepo, EntryFile, EntryDir} {
		if entry, ok := selection.exact(kind); ok {
			printEntryInfo(out, entry)
			return nil
		}
	}

	group, hasGroup := selection.exactGroup()
	if len(selection.Entries) == 0 {
		if err := unknownSelectorError(&ws.Model, path, options.Tags); err != nil {
			return err
		}
		return fmt.Errorf("no repository, file, or group matches %q", path)
	}
	if hasGroup {
		printEntryInfo(out, group)
	} else {
		// A directory with no $group of its own is still a selectable
		// scope; it has nothing but its entries.
		fmt.Fprintf(out, "path: %s\n", path)
		fmt.Fprintln(out, "type: group")
	}
	var children []Entry
	for _, entry := range selection.Entries {
		if entry.Path != path {
			children = append(children, entry)
		}
	}
	if len(children) > 0 {
		fmt.Fprintln(out, "entries:")
		for _, entry := range children {
			fmt.Fprintf(out, "  %-5s %s\n", entry.Kind, entry.Path)
		}
	}
	return nil
}

// printEntryInfo renders one entry: the header every kind shares, the
// kind's own fields, then the shared trailer (archived, tags, meta,
// conditions, dependencies).
func printEntryInfo(out io.Writer, entry Entry) {
	fmt.Fprintf(out, "path: %s\n", entry.Path)
	fmt.Fprintf(out, "type: %s\n", entry.Kind)
	fmt.Fprintf(out, "identity: %s\n", entry.Identity)
	var description, web string
	var lifecycle []string
	var dependsOn []Dependency
	archived := entry.archived()
	switch entry.Kind {
	case EntryRepo:
		repo := entry.Repo
		fmt.Fprintf(out, "git: %s\n", repo.Git)
		description, web = repo.Description, repo.Web
		lifecycle = []string{repo.Setup, repo.Fmt, repo.Lint, repo.Test}
		dependsOn = repo.DependsOn
	case EntryFile:
		file := entry.File
		printSources(out, file.Src, file.Link, file.Copy)
		description = file.Description
	case EntryDir:
		dir := entry.Dir
		printSources(out, dir.Src, dir.Link, dir.Copy)
		description = dir.Description
	case EntryGroup:
		group := entry.Group
		description, web = group.Description, group.Web
		lifecycle = []string{group.Setup, group.Fmt, group.Lint, group.Test}
		dependsOn = group.DependsOn
	}
	if web != "" {
		fmt.Fprintf(out, "web: %s\n", web)
	}
	if description != "" {
		fmt.Fprintf(out, "description: %s\n", description)
	}
	if lifecycle != nil {
		printLifecycleCommands(out, lifecycle[0], lifecycle[1], lifecycle[2], lifecycle[3])
	}
	if entry.Kind == EntryFile && entry.File.Executable {
		// The bit is only settable on src files; link and copy files
		// follow their target.
		fmt.Fprintln(out, "executable: true")
	}
	if archived {
		fmt.Fprintln(out, "archived: true")
	}
	printTags(out, entry.Tags)
	printMeta(out, entry.Meta)
	if len(entry.Conditions) > 0 {
		printConditions(out, "onlyWhen", entry.Conditions)
	}
	if len(dependsOn) > 0 {
		fmt.Fprintln(out, "dependsOn:")
		for _, dep := range dependsOn {
			printDependency(out, dep)
		}
	}
}

// printSources renders how a $file or $dir gets its content: its source
// list, or the link or copy target.
func printSources(out io.Writer, sources SrcList, link *Ref, copy *Ref) {
	switch {
	case link != nil:
		fmt.Fprintf(out, "link: %s\n", describeRef(*link))
	case copy != nil:
		fmt.Fprintf(out, "copy: %s\n", describeRef(*copy))
	default:
		printSrcList(out, sources)
	}
}

// printSrcList renders a $file or $dir source list: a single unconditional
// source inline, anything else as one line per source.
func printSrcList(out io.Writer, sources SrcList) {
	if len(sources) == 1 && sources[0].OnlyWhen == nil && !sources[0].Optional {
		fmt.Fprintf(out, "src: %s\n", sources[0].describe())
		return
	}
	fmt.Fprintln(out, "src:")
	for _, source := range sources {
		line := "  " + source.describe()
		if source.Optional {
			line += " (optional)"
		}
		if source.OnlyWhen != nil {
			line += " (onlyWhen: " + describeRef(source.OnlyWhen.Ref) + ")"
		}
		fmt.Fprintln(out, line)
	}
}

func printLifecycleCommands(out io.Writer, setup, fmtCmd, lint, test string) {
	for _, command := range []struct{ verb, cmd string }{
		{"setup", setup}, {"fmt", fmtCmd}, {"lint", lint}, {"test", test},
	} {
		if command.cmd != "" {
			fmt.Fprintf(out, "%s: %s\n", command.verb, command.cmd)
		}
	}
}

func printMeta(out io.Writer, meta map[string]string) {
	if len(meta) == 0 {
		return
	}
	fmt.Fprintln(out, "meta:")
	for _, key := range sortedMetaKeys(meta) {
		fmt.Fprintf(out, "  %s: %s\n", key, meta[key])
	}
}

func printTags(out io.Writer, tags []string) {
	if len(tags) > 0 {
		fmt.Fprintf(out, "tags: %s\n", strings.Join(tags, ", "))
	}
}

func printConditions(out io.Writer, label string, conditions []Condition) {
	if len(conditions) == 1 {
		fmt.Fprintf(out, "%s: %s\n", label, describeConditionWithReason(conditions[0]))
		return
	}
	fmt.Fprintf(out, "%s:\n", label)
	for _, condition := range conditions {
		fmt.Fprintf(out, "  %s\n", describeConditionWithReason(condition))
	}
}

func describeConditionWithReason(condition Condition) string {
	described := describeRef(condition.Ref)
	if condition.Reason != "" {
		described += ": " + condition.Reason
	}
	return described
}

func printDependency(out io.Writer, dep Dependency) {
	optional := ""
	if dep.Optional {
		optional = " optional"
	}
	if dep.Reason == "" {
		fmt.Fprintf(out, "  %s%s\n", describeRef(dep.Ref), optional)
	} else {
		fmt.Fprintf(out, "  %s%s: %s\n", describeRef(dep.Ref), optional, dep.Reason)
	}
}
