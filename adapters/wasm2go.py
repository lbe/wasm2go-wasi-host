import os
from pathlib import Path
from typing import Dict, List, Tuple
WASM2GO_RUN: List[str] = ["wasm2go-run"]

def get_name() -> str:
    return ""

def get_version() -> str:
    return ""

def get_wasi_versions() -> List[str]:
    return []

def get_wasi_worlds() -> List[str]:
    return []

def compute_argv(test_path: str,
                 args_env_dirs: Tuple[List[str], Dict[str, str], List[Tuple[Path, str]]],
                 proposals: List[str],
                 wasi_world: str,
                 wasi_version: str) -> List[str]:
    return []
