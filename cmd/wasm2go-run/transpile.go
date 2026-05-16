package main

import (
	"fmt"
	"os/exec"
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
	for _, part := range strings.Split(params, ",") {
		fields := strings.Fields(part)
		if len(fields) >= 2 {
			imports = append(imports, fields[1])
		}
	}

	return imports, nil
}

func transpile(wasmPath string) (string, error) {
	cmd := exec.Command("wasm2go", wasmPath)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
