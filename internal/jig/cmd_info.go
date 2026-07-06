package jig

import (
	"fmt"
	"io"
	"strings"
)

type InfoOptions struct {
	Path            string
	IncludeArchived bool
	Tags            []string
}

func Info(options InfoOptions, out io.Writer) error {
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	selection, err := ws.Select(NodeQuery{Path: options.Path, IncludeArchived: options.IncludeArchived, Tags: options.Tags})
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
		if repo.Archived {
			fmt.Fprintln(out, "archived: true")
		}
		printTags(out, entry.Tags)
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
		if file.Src != "" {
			fmt.Fprintf(out, "src: %s\n", file.Src)
		}
		if file.Link != "" {
			fmt.Fprintf(out, "link: %s\n", file.Link)
		}
		if file.Description != "" {
			fmt.Fprintf(out, "description: %s\n", file.Description)
		}
		if file.Archived {
			fmt.Fprintln(out, "archived: true")
		}
		printTags(out, entry.Tags)
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
		if len(dir.Src) == 1 && dir.Src[0].OnlyWhen == nil {
			fmt.Fprintf(out, "src: %s\n", dir.Src[0].Src)
		} else {
			fmt.Fprintln(out, "src:")
			for _, source := range dir.Src {
				line := "  " + source.Src
				if source.OnlyWhen != nil {
					line += " (onlyWhen: " + source.OnlyWhen.Path + ")"
				}
				fmt.Fprintln(out, line)
			}
		}
		if dir.Description != "" {
			fmt.Fprintf(out, "description: %s\n", dir.Description)
		}
		if dir.Archived {
			fmt.Fprintln(out, "archived: true")
		}
		printTags(out, entry.Tags)
		if len(entry.Conditions) > 0 {
			printConditions(out, "onlyWhen", entry.Conditions)
		}
		return nil
	}

	group, hasGroup := selection.exactGroup()
	if len(selection.Entries) == 0 {
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
		if group.Group.Archived {
			fmt.Fprintln(out, "archived: true")
		}
		printTags(out, group.Tags)
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

func printTags(out io.Writer, tags []string) {
	if len(tags) > 0 {
		fmt.Fprintf(out, "tags: %s\n", strings.Join(tags, ", "))
	}
}

func printConditions(out io.Writer, label string, conditions []Condition) {
	if len(conditions) == 1 {
		condition := conditions[0]
		if condition.Reason == "" {
			fmt.Fprintf(out, "%s: %s\n", label, condition.Path)
		} else {
			fmt.Fprintf(out, "%s: %s: %s\n", label, condition.Path, condition.Reason)
		}
		return
	}
	fmt.Fprintf(out, "%s:\n", label)
	for _, condition := range conditions {
		if condition.Reason == "" {
			fmt.Fprintf(out, "  %s\n", condition.Path)
		} else {
			fmt.Fprintf(out, "  %s: %s\n", condition.Path, condition.Reason)
		}
	}
}

func printDependency(out io.Writer, dep Dependency) {
	optional := ""
	if dep.Optional {
		optional = " optional"
	}
	if dep.Reason == "" {
		fmt.Fprintf(out, "  %s%s\n", dep.Path, optional)
	} else {
		fmt.Fprintf(out, "  %s%s: %s\n", dep.Path, optional, dep.Reason)
	}
}
