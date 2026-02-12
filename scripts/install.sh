#!/usr/bin/env bash
set -euo pipefail

SOURCE="release"
VERSION=""
TARGET_ENV="auto"
TARGET_ARCH="auto"
MARGIN_ROOT=""
CLI_PATH=""
SUBLIME_INSTALLED_PACKAGES_DIR=""
SUBLIME_USER_DIR=""
PLUGIN_MODE="package"
YES=0
DRY_RUN=0
GITHUB_REPO="${MARGIN_INSTALL_REPO:-}"

MARGIN_ROOT_EXPLICIT=0
CLI_PATH_EXPLICIT=0
SUBLIME_INSTALLED_EXPLICIT=0
SUBLIME_USER_EXPLICIT=0

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

log() {
  printf 'margin-install: %s\n' "$*"
}

die() {
  printf 'margin-install: ERROR: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<USAGE
Usage: install.sh [options]

Options:
  --source release|local|auto
  --version <tag>
  --target-env auto|linux|macos|windows
  --target-arch auto|amd64|arm64
  --margin-root <path>
  --cli-path <path>
  --sublime-installed-packages-dir <path>
  --sublime-user-dir <path>
  --plugin-mode package|unpacked
  --github-repo <owner/repo>
  --yes
  --dry-run
  -h, --help
USAGE
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

require_arg_value() {
  local flag="$1"
  if [[ $# -lt 2 || -z "$2" ]]; then
    die "missing value for $flag"
  fi
}

is_wsl() {
  [[ -n "${WSL_DISTRO_NAME:-}" ]] && return 0
  [[ -r /proc/version ]] && grep -qiE 'microsoft|wsl' /proc/version
}

detect_host_env() {
  case "$(uname -s)" in
    Linux*) echo "linux" ;;
    Darwin*) echo "macos" ;;
    MINGW*|MSYS*|CYGWIN*) echo "windows" ;;
    *) die "unsupported host OS: $(uname -s)" ;;
  esac
}

detect_host_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *) die "unsupported architecture: $(uname -m)" ;;
  esac
}

resolve_target_env() {
  local host_env="$1"
  local resolved="$TARGET_ENV"
  if [[ "$resolved" == "auto" ]]; then
    if is_wsl; then
      if [[ -t 0 && $YES -eq 0 ]]; then
        printf 'margin-install: Running in WSL with --target-env auto.\n' >&2
        printf 'margin-install: Choose install target [windows/linux]: ' >&2
        if ! read -r resolved </dev/tty; then
          die "failed to read WSL target selection; pass --target-env windows or --target-env linux"
        fi
      else
        die "WSL detected with --target-env auto in non-interactive mode. Pass --target-env windows or --target-env linux"
      fi
    else
      resolved="$host_env"
    fi
  fi
  case "$resolved" in
    linux|macos|windows) ;;
    *) die "invalid --target-env: $resolved" ;;
  esac
  printf '%s' "$resolved"
}

resolve_target_arch() {
  local host_arch="$1"
  local resolved="$TARGET_ARCH"
  [[ "$resolved" == "auto" ]] && resolved="$host_arch"
  case "$resolved" in
    amd64|arm64) ;;
    *) die "invalid --target-arch: $resolved" ;;
  esac
  printf '%s' "$resolved"
}

windows_appdata() {
  local appdata=""
  if is_wsl; then
    require_cmd cmd.exe
    appdata="$(cmd.exe /c echo %APPDATA% 2>/dev/null | tr -d '\r')"
  else
    appdata="${APPDATA:-}"
  fi
  [[ -n "$appdata" ]] || die "failed to determine Windows APPDATA"
  printf '%s' "$appdata"
}

default_margin_root() {
  case "$1" in
    linux) printf '%s' "$HOME/.local/share/margin" ;;
    macos) printf '%s' "$HOME/Library/Application Support/Margin" ;;
    windows) printf '%s\\Margin' "$(windows_appdata)" ;;
  esac
}

