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

// deduplicateInterfaceMethods removes duplicate method signatures from generated
// Go interface blocks. This is a tactical workaround for a wasm2go bug where a
// WebAssembly module that imports the same host function more than once causes
// wasm2go to emit duplicate interface method declarations, which are invalid Go.
// TODO: Remove this function once the upstream wasm2go issue is fixed.
// See: wasm2go-duplicate-import-issue.md
func deduplicateInterfaceMethods(src string) string {
	var sb strings.Builder
	lines := strings.Split(src, "\n")
	inInterface := false
	seenMethods := make(map[string]bool)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "type ") && strings.HasSuffix(trimmed, " interface {") {
			inInterface = true
			seenMethods = make(map[string]bool)
			sb.WriteString(line)
			sb.WriteString("\n")
			continue
		}

		if inInterface {
			if trimmed == "}" {
				inInterface = false
				sb.WriteString(line)
				sb.WriteString("\n")
				continue
			}

			if trimmed != "" {
				if seenMethods[trimmed] {
					continue
				}
				seenMethods[trimmed] = true
			}
		}

		sb.WriteString(line)
		sb.WriteString("\n")
	}

	return strings.TrimSuffix(sb.String(), "\n")
}
