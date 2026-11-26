//go:build e2e

package wasihost_test

import (
	"encoding/json"
	"os"
	"testing"
)

type e2eCase struct {
	Suite      string   `json:"suite"`
	Name       string   `json:"name"`
	StdioOnly  bool     `json:"stdioOnly"`
	Smoke      bool     `json:"smoke"`
	UnitMirror []string `json:"unitMirror"`
}

func loadE2ECases(t *testing.T) []e2eCase {
	t.Helper()
	data, err := os.ReadFile("testdata/e2e_cases.json")
	if err != nil {
		t.Fatalf("read e2e manifest: %v", err)
	}
	var cases []e2eCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("parse e2e manifest: %v", err)
	}
	return cases
}

func filterE2ECases(cases []e2eCase, suite string, smokeOnly bool) []wasiTestsuiteCase {
	out := make([]wasiTestsuiteCase, 0, len(cases))
	for _, c := range cases {
		if c.Suite != suite {
			continue
		}
		if smokeOnly && !c.Smoke {
			continue
		}
		if !smokeOnly && c.Smoke && suite == "rust" {
			continue
		}
		out = append(out, wasiTestsuiteCase{
			name:      c.Name,
			stdioOnly: c.StdioOnly,
		})
	}
	return out
}

func TestWasiTestsuiteSmoke(t *testing.T) {
	runWasiTestsuiteCases(t, "rust", filterE2ECases(loadE2ECases(t), "rust", true))
}

func TestWasiTestsuiteRust(t *testing.T) {
	runWasiTestsuiteCases(t, "rust", filterE2ECases(loadE2ECases(t), "rust", false))
}

func TestWasiTestsuiteC(t *testing.T) {
	runWasiTestsuiteCases(t, "c", filterE2ECases(loadE2ECases(t), "c", false))
}

func TestWasiTestsuiteAssemblyScript(t *testing.T) {
	runWasiTestsuiteCases(t, "assemblyscript", filterE2ECases(loadE2ECases(t), "assemblyscript", false))
}
