import os
import tempfile
import time
import uuid
from datetime import datetime

import sublime

try:
    from .margin_settings import load_config, margin_root, safe_float, safe_int
except ImportError:
    from margin_settings import load_config, margin_root, safe_float, safe_int


_TICK_RUNNING = False


def stop_tick():
    global _TICK_RUNNING
    _TICK_RUNNING = False


def ensure_tick():
    global _TICK_RUNNING
    if _TICK_RUNNING:
        return
    _TICK_RUNNING = True
    sublime.set_timeout_async(_tick, 1000)


def _tick():
    global _TICK_RUNNING
    if not _TICK_RUNNING:
        return
    now_ts = time.time()
    for window in sublime.windows():
        for view in window.views():
            persist_view_if_needed(view, now_ts, force=False)
    sublime.set_timeout_async(_tick, 1000)


def ensure_layout(root):
    dirs = [
        os.path.join(root, "scratch", "current"),
        os.path.join(root, "scratch", "history"),
        os.path.join(root, "inbox"),
        os.path.join(root, "slack"),
        os.path.join(root, "index"),
        os.path.join(root, "bin"),
        os.path.join(root, "logs"),
    ]
    for directory in dirs:
        os.makedirs(directory, exist_ok=True)


def atomic_write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    fd, tmp_path = tempfile.mkstemp(prefix=".margin-", dir=os.path.dirname(path))
    try:
        with os.fdopen(fd, "w", encoding="utf-8", newline="") as fh:
            fh.write(text)
            fh.flush()
            os.fsync(fh.fileno())
        os.replace(tmp_path, path)
    finally:
        if os.path.exists(tmp_path):
            os.remove(tmp_path)


def view_text(view):
    return view.substr(sublime.Region(0, view.size()))


def syntax_name(view):
    syntax = view.settings().get("syntax") or ""
    base = os.path.basename(syntax)
    return base.split(".")[0] if base else "Plain Text"


def extension_for_view(view, cfg):
    if cfg.get("force_markdown_extension", True):
        return "md"
    mapping = cfg.get("syntax_extension_map") or {}
    ext = mapping.get(syntax_name(view), "txt")
    return ext.lstrip(".")


def scratch_id(view):
    sid = view.settings().get("margin_scratch_id")
    if not sid:
        sid = str(uuid.uuid4())
        view.settings().set("margin_scratch_id", sid)
    return sid


def is_margin_managed(view):
    return bool(view.settings().get("margin_managed"))


def set_margin_managed(view, value=True):
    view.settings().set("margin_managed", value)


def scratch_current_path(root, view, cfg):
    ext = extension_for_view(view, cfg)
    sid = scratch_id(view)
    return os.path.join(root, "scratch", "current", "{}.{}".format(sid, ext))


def history_path(root, view, cfg):
    now = datetime.now()
    ext = extension_for_view(view, cfg)
    sid = scratch_id(view)
    day_dir = os.path.join(root, "scratch", "history", now.strftime("%Y"), now.strftime("%Y-%m-%d"))
    return os.path.join(day_dir, "{}_{}.{}".format(now.strftime("%Y%m%dT%H%M%S%f"), sid, ext))


def replace_with_file_backed_tab(view, file_path):
    if not view or not view.is_valid():
        return
    window = view.window()
    if not window:
        return
    syntax = view.settings().get("syntax")

    def done():
        if not view.is_valid():
            return
        window.focus_view(view)
        view.set_scratch(True)
        window.run_command("close_file")
        opened = window.open_file(file_path)
        if syntax and opened:
            try:
                opened.assign_syntax(syntax)
            except Exception:
                pass

    sublime.set_timeout(done, 0)


def save_view(view):
    if not view or not view.is_valid():
        return

    def do_save():
        if view.is_valid() and view.is_dirty():
            view.run_command("save")

    sublime.set_timeout(do_save, 0)


def persist_view_if_needed(view, now_ts, force=False):
    if not view or not view.is_valid() or view.is_loading() or not is_margin_managed(view):
        return
    cfg = load_config()
    autosave_s = safe_int(cfg.get("autosave_interval_seconds", 5), 5)
    snapshot_s = safe_int(cfg.get("snapshot_interval_minutes", 10), 10) * 60
    last_save = safe_float(view.settings().get("margin_last_autosave", 0.0))
    last_snap = safe_float(view.settings().get("margin_last_snapshot", 0.0))
    dirty = view.is_dirty() or force
    if not dirty:
        return
    root = margin_root()
    ensure_layout(root)
    text = view_text(view)
    current_path = scratch_current_path(root, view, cfg)
    view_path = view.file_name() or ""
    is_file_backed_current = os.path.normcase(view_path) == os.path.normcase(current_path)
    if force or now_ts - last_save >= autosave_s:
        if is_file_backed_current:
            save_view(view)
        else:
            atomic_write(current_path, text)
        view.settings().set("margin_last_autosave", now_ts)
        if cfg.get("auto_replace_scratch_tab_with_file", True):
            if not view_path and not view.settings().get("margin_file_backed_opened"):
                view.settings().set("margin_file_backed_opened", True)
                replace_with_file_backed_tab(view, current_path)
    if force or now_ts - last_snap >= snapshot_s:
        snap_path = history_path(root, view, cfg)
        atomic_write(snap_path, text)
        view.settings().set("margin_last_snapshot", now_ts)
