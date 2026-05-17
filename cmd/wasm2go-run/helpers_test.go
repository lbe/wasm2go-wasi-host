package main

import (
	"path/filepath"
	"runtime"
)

func testdata(rel string) string {
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	return filepath.Join(dir, rel)
}
