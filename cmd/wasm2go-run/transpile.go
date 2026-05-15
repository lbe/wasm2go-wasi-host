package main

import (
	"fmt"
	"regexp"
	"strings"
)

func parseImports(src string) ([]string, error) {
	re := regexp.MustCompile(`func New\(([^)]+)\) \*Module`)
	match := re.FindStringSubmatch(src)
	if match == nil {
		return nil, fmt.Errorf("New function not found")
	}

	params := match[1]
	if params == "" {
		return []string{}, nil
	}

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
