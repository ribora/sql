package main

import (
	"io/fs"
	"iter"
	"os"
	"path/filepath"
)

func walk(path string, needTree bool) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		info, err := os.Lstat(path)
		if err != nil {
			yield("", err)
			return
		}
		inspect(path, fs.FileInfoToDirEntry(info), yield, needTree, true)
	}
}

func inspect(path string, d fs.DirEntry, yield func(string, error) bool, needTree, isRoot bool) bool {
	if !d.IsDir() {
		return yield(path, nil)
	}
	if needTree || isRoot {
		entities, err := os.ReadDir(path)
		if err != nil {
			return yield("", err)
		}
		for _, entity := range entities {
			if !inspect(filepath.Join(path, entity.Name()), entity, yield, needTree, false) {
				return false
			}
		}
	}
	return true
}
