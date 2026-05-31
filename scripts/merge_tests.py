#!/usr/bin/env python3
"""One-off merger for wasihost test file consolidation."""
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent


def parse_go(path: Path):
    text = path.read_text()
    pkg = re.search(r"^package\s+(\S+)", text, re.M).group(1)
    imports = []
    rest = re.sub(r"^package\s+\S+\s*\n+", "", text, count=1)
    while True:
        m = re.match(
            r'(?s)^import\s+(?:\(\s*(.*?)\s*\)|(?:"[^"]+"|\S+\s+"[^"]+"))\s*\n',
            rest,
        )
        if not m:
            break
        block = m.group(1)
        if block is not None:
            for line in block.split("\n"):
                line = line.strip().rstrip(",")
                if line and not line.startswith("//"):
                    imports.append(line)
        else:
            single = re.match(r'^import\s+("(?:\\.|[^"\\])*")', rest)
            if single:
                imports.append(single.group(1))
        rest = rest[m.end() :]
    return pkg, imports, rest.strip() + "\n"


def clean_body(body: str) -> str:
    return re.sub(r'^import\s+(?:\([^)]*\)|"[^"]+"|\S+\s+"[^"]+")\s*\n', "", body, flags=re.M)


def dedupe_imports(import_lines):
    seen = []
    for line in import_lines:
        if line not in seen:
            seen.append(line)
    seen.sort(key=lambda s: s)
    return seen


def write_package(out_path: Path, package: str, import_lines, bodies):
    imps = dedupe_imports(import_lines)
    content = f"package {package}\n\nimport (\n\t" + "\n\t".join(imps) + "\n)\n\n" + "\n".join(bodies)
    out_path.write_text(content)


def merge_wasihost(out_name: str, sources):
    imports = []
    bodies = []
    for src in sources:
        if isinstance(src, tuple):
            path, mode = src
            text = (ROOT / path).read_text()
            _, imps, body = parse_go(ROOT / path)
            imports.extend(imps)
            if mode.startswith("after:"):
                marker = mode.split(":", 1)[1]
                bodies.append(clean_body(body[body.index(marker) :]))
            else:
                bodies.append(clean_body(body))
        else:
            _, imps, body = parse_go(ROOT / src)
            imports.extend(imps)
            if src == "helpers_test.go":
                bodies.append(clean_body(body[body.index("func TestWASIStubs") :]))
            else:
                bodies.append(clean_body(body))
    write_package(ROOT / out_name, "wasihost", imports, bodies)


def build_test_helpers():
    ga_body = parse_go(ROOT / "group_a_test.go")[2]
    ga_helpers = ga_body[: ga_body.index("func TestFoundationSyscalls")]
    h_body = parse_go(ROOT / "helpers_test.go")[2]
    h_helpers = h_body[h_body.index("type ReaddirDirent") : h_body.index("func TestWASIStubs")]
    tf_body = parse_go(ROOT / "test_fd_helpers_test.go")[2]
    imports = (
        parse_go(ROOT / "group_a_test.go")[1]
        + parse_go(ROOT / "helpers_test.go")[1]
        + parse_go(ROOT / "test_fd_helpers_test.go")[1]
    )
    write_package(ROOT / "test_helpers_test.go", "wasihost", imports, [ga_helpers, h_helpers, tf_body])


def build_guardrails():
    bodies = []
    imports = []
    for name in ("anti_regression_guard_test.go", "docs_compliance_test.go", "api_removal_test.go"):
        text = (ROOT / name).read_text().replace("group_a_test.go", "syscall_foundation_test.go")
        _, imps, body = parse_go_from_text(text)
        imports.extend(imps)
        bodies.append(body)

    cl = ROOT / "config_layer_correction2_safety_net_test.go"
    if cl.exists():
        text = cl.read_text().replace("//go:build e2e\n\n", "")
        text = text.replace(
            "TestConfigLayerCorrection2FullSafetyNet",
            "TestAntiRegressionGuardrails_Wasm2goRunTemplateUsesModuleConfig",
        )
        text = text.replace(
            "// TestConfigLayerCorrection2FullSafetyNet verifies wasm2go-run template.go\n// uses ModuleConfig builder only and does not reference legacy root options.\n",
            "// TestAntiRegressionGuardrails_Wasm2goRunTemplateUsesModuleConfig verifies\n// wasm2go-run template.go uses ModuleConfig builder only.\n",
        )
        _, imps, body = parse_go_from_text(text)
        imports.extend(imps)
        bodies.append(body)

    write_package(ROOT / "guardrails_test.go", "wasihost_test", imports, bodies)


