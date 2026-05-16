package main

import (
	"fmt"
	"regexp"
	"strings"
)

var newFuncRegexp = regexp.MustCompile(`func New\(([^)]+)\) \*Module`)

func parseImports(src string) ([]string, error) {
	match := newFuncRegexp.FindStringSubmatch(src)
	if match == nil {
		return nil, fmt.Errorf("New function not found")
	}

	params := match[1]
	var imports []string
	parts := strings.Split(params, ",")
	for _, part := range parts {
		fields := strings.Fields(part)
		if len(fields) >= 2 {
			imports = append(imports, fields[1])
		}
	}

	return imports, nil
}

func transpile(wasmPath string) (string, error) {
	return "", nil
}
