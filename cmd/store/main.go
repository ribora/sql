package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	os.Args = append(os.Args, ".")
	path := os.Args[1]
	var needTree bool
	if strings.HasSuffix(path, "/...") {
		needTree = true
		path = path[:len(path)-4]
	}
	if err := generate(path, needTree); err != nil {
		fmt.Fprintln(os.Stderr, "store:", err)
		os.Exit(1)
	}
}

func generate(path string, needTree bool) error {
	for f, err := range walk(path, needTree) {
		if err != nil {
			return err
		}
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_gen.go") {
			continue
		}
		packageName, lexems, err := parse(f)
		if err != nil {
			return err
		}
		for _, lexem := range lexems {
			if err := write(filepath.Dir(f), packageName, lexem); err != nil {
				return fmt.Errorf("%s: %w", f, err)
			}
		}
	}
	return nil
}
