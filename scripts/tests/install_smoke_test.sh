#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

run_package_mode_test() {
  local env_root="$TMP_DIR/package"
  local margin_root="$env_root/margin-root"
  local cli_path="$margin_root/bin/margin"
  local installed_dir="$env_root/sublime/Installed Packages"
  local user_dir="$env_root/sublime/Packages/User"

  mkdir -p "$user_dir"
  cat > "$user_dir/Margin.sublime-settings" <<'JSON'
{
  "margin_llm_timeout_seconds": 180
}
JSON

  "$ROOT_DIR/scripts/install.sh" \
    --source local \
    --target-env linux \
    --margin-root "$margin_root" \
    --cli-path "$cli_path" \
    --sublime-installed-packages-dir "$installed_dir" \
    --sublime-user-dir "$user_dir" \
    --plugin-mode package

  [[ -x "$cli_path" ]] || {
    echo "expected CLI binary at $cli_path" >&2
    return 1
  }
  [[ -f "$installed_dir/Margin.sublime-package" ]] || {
    echo "expected plugin package at $installed_dir/Margin.sublime-package" >&2
    return 1
  }
  python3 - <<PY
import zipfile
from pathlib import Path
z = zipfile.ZipFile(Path("$installed_dir/Margin.sublime-package"))
names = set(z.namelist())
assert "margin.py" in names, "expected margin.py at package root"
assert "Margin/margin.py" not in names, "unexpected nested package root Margin/"
PY

  python3 - <<PY
import json
from pathlib import Path

settings = json.loads(Path("$user_dir/Margin.sublime-settings").read_text(encoding="utf-8"))
assert settings["margin_cli_path"] == "$cli_path"
assert settings["margin_root"] == "$margin_root"
assert settings["margin_llm_timeout_seconds"] == 180
PY
}

run_unpacked_mode_test() {
  local env_root="$TMP_DIR/unpacked"
  local margin_root="$env_root/margin-root"
  local cli_path="$margin_root/bin/margin"
  local installed_dir="$env_root/sublime/Installed Packages"
  local user_dir="$env_root/sublime/Packages/User"

  "$ROOT_DIR/scripts/install.sh" \
    --source local \
    --target-env linux \
    --margin-root "$margin_root" \
    --cli-path "$cli_path" \
    --sublime-installed-packages-dir "$installed_dir" \
    --sublime-user-dir "$user_dir" \
    --plugin-mode unpacked

  [[ -f "$env_root/sublime/Packages/Margin/margin.py" ]] || {
    echo "expected unpacked plugin at $env_root/sublime/Packages/Margin" >&2
    return 1
  }
}

run_package_mode_test
run_unpacked_mode_test

echo "install_smoke_test: PASS"
