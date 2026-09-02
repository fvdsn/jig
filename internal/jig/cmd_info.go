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
	if entry, ok := selection.exactRepo(); ok {
		repo := entry.Repo
		fmt.Fprintf(out, "path: %s\n", path)
		fmt.Fprintln(out, "type: repo")
		fmt.Fprintf(out, "identity: %s\n", entry.Identity)
		fmt.Fprintf(out, "git: %s\n", repo.Git)
		if repo.Web != "" {
			fmt.Fprintf(out, "web: %s\n", repo.Web)
		}
		if repo.Description != "" {
			fmt.Fprintf(out, "description: %s\n", repo.Description)
		}
		printLifecycleCommands(out, repo.Setup, repo.Fmt, repo.Lint, repo.Test)
		if repo.Archived {
			fmt.Fprintln(out, "archived: true")
		}
		printTags(out, entry.Tags)
		printMeta(out, entry.Meta)
		if len(entry.Conditions) > 0 {
			printConditions(out, "onlyWhen", entry.Conditions)
		}
		if len(repo.DependsOn) > 0 {
			fmt.Fprintln(out, "dependsOn:")
			for _, dep := range repo.DependsOn {
				printDependency(out, dep)
			}
		}
		return nil
	}
	if entry, ok := selection.exactFile(); ok {
		file := entry.File
		fmt.Fprintf(out, "path: %s\n", path)
		fmt.Fprintln(out, "type: file")
		fmt.Fprintf(out, "identity: %s\n", entry.Identity)
		if len(file.Src) > 0 {
			printSrcList(out, file.Src)
		}
		if file.Link != nil {
			fmt.Fprintf(out, "link: %s\n", describeRef(*file.Link))
		}
		if file.Description != "" {
			fmt.Fprintf(out, "description: %s\n", file.Description)
		}
		if file.Archived {
			fmt.Fprintln(out, "archived: true")
		}
		printTags(out, entry.Tags)
		printMeta(out, entry.Meta)
		fmt.Fprintf(out, "executable: %v\n", file.Executable)
		if len(entry.Conditions) > 0 {
			printConditions(out, "onlyWhen", entry.Conditions)
		}
		return nil
	}

	if entry, ok := selection.exact(EntryDir); ok {
		dir := entry.Dir
		fmt.Fprintf(out, "path: %s\n", path)
		fmt.Fprintln(out, "type: dir")
		fmt.Fprintf(out, "identity: %s\n", entry.Identity)
		if dir.Link != nil {
			fmt.Fprintf(out, "link: %s\n", describeRef(*dir.Link))
		} else {
			printSrcList(out, dir.Src)
		}
		if dir.Description != "" {
			fmt.Fprintf(out, "description: %s\n", dir.Description)
		}
		if dir.Archived {
			fmt.Fprintln(out, "archived: true")
		}
		printTags(out, entry.Tags)
		printMeta(out, entry.Meta)
		if len(entry.Conditions) > 0 {
			printConditions(out, "onlyWhen", entry.Conditions)
		}
		return nil
	}

	group, hasGroup := selection.exactGroup()
	if len(selection.Entries) == 0 {
		if err := unknownSelectorError(&ws.Model, path, options.Tags); err != nil {
			return err
		}
		return fmt.Errorf("no repository, file, or group matches %q", path)
	}
	fmt.Fprintf(out, "group: %s\n", path)
	if hasGroup {
		fmt.Fprintf(out, "identity: %s\n", group.Identity)
		if group.Group.Description != "" {
			fmt.Fprintf(out, "description: %s\n", group.Group.Description)
		}
		if group.Group.Web != "" {
			fmt.Fprintf(out, "web: %s\n", group.Group.Web)
		}
		printLifecycleCommands(out, group.Group.Setup, group.Group.Fmt, group.Group.Lint, group.Group.Test)
		if group.Group.Archived {
			fmt.Fprintln(out, "archived: true")
		}
		printTags(out, group.Tags)
		printMeta(out, group.Meta)
		if len(group.Conditions) > 0 {
			printConditions(out, "onlyWhen", group.Conditions)
		}
		if len(group.Group.DependsOn) > 0 {
			fmt.Fprintln(out, "dependsOn:")
			for _, dep := range group.Group.DependsOn {
				printDependency(out, dep)
			}
		}
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

// printSrcList renders a $file or $dir source list: a single unconditional
// source inline, anything else as one line per source.
func printSrcList(out io.Writer, sources SrcList) {
	if len(sources) == 1 && sources[0].OnlyWhen == nil {
		fmt.Fprintf(out, "src: %s\n", sources[0].Src)
		return
	}
	fmt.Fprintln(out, "src:")
	for _, source := range sources {
		line := "  " + source.Src
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
