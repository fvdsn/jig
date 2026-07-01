package jig

import (
	"errors"
	"fmt"
	"io"
)

type ValidateOptions struct {
	File string // validate this schema file instead of the current workspace's
}

func Validate(options ValidateOptions, out io.Writer) error {
	var def *Definition
	if options.File != "" {
		loaded, err := loadDefinition(options.File)
		if err != nil {
			return err
		}
		def = loaded
	} else {
		ws, err := loadWorkspace(false)
		if err != nil {
			return err
		}
		def = &ws.Def
	}
	validation := validateDefinition(def)
	if len(validation.Errors) > 0 {
		for _, msg := range validation.Errors {
			fmt.Fprintf(out, "error: %s\n", msg)
		}
		for _, msg := range validation.Warnings {
			fmt.Fprintf(out, "warning: %s\n", msg)
		}
		return errors.New("validation failed")
	}
	for _, msg := range validation.Warnings {
		fmt.Fprintf(out, "warning: %s\n", msg)
	}
	fmt.Fprintln(out, "valid schema")
	return nil
}