def parse_go_from_text(text: str):
    pkg = re.search(r"^package\s+(\S+)", text, re.M).group(1)
    imports = []
    rest = re.sub(r"^package\s+\S+\s*\n+", "", text, count=1)
    while True:
        m = re.match(
            r'(?s)^import\s+(?:\(\s*(.*?)\s*\)|(?:"[^"]+"|\S+\s+"[^"]+"))\s*\n',
            rest,
        )
        if not m:
            break
        block = m.group(1)
        if block is not None:
            for line in block.split("\n"):
                line = line.strip().rstrip(",")
                if line and not line.startswith("//"):
                    imports.append(line)
        else:
            single = re.match(r'^import\s+("(?:\\.|[^"\\])*")', rest)
            if single:
                imports.append(single.group(1))
        rest = rest[m.end() :]
    return pkg, imports, rest.strip() + "\n"


def build_e2e_manifest():
    smoke_names = {
        "path_open_read_write",
        "stdio",
        "sched_yield",
        "nofollow_errors",
        "path_open_missing",
        "overwrite_preopen",
        "file_truncation",
    }
    cur = {}
    for f in sorted(ROOT.glob("e2e_*.go")):
        if f.name in ("e2e_helpers_test.go", "e2e_quality_gate_test.go", "e2e_inventory_test.go"):
            continue
        for m in re.finditer(
            r'\{name:\s*"([^"]+)"(?:,\s*wantPass:\s*(true|false))?(?:,\s*stdioOnly:\s*(true))?',
            f.read_text(),
        ):
            cur[m.group(1)] = (
                (m.group(2) or "true") == "true",
                m.group(3) == "true",
            )

    existing_mirrors = {}
    manifest = ROOT / "testdata" / "e2e_cases.json"
    if manifest.exists():
        for row in json.loads(manifest.read_text()):
            existing_mirrors[row["name"]] = row.get("unitMirror", [])

    cases = []
    for suite in ("rust", "c", "assemblyscript"):
        base = ROOT / "wasi-testsuite" / "tests" / suite
        for wasm in sorted(base.rglob("wasm32-wasip1/*.wasm")):
            name = wasm.stem
            wp, so = cur.get(name, (True, False))
            j = wasm.with_suffix(".json")
            if name not in cur and j.exists():
                cfg = json.loads(j.read_text())
                if cfg.get("exit_code", 0) != 0:
                    wp = False
                if cfg.get("dirs"):
                    so = False
            cases.append(
                {
                    "suite": suite,
                    "name": name,
                    "wantPass": wp,
                    "stdioOnly": so,
                    "smoke": suite == "rust" and name in smoke_names,
                    "unitMirror": existing_mirrors.get(name, []),
                }
            )
    manifest = ROOT / "testdata" / "e2e_cases.json"
    manifest.parent.mkdir(exist_ok=True)
    manifest.write_text(json.dumps(cases, indent=2) + "\n")
    inventory = '''//go:build e2e

package wasihost_test

import (
	"encoding/json"
	"os"
	"testing"
)

type e2eCase struct {
	Suite     string `json:"suite"`
	Name      string `json:"name"`
	WantPass  bool   `json:"wantPass"`
	StdioOnly bool   `json:"stdioOnly"`
	Smoke     bool   `json:"smoke"`
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
			wantPass:  c.WantPass,
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
'''
    (ROOT / "e2e_inventory_test.go").write_text(inventory)
    print(f"e2e manifest: {len(cases)} cases")


def merge_cache_tests():
    cache = ROOT / "cmd/wasm2go-run/cache_test.go"
    extra_files = [
        "cache_invalidation_test.go",
        "cache_concurrency_test.go",
        "cache_build_failure_test.go",
    ]
    _, imports, base_body = parse_go(cache)
    bodies = [base_body]
    for name in extra_files:
        _, imps, body = parse_go(ROOT / "cmd/wasm2go-run" / name)
        imports.extend(imps)
        bodies.append(clean_body(body))
    write_package(cache, "main", imports, bodies)


