package jig

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

func shortError(err error) string {
	msg := strings.TrimSpace(err.Error())
	if idx := strings.IndexByte(msg, '\n'); idx >= 0 {
		return msg[:idx]
	}
	return msg
}

func printGroup(out io.Writer, label string, items []string) {
	if len(items) == 0 {
		return
	}
	sort.Strings(items)
	fmt.Fprintf(out, "%s:\n", label)
	for _, item := range items {
		fmt.Fprintf(out, "  %s\n", item)
	}
}
