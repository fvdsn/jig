package jig

import (
	"fmt"
	"io"
)

type InfoOptions struct {
	Path            string
	IncludeArchived bool
}

func Info(options InfoOptions, out io.Writer) error {
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	selection, err := ws.Select(NodeQuery{Path: options.Path, IncludeArchived: options.IncludeArchived})
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
		if len(entry.Conditions) > 0 {
			printConditions(out, "onlyWhen", entry.Conditions)
		}
		if len(repo.DependsOn) > 0 {
			fmt.Fprintln(out, "dependsOn:")
			for _, dep := range repo.DependsOn {
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
		fmt.Fprintf(out, "executable: %v\n", file.Executable)
		if len(entry.Conditions) > 0 {
			printConditions(out, "onlyWhen", entry.Conditions)
		}
		return nil
	}

	group, hasGroup := selection.exactGroup()
	if len(selection.Repos) == 0 && len(selection.Files) == 0 && !hasGroup {
		return fmt.Errorf("no repository, file, or group matches %q", path)
	}
	fmt.Fprintf(out, "group: %s\n", path)
	if hasGroup {
		if group.Group.Description != "" {
			fmt.Fprintf(out, "description: %s\n", group.Group.Description)
		}
		if group.Group.Web != "" {
			fmt.Fprintf(out, "web: %s\n", group.Group.Web)
		}
		if group.Group.Archived {
			fmt.Fprintln(out, "archived: true")
		}
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
	if len(selection.Repos) > 0 {
		fmt.Fprintln(out, "repos:")
		for _, entry := range selection.Repos {
			fmt.Fprintf(out, "  %s\n", entry.Path)
		}
	}
	if len(selection.Files) > 0 {
		fmt.Fprintln(out, "files:")
		for _, entry := range selection.Files {
			fmt.Fprintf(out, "  %s\n", entry.Path)
		}
	}
	return nil
}

func printCondition(out io.Writer, label string, condition *Condition) {
	if condition == nil {
		return
	}
	if condition.Reason == "" {
		fmt.Fprintf(out, "%s: %s\n", label, condition.Path)
	} else {
		fmt.Fprintf(out, "%s: %s: %s\n", label, condition.Path, condition.Reason)
	}
}

func printConditions(out io.Writer, label string, conditions []Condition) {
	if len(conditions) == 1 {
		printCondition(out, label, &conditions[0])
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