def main():
    build_test_helpers()

    merges = {
        "syscall_foundation_test.go": [
            ("group_a_test.go", "after:func TestFoundationSyscalls"),
            "helpers_test.go",
            "clock_args_test.go",
        ],
        "syscall_fd_test.go": [
            "group_a_fd_test.go",
            "group_8_allocate_test.go",
            "group_9_10_fd_renumber_test.go",
            "group_f_fdflags_test.go",
            "group_e_test.go",
            "readdir_parser_test.go",
        ],
        "syscall_path_test.go": [
            "group_3_interesting_paths_test.go",
            "group_9_10_path_link_test.go",
            "group_9_10_path_rename_test.go",
            "group_6_7_path_open_truncation_test.go",
            "group_dir_fd_test.go",
        ],
        "syscall_symlink_test.go": [
            "group_4_5_dangling_symlink_test.go",
            "group_4_5_symlink_create_and_follow_test.go",
            "group_4_5_symlink_filestat_test.go",
            "group_4_5_symlink_trailing_slash_test.go",
            "group_4_5_unlink_trailing_slash_test.go",
            "symlink_confinement_test.go",
            "symlink_mutation_confinement_test.go",
        ],
        "syscall_filestat_test.go": [
            "group_c_test.go",
            "group_c_atim_test.go",
            "group_6_7_path_filestat_test.go",
            "group_fstflags_test.go",
            "fd_filestat_set_size_test.go",
            "fd_filestat_set_times_test.go",
            "filestat_ino_test.go",
        ],
        "syscall_poll_test.go": [
            "group_11_poll_test.go",
            "poll_oneoff_fd_read_pollable_test.go",
            "poll_oneoff_fd_read_stdin_test.go",
        ],
        "syscall_sock_test.go": ["group_12_sock_test.go"],
        "syscall_fs_test.go": [
            "group_b_test.go",
            "fs_mutation_confinement_test.go",
            "fs_readonly_preopen_test.go",
            "fs_rights_test.go",
            "confinement_test.go",
            "preopen_test.go",
            "group_6_7_preopen_rights_test.go",
            "wasi_testsuite_rights_test.go",
        ],
    }
    for out, srcs in merges.items():
        merge_wasihost(out, srcs)

    build_guardrails()
    build_e2e_manifest()
    merge_cache_tests()


DELETE_SOURCES = [
    "group_a_test.go",
    "group_a_fd_test.go",
    "group_b_test.go",
    "group_c_test.go",
    "group_c_atim_test.go",
    "group_e_test.go",
    "group_f_fdflags_test.go",
    "group_fstflags_test.go",
    "group_3_interesting_paths_test.go",
    "group_4_5_dangling_symlink_test.go",
    "group_4_5_symlink_create_and_follow_test.go",
    "group_4_5_symlink_filestat_test.go",
    "group_4_5_symlink_trailing_slash_test.go",
    "group_4_5_unlink_trailing_slash_test.go",
    "group_6_7_path_filestat_test.go",
    "group_6_7_path_open_truncation_test.go",
    "group_6_7_preopen_rights_test.go",
    "group_8_allocate_test.go",
    "group_9_10_fd_renumber_test.go",
    "group_9_10_path_link_test.go",
    "group_9_10_path_rename_test.go",
    "group_11_poll_test.go",
    "group_12_sock_test.go",
    "group_dir_fd_test.go",
    "helpers_test.go",
    "test_fd_helpers_test.go",
    "clock_args_test.go",
    "confinement_test.go",
    "fd_filestat_set_size_test.go",
    "fd_filestat_set_times_test.go",
    "filestat_ino_test.go",
    "fs_mutation_confinement_test.go",
    "fs_readonly_preopen_test.go",
    "fs_rights_test.go",
    "poll_oneoff_fd_read_pollable_test.go",
    "poll_oneoff_fd_read_stdin_test.go",
    "preopen_test.go",
    "readdir_parser_test.go",
    "symlink_confinement_test.go",
    "symlink_mutation_confinement_test.go",
    "wasi_testsuite_rights_test.go",
    "anti_regression_guard_test.go",
    "docs_compliance_test.go",
    "api_removal_test.go",
    "e2e_smoke_test.go",
    "e2e_assemblyscript_test.go",
    "e2e_c_inventory_test.go",
    "e2e_rust_inventory_test.go",
    "e2e_group1_test.go",
    "e2e_group2_test.go",
    "e2e_group3_test.go",
    "e2e_group6a_test.go",
    "e2e_group6_7_test.go",
    "e2e_group8_test.go",
    "e2e_group9_10_test.go",
    "e2e_group11_test.go",
    "e2e_group12_test.go",
    "e2e_group45_test.go",
    "config_layer_correction2_safety_net_test.go",
    "cmd/wasm2go-run/cache_invalidation_test.go",
    "cmd/wasm2go-run/cache_concurrency_test.go",
    "cmd/wasm2go-run/cache_build_failure_test.go",
]

if __name__ == "__main__":
    main()
    for rel in DELETE_SOURCES:
        p = ROOT / rel
        if p.exists():
            p.unlink()
    print("merge complete")
