// Команда запускается через go generate из internal/config.
package main

import (
	"fmt"
	"os"

	"hotels/internal/pkg/rtconfig/codegen"
	"hotels/internal/pkg/rtconfig/system"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	generator, err := codegen.New(codegen.Options{
		SchemaPath:        "../../config/rtconfig.yaml",
		OutputPath:        "config.go",
		SystemDefinitions: system.Definitions(),
	})
	if err != nil {
		return err
	}

	return generator.Generate()
}
