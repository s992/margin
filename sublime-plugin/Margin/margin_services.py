import json
import os
import shutil
import subprocess

import sublime
try:
    from send2trash import send2trash as _send2trash
except Exception:
    _send2trash = None

try:
    from .margin_settings import CLI_TIMEOUT_SECONDS, margin_root, safe_int, settings
except ImportError:
    from margin_settings import CLI_TIMEOUT_SECONDS, margin_root, safe_int, settings


def cli_path():
    explicit = settings().get("margin_cli_path")
    if explicit and os.path.isfile(explicit):
        return explicit
    root = margin_root()
    binary_name = "margin.exe" if sublime.platform() == "windows" else "margin"
    candidate = os.path.join(root, "bin", binary_name)
    if os.path.isfile(candidate):
        return candidate
    return shutil.which("margin")


def run_process(cmd, stdin_text=None, timeout_s=None):
    popen_kwargs = {
        "stdin": subprocess.PIPE if stdin_text is not None else None,
        "stdout": subprocess.PIPE,
        "stderr": subprocess.PIPE,
    }
    if sublime.platform() == "windows":
        startupinfo = subprocess.STARTUPINFO()
        startupinfo.dwFlags |= subprocess.STARTF_USESHOWWINDOW
        popen_kwargs["startupinfo"] = startupinfo
        popen_kwargs["creationflags"] = 0x08000000  # CREATE_NO_WINDOW
    proc = subprocess.Popen(cmd, **popen_kwargs)
    in_bytes = None
    if stdin_text is not None:
        in_bytes = stdin_text.encode("utf-8")
    try:
        out_b, err_b = proc.communicate(in_bytes, timeout=timeout_s)
    except subprocess.TimeoutExpired:
        proc.kill()
        out_b, err_b = proc.communicate()
        out = out_b.decode("utf-8", "replace") if out_b else ""
        err = err_b.decode("utf-8", "replace") if err_b else ""
        err = (err + "\n" if err else "") + "Command timed out after {}s".format(timeout_s)
        return 124, out, err
    out = out_b.decode("utf-8", "replace") if out_b else ""
    err = err_b.decode("utf-8", "replace") if err_b else ""
    return proc.returncode, out, err


def run_cli_json(args):
    cli = cli_path()
    if not cli:
        raise RuntimeError("Margin CLI not found (set margin_cli_path or install margin in PATH)")
    timeout_s = safe_int(
        settings().get("margin_cli_timeout_seconds", CLI_TIMEOUT_SECONDS),
        CLI_TIMEOUT_SECONDS,
    )
    code, out, err = run_process([cli] + args, timeout_s=timeout_s)
    if code != 0:
        detail = (err or "").strip() or (out or "").strip() or "margin CLI failed"
        raise RuntimeError(detail)
    try:
        return json.loads(out or "null")
    except Exception:
        snippet = (out or "").strip()
        if len(snippet) > 400:
            snippet = snippet[:400] + "..."
        raise RuntimeError("margin CLI returned invalid JSON: {}".format(snippet or "<empty>"))


def move_to_trash(path):
    if not path or not os.path.exists(path):
        raise RuntimeError("File does not exist: {}".format(path))
    if _send2trash is not None:
        try:
            _send2trash(path)
            return
        except Exception:
            # Fall back to platform-specific commands when send2trash is unavailable at runtime.
            pass
    plat = sublime.platform()
    if plat == "windows":
        escaped = path.replace("'", "''")
        if os.path.isdir(path):
            script = (
                "Add-Type -AssemblyName Microsoft.VisualBasic; "
                "[Microsoft.VisualBasic.FileIO.FileSystem]::DeleteDirectory("
                "'{}', 'OnlyErrorDialogs', 'SendToRecycleBin')"
            ).format(escaped)
        else:
            script = (
                "Add-Type -AssemblyName Microsoft.VisualBasic; "
                "[Microsoft.VisualBasic.FileIO.FileSystem]::DeleteFile("
                "'{}', 'OnlyErrorDialogs', 'SendToRecycleBin')"
            ).format(escaped)
        code, _, err = run_process(["powershell", "-NoProfile", "-Command", script])
        if code != 0:
            raise RuntimeError((err or "").strip() or "Failed to move file to Recycle Bin")
        return
    if plat == "osx":
        escaped = path.replace("\\", "\\\\").replace('"', '\\"')
        code, _, err = run_process(
            [
                "osascript",
                "-e",
                'tell application "Finder" to delete POSIX file "{}"'.format(escaped),
            ]
        )
        if code != 0:
            raise RuntimeError((err or "").strip() or "Failed to move file to Trash")
        return
    gio = shutil.which("gio")
    if gio:
        code, _, err = run_process([gio, "trash", path])
        if code == 0:
            return
        raise RuntimeError((err or "").strip() or "Failed to move file to trash")
    trash_put = shutil.which("trash-put")
    if trash_put:
        code, _, err = run_process([trash_put, path])
        if code == 0:
            return
        raise RuntimeError((err or "").strip() or "Failed to move file to trash")
    raise RuntimeError("No trash command found (install gio or trash-cli)")
