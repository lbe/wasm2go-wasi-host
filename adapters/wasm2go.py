import os
import shlex
import subprocess
from pathlib import Path
from typing import Dict, List, Tuple

WASM2GO_RUN: List[str] = shlex.split(os.getenv("WASM2GO_RUN", "wasm2go-run"))

def get_name() -> str:
    return "wasm2go"

def get_version() -> str:
    return subprocess.run(WASM2GO_RUN[0:1] + ["--version"], capture_output=True, text=True, check=True).stdout.strip()

def get_wasi_versions() -> List[str]:
    return ["wasm32-wasip1"]

def get_wasi_worlds() -> List[str]:
    return ["wasi:cli/command"]

def compute_argv(test_path: str,
                 args_env_dirs: Tuple[List[str], Dict[str, str], List[Tuple[Path, str]]],
                 proposals: List[str],
                 wasi_world: str,
                 wasi_version: str) -> List[str]:
    args, env, dirs = args_env_dirs
    argv = list(WASM2GO_RUN)
    for k, v in env.items():
        argv.extend(["--env", f"{k}={v}"])
    for host, guest in dirs:
        argv.extend(["--dir", f"{host}::{guest}"])
    argv.append(test_path)
    argv.extend(args)
    return argv
