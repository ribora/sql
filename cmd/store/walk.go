package main

import (
	"io/fs"
	"iter"
	"os"
	"path/filepath"
)

func walk(path string, needTree bool) iter.Seq[string] {
	return func(yield func(string) bool) {
		info, err := os.Lstat(path)
		if err != nil {
			panic(err)
		}
		inspect(path, fs.FileInfoToDirEntry(info), yield, needTree, true)
	}
}

func inspect(path string, d fs.DirEntry, yield func(string) bool, needTree, isRoot bool) bool {
	if !d.IsDir() {
		return yield(path)
	}
	if d.IsDir() && (needTree || isRoot) {
		entities, err := os.ReadDir(path)
		if err != nil {
			panic(err)
		}
		for _, entity := range entities {
			if !inspect(filepath.Join(path, entity.Name()), entity, yield, needTree, false) {
				return false
			}
		}
	}
	return true
}
