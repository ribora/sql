package main

import (
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
	generate(path, needTree)
}

func generate(path string, needTree bool) {
	for f := range walk(path, needTree) {
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_gen.go") {
			continue
		}
		packageName, lexems := parse(f)
		for _, lexem := range lexems {
			write(filepath.Dir(f), packageName, lexem)
		}
	}
}
