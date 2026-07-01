package jig

import (
	"errors"
	"fmt"
	"io"
)

func Validate(out io.Writer) error {
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	validation := validateDefinition(&ws.Def)
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
	fmt.Fprintln(out, "valid .jig.json")
	return nil
}