default_sublime_base() {
  case "$1" in
    linux) printf '%s' "$HOME/.config/sublime-text" ;;
    macos) printf '%s' "$HOME/Library/Application Support/Sublime Text" ;;
    windows) printf '%s\\Sublime Text' "$(windows_appdata)" ;;
  esac
}

windows_to_posix_path() {
  local p="$1"
  if is_wsl; then
    require_cmd wslpath
    wslpath -u "$p"
  elif command -v cygpath >/dev/null 2>&1; then
    cygpath -u "$p"
  else
    die "cannot convert Windows path on this host: $p"
  fi
}

posix_to_windows_path() {
  local p="$1"
  if is_wsl; then
    require_cmd wslpath
    wslpath -w "$p"
  elif command -v cygpath >/dev/null 2>&1; then
    cygpath -w "$p"
  else
    die "cannot convert POSIX path to Windows on this host: $p"
  fi
}

normalize_for_target() {
  local target="$1"
  local p="$2"
  if [[ "$target" == "windows" ]]; then
    case "$p" in
      [A-Za-z]:\\*|[A-Za-z]:/*) printf '%s' "${p//\//\\}" ;;
      *) posix_to_windows_path "$p" ;;
    esac
  else
    case "$p" in
      [A-Za-z]:\\*|[A-Za-z]:/*) windows_to_posix_path "$p" ;;
      *) printf '%s' "$p" ;;
    esac
  fi
}

host_path_for_write() {
  local target="$1"
  local p="$2"
  if [[ "$target" == "windows" ]]; then
    windows_to_posix_path "$p"
  else
    printf '%s' "$p"
  fi
}

sha256_of_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    die "sha256 tool missing (sha256sum or shasum required)"
  fi
}

checksum_for_asset() {
  local checksums_file="$1"
  local asset_name="$2"
  awk -v n="$asset_name" '$2==n {print $1; exit}' "$checksums_file"
}

resolve_repo_default() {
  if [[ -n "$GITHUB_REPO" ]]; then
    printf '%s' "$GITHUB_REPO"
    return
  fi
  if command -v git >/dev/null 2>&1 && git -C "$REPO_ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    local origin
    origin="$(git -C "$REPO_ROOT" remote get-url origin 2>/dev/null || true)"
    if [[ "$origin" =~ github\.com[:/]([^/]+/[^/.]+)(\.git)?$ ]]; then
      printf '%s' "${BASH_REMATCH[1]}"
      return
    fi
  fi
  printf '%s' "s992/margin"
}

latest_release_tag() {
  require_cmd curl
  local json tag
  json="$(curl -fsSL "https://api.github.com/repos/$1/releases/latest")" || return 1
  tag="$(printf '%s' "$json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
  [[ -n "$tag" ]] || return 1
  printf '%s' "$tag"
}

download_release_asset() {
  require_cmd curl
  curl -fsSL "https://github.com/$1/releases/download/$2/$3" -o "$4"
}

resolve_release_asset_name() {
  local version_no_v="$1" env="$2" arch="$3"
  local ext="tar.gz" goos=""
  case "$env" in
    linux) goos="linux" ;;
    macos) goos="darwin" ;;
    windows) goos="windows"; ext="zip" ;;
    *) return 1 ;;
  esac
  printf 'margin_%s_%s_%s.%s' "$version_no_v" "$goos" "$arch" "$ext"
}

build_local_cli() {
  require_cmd go
  local env="$1" arch="$2" out="$3" goos=""
  case "$env" in
    linux) goos="linux" ;;
    macos) goos="darwin" ;;
    windows) goos="windows" ;;
  esac
  log "building local CLI for $goos/$arch"
  (( DRY_RUN == 1 )) && return
  (
    cd "$REPO_ROOT/cli-go"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$arch" go build -o "$out" ./cmd/margin
  )
}

build_local_plugin_package() {
  require_cmd zip
  (( DRY_RUN == 1 )) && return
  (
    cd "$REPO_ROOT/sublime-plugin/Margin"
    zip -rq "$1" . -x '*/__pycache__/*' '*.pyc'
  )
}

install_file() {
  (( DRY_RUN == 1 )) && {
    log "dry-run: install $1 -> $2"
    return
  }
  mkdir -p "$(dirname "$2")"
  cp "$1" "$2"
}

install_unpacked_plugin() {
  local package_src="$1"
  local dst="$2"
  (( DRY_RUN == 1 )) && {
    log "dry-run: install unpacked plugin -> $dst"
    return
  }
  require_cmd unzip
  local extract_dir
  extract_dir="$(mktemp -d)"
  unzip -q "$package_src" -d "$extract_dir"
  local extracted="$extract_dir"
  if [[ -d "$extract_dir/Margin" ]]; then
    extracted="$extract_dir/Margin"
  fi
  [[ -f "$extracted/margin.py" ]] || die "plugin package is missing margin.py at package root"
  rm -rf "$dst"
  mkdir -p "$dst"
  cp -R "$extracted"/. "$dst"/
  rm -rf "$extract_dir"
}

merge_settings_json() {
  (( DRY_RUN == 1 )) && {
    log "dry-run: merge settings at $1"
    return
  }
  mkdir -p "$(dirname "$1")"
  python3 - "$1" "$2" "$3" <<'PY'
import json
import pathlib
import sys

settings_path = pathlib.Path(sys.argv[1])
cli_path = sys.argv[2]
margin_root = sys.argv[3]

obj = {}
if settings_path.exists():
    raw = settings_path.read_text(encoding="utf-8")
    if raw.strip():
        parsed = json.loads(raw)
        if not isinstance(parsed, dict):
            raise SystemExit(f"settings file must be a JSON object: {settings_path}")
        obj = parsed
obj["margin_cli_path"] = cli_path
if margin_root:
    obj["margin_root"] = margin_root
settings_path.write_text(json.dumps(obj, indent=2, sort_keys=True) + "\n", encoding="utf-8")
PY
}

extract_cli_from_archive() {
  local archive="$1" env="$2" out="$3" extract_dir="$4"
  (( DRY_RUN == 1 )) && return
  mkdir -p "$extract_dir"
  case "$archive" in
    *.tar.gz) require_cmd tar; tar -xzf "$archive" -C "$extract_dir" ;;
    *.zip) require_cmd unzip; unzip -q "$archive" -d "$extract_dir" ;;
    *) die "unsupported archive format: $archive" ;;
  esac
  local binary="margin"
  [[ "$env" == "windows" ]] && binary="margin.exe"
  local found
  found="$(find "$extract_dir" -type f -name "$binary" | head -n1 || true)"
  [[ -n "$found" ]] || die "failed to find $binary in archive"
  cp "$found" "$out"
}

install_from_release() {
  local repo="$1" tag="$2" env="$3" arch="$4" cli_out="$5" plugin_out="$6" tmp="$7"
  local asset archive checksums expected actual plugin_expected plugin_actual
  asset="$(resolve_release_asset_name "${tag#v}" "$env" "$arch")" || return 1
  archive="$tmp/$asset"
  checksums="$tmp/checksums.txt"
  log "downloading release assets for $repo@$tag"
  (( DRY_RUN == 1 )) && return 0
  download_release_asset "$repo" "$tag" "$asset" "$archive" || return 1
  download_release_asset "$repo" "$tag" "checksums.txt" "$checksums" || return 1
  download_release_asset "$repo" "$tag" "Margin.sublime-package" "$plugin_out" || return 1
  expected="$(checksum_for_asset "$checksums" "$asset")"
  [[ -n "$expected" ]] || return 1
  actual="$(sha256_of_file "$archive")"
  [[ "$actual" == "$expected" ]] || die "checksum mismatch for $asset"
  plugin_expected="$(checksum_for_asset "$checksums" "Margin.sublime-package")"
  [[ -n "$plugin_expected" ]] || die "checksum missing for Margin.sublime-package"
  plugin_actual="$(sha256_of_file "$plugin_out")"
  [[ "$plugin_actual" == "$plugin_expected" ]] || die "checksum mismatch for Margin.sublime-package"
  extract_cli_from_archive "$archive" "$env" "$cli_out" "$tmp/extract"
}

install_from_local() {
  [[ -d "$REPO_ROOT/cli-go" ]] || die "local source mode requires repository checkout"
  build_local_cli "$1" "$2" "$3"
  build_local_plugin_package "$5"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --source) require_arg_value "$1" "${2-}"; SOURCE="${2-}"; shift 2 ;;
    --version) require_arg_value "$1" "${2-}"; VERSION="${2-}"; shift 2 ;;
    --target-env) require_arg_value "$1" "${2-}"; TARGET_ENV="${2-}"; shift 2 ;;
    --target-arch) require_arg_value "$1" "${2-}"; TARGET_ARCH="${2-}"; shift 2 ;;
    --margin-root) require_arg_value "$1" "${2-}"; MARGIN_ROOT="${2-}"; MARGIN_ROOT_EXPLICIT=1; shift 2 ;;
    --cli-path) require_arg_value "$1" "${2-}"; CLI_PATH="${2-}"; CLI_PATH_EXPLICIT=1; shift 2 ;;
    --sublime-installed-packages-dir) require_arg_value "$1" "${2-}"; SUBLIME_INSTALLED_PACKAGES_DIR="${2-}"; SUBLIME_INSTALLED_EXPLICIT=1; shift 2 ;;
    --sublime-user-dir) require_arg_value "$1" "${2-}"; SUBLIME_USER_DIR="${2-}"; SUBLIME_USER_EXPLICIT=1; shift 2 ;;
    --plugin-mode) require_arg_value "$1" "${2-}"; PLUGIN_MODE="${2-}"; shift 2 ;;
    --github-repo) require_arg_value "$1" "${2-}"; GITHUB_REPO="${2-}"; shift 2 ;;
    --yes) YES=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

case "$SOURCE" in release|local|auto) ;; *) die "invalid --source: $SOURCE" ;; esac
case "$PLUGIN_MODE" in package|unpacked) ;; *) die "invalid --plugin-mode: $PLUGIN_MODE" ;; esac

host_env="$(detect_host_env)"
host_arch="$(detect_host_arch)"
target_env="$(resolve_target_env "$host_env")"
target_arch="$(resolve_target_arch "$host_arch")"

if [[ "$target_env" == "windows" && "$host_env" != "windows" ]] && ! is_wsl; then
  die "Windows target from non-Windows host is supported only in WSL. Use install.ps1 on Windows."
fi

if (( MARGIN_ROOT_EXPLICIT == 0 )); then
  MARGIN_ROOT="$(default_margin_root "$target_env")"
else
  MARGIN_ROOT="$(normalize_for_target "$target_env" "$MARGIN_ROOT")"
fi

if (( CLI_PATH_EXPLICIT == 0 )); then
  if [[ "$target_env" == "windows" ]]; then
    CLI_PATH="$MARGIN_ROOT\\bin\\margin.exe"
  else
    CLI_PATH="$MARGIN_ROOT/bin/margin"
  fi
else
  CLI_PATH="$(normalize_for_target "$target_env" "$CLI_PATH")"
fi

sublime_base="$(default_sublime_base "$target_env")"
if (( SUBLIME_INSTALLED_EXPLICIT == 0 )); then
  if [[ "$target_env" == "windows" ]]; then
    SUBLIME_INSTALLED_PACKAGES_DIR="$sublime_base\\Installed Packages"
  else
    SUBLIME_INSTALLED_PACKAGES_DIR="$sublime_base/Installed Packages"
  fi
else
  SUBLIME_INSTALLED_PACKAGES_DIR="$(normalize_for_target "$target_env" "$SUBLIME_INSTALLED_PACKAGES_DIR")"
fi
if (( SUBLIME_USER_EXPLICIT == 0 )); then
  if [[ "$target_env" == "windows" ]]; then
    SUBLIME_USER_DIR="$sublime_base\\Packages\\User"
  else
    SUBLIME_USER_DIR="$sublime_base/Packages/User"
  fi
else
  SUBLIME_USER_DIR="$(normalize_for_target "$target_env" "$SUBLIME_USER_DIR")"
fi

cli_host_path="$(host_path_for_write "$target_env" "$CLI_PATH")"
installed_packages_host_dir="$(host_path_for_write "$target_env" "$SUBLIME_INSTALLED_PACKAGES_DIR")"
user_dir_host="$(host_path_for_write "$target_env" "$SUBLIME_USER_DIR")"
settings_host_path="$user_dir_host/Margin.sublime-settings"
packages_root_host="$(dirname "$user_dir_host")"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

cli_artifact="$tmp_dir/margin"
[[ "$target_env" == "windows" ]] && cli_artifact="$tmp_dir/margin.exe"
plugin_artifact="$tmp_dir/Margin.sublime-package"

repo="$(resolve_repo_default)"
tag="$VERSION"
if [[ "$SOURCE" != "local" && -z "$tag" ]]; then
  if (( DRY_RUN == 1 )); then
    [[ "$SOURCE" == "release" ]] && tag="<latest>"
  else
    tag="$(latest_release_tag "$repo" 2>/dev/null || true)"
    [[ -n "$tag" ]] || [[ "$SOURCE" == "auto" ]] || die "failed to determine latest release for $repo"
  fi
fi

source_used="$SOURCE"
if [[ "$SOURCE" == "release" ]]; then
  install_from_release "$repo" "$tag" "$target_env" "$target_arch" "$cli_artifact" "$plugin_artifact" "$tmp_dir" \
    || die "release install failed for $repo@$tag"
elif [[ "$SOURCE" == "auto" ]]; then
  if [[ -n "$tag" ]] && install_from_release "$repo" "$tag" "$target_env" "$target_arch" "$cli_artifact" "$plugin_artifact" "$tmp_dir"; then
    source_used="release"
  else
    log "release install unavailable; falling back to local source"
    source_used="local"
    install_from_local "$target_env" "$target_arch" "$cli_artifact" "$PLUGIN_MODE" "$plugin_artifact"
  fi
else
  install_from_local "$target_env" "$target_arch" "$cli_artifact" "$PLUGIN_MODE" "$plugin_artifact"
fi

log "host=$host_env/$host_arch target=$target_env/$target_arch source=$source_used plugin_mode=$PLUGIN_MODE"
log "margin_root=$MARGIN_ROOT"
log "cli_path=$CLI_PATH"
log "installed_packages_dir=$SUBLIME_INSTALLED_PACKAGES_DIR"
log "user_dir=$SUBLIME_USER_DIR"

install_file "$cli_artifact" "$cli_host_path"
if (( DRY_RUN == 0 )) && [[ "$target_env" != "windows" ]]; then
  chmod 0755 "$cli_host_path"
fi

if [[ "$PLUGIN_MODE" == "package" ]]; then
  install_file "$plugin_artifact" "$installed_packages_host_dir/Margin.sublime-package"
else
  install_unpacked_plugin "$plugin_artifact" "$packages_root_host/Margin"
fi

explicit_margin_root=""
(( MARGIN_ROOT_EXPLICIT == 1 )) && explicit_margin_root="$MARGIN_ROOT"
merge_settings_json "$settings_host_path" "$CLI_PATH" "$explicit_margin_root"

log "installation complete"
