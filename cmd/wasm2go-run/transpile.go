package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var newFuncRegexp = regexp.MustCompile(`func New\(([^)]+)\) \*Module`)

// parseImports extracts the import parameter names from the New function
// signature in the generated Go source.
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

// transpile invokes the wasm2go tool on wasmPath and returns the generated
// Go source with duplicate interface methods deduplicated.
func transpile(wasmPath string) (string, error) {
	cmd := exec.Command("wasm2go", wasmPath)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return deduplicateInterfaceMethods(string(out)), nil
}

// transpileCached returns the transpiled Go source for wasmPath, using the
// tier-1 cache when enabled. If the cache is disabled or there is no cache
// hit, it falls back to invoking wasm2go directly.
func transpileCached(wasmPath string, cfg Config) (string, error) {
	if !cacheEnabled(cfg) {
		return transpile(wasmPath)
	}
	key := transpileCacheKey(wasmPath)
	if src, hit := cacheGetTranspile(key); hit {
		return src, nil
	}
	return withTranspileCachePopulateLock(key, func() (string, error) {
		src, err := transpile(wasmPath)
		if err != nil {
			return "", err
		}
		if err := cachePutTranspile(key, src, currentTranspileCacheMeta()); err != nil {
			return "", err
		}
		return src, nil
	})
}

// deduplicateInterfaceMethods removes duplicate method signatures from generated
// Go interface blocks. This is a tactical workaround for a wasm2go bug where a
// WebAssembly module that imports the same host function more than once causes
// wasm2go to emit duplicate interface method declarations, which are invalid Go.
//
// TODO: Remove once wasm2go stops emitting duplicate interface methods for
// repeated host imports (see ncruces/wasm2go issue tracker).
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
			sb.WriteString(line + "\n")
			continue
		}

		if !inInterface {
			sb.WriteString(line + "\n")
			continue
		}

		// Inside an interface block.
		if trimmed == "}" {
			inInterface = false
			sb.WriteString(line + "\n")
			continue
		}

		if trimmed == "" {
			sb.WriteString(line + "\n")
			continue
		}

		if seenMethods[trimmed] {
			continue // drop duplicate method signature
		}
		seenMethods[trimmed] = true
		sb.WriteString(line + "\n")
	}

	return strings.TrimSuffix(sb.String(), "\n")
}
