package prompt

import (
	_ "embed"
	"fmt"
	"os"
)

//go:embed conventions.md
var defaultConventions string

func LoadConventions(path string) string {
	if path == "" {
		return defaultConventions
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "conventions file: %v (using default)\n", err)
		return defaultConventions
	}
	return string(data)
}
