import json
import os
import re
import shutil
import subprocess
import tempfile
import time
import uuid
from datetime import datetime

import sublime
import sublime_plugin


_DEFAULT_CONFIG = {
    "autosave_interval_seconds": 5,
    "snapshot_interval_minutes": 10,
    "search_paths": ["scratch", "inbox", "slack"],
    "remind_enabled": False,
    "slack_enabled": False,
    "mcp_enabled": False,
    "mcp_readonly": True,
    "force_markdown_extension": True,
    "auto_replace_scratch_tab_with_file": True,
    "syntax_extension_map": {
        "Plain Text": "md",
        "Markdown": "md",
        "Python": "py",
        "JSON": "json",
        "Shell": "sh",
    },
    "runblock": {"python_bin": "python", "shell": "bash"},
}

_RUNBLOCK_CONSENT = None
_LAST_LLM_ANSWER = ""
_TICK_RUNNING = False


def plugin_loaded():
    _ensure_tick()


def plugin_unloaded():
    global _TICK_RUNNING
    _TICK_RUNNING = False


def _settings():
    return sublime.load_settings("Margin.sublime-settings")


def _default_root():
    plat = sublime.platform()
    home = os.path.expanduser("~")
    if plat == "windows":
        return os.path.join(os.getenv("APPDATA", os.path.join(home, "AppData", "Roaming")), "Margin")
    if plat == "osx":
        return os.path.join(home, "Library", "Application Support", "Margin")
    return os.path.join(home, ".local", "share", "margin")


def _margin_root():
    configured = _settings().get("margin_root", "")
    if configured and str(configured).strip():
        return configured
    return _default_root()


def _load_config():
    cfg = dict(_DEFAULT_CONFIG)
    root = _margin_root()
    config_path = os.path.join(root, "config.json")
    if os.path.isfile(config_path):
        try:
            with open(config_path, "r", encoding="utf-8") as fh:
                loaded = json.load(fh)
            cfg.update(loaded)
        except Exception:
            pass
    cfg["auto_replace_scratch_tab_with_file"] = bool(
        _settings().get("margin_auto_replace_scratch_tab_with_file", cfg.get("auto_replace_scratch_tab_with_file", True))
    )
    return cfg


def _ensure_layout(root):
    dirs = [
        os.path.join(root, "scratch", "current"),
        os.path.join(root, "scratch", "history"),
        os.path.join(root, "inbox"),
        os.path.join(root, "slack"),
        os.path.join(root, "index"),
        os.path.join(root, "bin"),
        os.path.join(root, "logs"),
    ]
    for d in dirs:
        os.makedirs(d, exist_ok=True)


def _atomic_write(path, text):
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


def _view_text(view):
    return view.substr(sublime.Region(0, view.size()))


def _syntax_name(view):
    syntax = view.settings().get("syntax") or ""
    base = os.path.basename(syntax)
    return base.split(".")[0] if base else "Plain Text"


def _extension_for_view(view, cfg):
    if cfg.get("force_markdown_extension", True):
        return "md"
    mapping = cfg.get("syntax_extension_map") or {}
    ext = mapping.get(_syntax_name(view), "txt")
    return ext.lstrip(".")


def _scratch_id(view):
    sid = view.settings().get("margin_scratch_id")
    if not sid:
        sid = str(uuid.uuid4())
        view.settings().set("margin_scratch_id", sid)
    return sid


def _is_margin_managed(view):
    return bool(view.settings().get("margin_managed"))


def _set_margin_managed(view, value=True):
    view.settings().set("margin_managed", value)


def _scratch_current_path(root, view, cfg):
    ext = _extension_for_view(view, cfg)
    sid = _scratch_id(view)
    return os.path.join(root, "scratch", "current", "{}.{}".format(sid, ext))


def _history_path(root, view, cfg):
    now = datetime.now()
    ext = _extension_for_view(view, cfg)
    sid = _scratch_id(view)
    day_dir = os.path.join(root, "scratch", "history", now.strftime("%Y"), now.strftime("%Y-%m-%d"))
    return os.path.join(day_dir, "{}_{}.{}".format(now.strftime("%Y%m%dT%H%M%S"), sid, ext))


def _persist_view_if_needed(view, now_ts, force=False):
    if not view or not view.is_valid() or view.is_loading() or not _is_margin_managed(view):
        return
    cfg = _load_config()
    autosave_s = int(cfg.get("autosave_interval_seconds", 5))
    snapshot_s = int(cfg.get("snapshot_interval_minutes", 10)) * 60
    last_save = float(view.settings().get("margin_last_autosave", 0.0))
    last_snap = float(view.settings().get("margin_last_snapshot", 0.0))
    dirty = view.is_dirty() or force
    if not dirty:
        return
    root = _margin_root()
    _ensure_layout(root)
    text = _view_text(view)
    current_path = _scratch_current_path(root, view, cfg)
    view_path = view.file_name() or ""
    is_file_backed_current = os.path.normcase(view_path) == os.path.normcase(current_path)
    if force or now_ts - last_save >= autosave_s:
        if is_file_backed_current:
            _save_view(view)
        else:
            _atomic_write(current_path, text)
        view.settings().set("margin_last_autosave", now_ts)
        if cfg.get("auto_replace_scratch_tab_with_file", True):
            if not view_path and not view.settings().get("margin_file_backed_opened"):
                view.settings().set("margin_file_backed_opened", True)
                _replace_with_file_backed_tab(view, current_path)
    if force or now_ts - last_snap >= snapshot_s:
        snap_path = _history_path(root, view, cfg)
        _atomic_write(snap_path, text)
        view.settings().set("margin_last_snapshot", now_ts)


def _tick():
    global _TICK_RUNNING
    if not _TICK_RUNNING:
        return
    now_ts = time.time()
    for w in sublime.windows():
        for view in w.views():
            _persist_view_if_needed(view, now_ts, force=False)
    sublime.set_timeout_async(_tick, 1000)


def _ensure_tick():
    global _TICK_RUNNING
    if _TICK_RUNNING:
        return
    _TICK_RUNNING = True
    sublime.set_timeout_async(_tick, 1000)


def _cli_path():
    settings = _settings()
    explicit = settings.get("margin_cli_path")
    if explicit and os.path.isfile(explicit):
        return explicit
    root = _margin_root()
    candidate = os.path.join(root, "bin", "margin.exe" if sublime.platform() == "windows" else "margin")
    if os.path.isfile(candidate):
        return candidate
    return shutil.which("margin")


def _run_cli_json(args):
    cli = _cli_path()
    if not cli:
        raise RuntimeError("Margin CLI not found (set margin_cli_path or install margin in PATH)")
    code, out, err = _run_process([cli] + args)
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


def _run_process(cmd, stdin_text=None):
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
    proc = subprocess.Popen(
        cmd,
        **popen_kwargs
    )
    in_bytes = None
    if stdin_text is not None:
        in_bytes = stdin_text.encode("utf-8")
    out_b, err_b = proc.communicate(in_bytes)
    out = out_b.decode("utf-8", "replace") if out_b else ""
    err = err_b.decode("utf-8", "replace") if err_b else ""
    return proc.returncode, out, err


def _open_at_result(window, result):
    root = _margin_root()
    rel = result.get("file") or result.get("path")
    line = int(result.get("line", 1))
    col = int(result.get("col", 1))
    target = os.path.join(root, rel)
    window.open_file("{}:{}:{}".format(target, line, col), sublime.ENCODED_POSITION)


def _insert_at_cursor(view, text):
    if not view:
        return
    sel = view.sel()
    point = sel[0].begin() if sel else view.size()
    view.run_command("margin_insert_at", {"point": point, "text": text})


def _find_fenced_block_insert_point(view, point):
    if not view or not view.is_valid():
        return None
    current_row, _ = view.rowcol(point)
    max_row, _ = view.rowcol(view.size())
    open_re = re.compile(r"^[ \t]{0,3}```[A-Za-z0-9_+-]*[ \t]*$")
    close_re = re.compile(r"^[ \t]{0,3}```[ \t]*$")

    open_row = None
    row = current_row
    while row >= 0:
        line_pt = view.text_point(row, 0)
        line_text = view.substr(view.line(line_pt)).rstrip("\r")
        if open_re.match(line_text):
            open_row = row
            break
        row -= 1
    if open_row is None:
        return None

    close_row = None
    row = open_row + 1
    while row <= max_row:
        line_pt = view.text_point(row, 0)
        line_text = view.substr(view.line(line_pt)).rstrip("\r")
        if close_re.match(line_text):
            close_row = row
            break
        row += 1
    if close_row is None or current_row > close_row:
        return None

    close_line_region = view.full_line(view.text_point(close_row, 0))
    return close_line_region.end()


def _replace_with_file_backed_tab(view, file_path):
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


def _save_view(view):
    if not view or not view.is_valid():
        return

    def do_save():
        if view.is_valid() and view.is_dirty():
            view.run_command("save")

    sublime.set_timeout(do_save, 0)


def _move_to_trash(path):
    if not path or not os.path.exists(path):
        raise RuntimeError("File does not exist: {}".format(path))
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
        code, _, err = _run_process(["powershell", "-NoProfile", "-Command", script])
        if code != 0:
            raise RuntimeError((err or "").strip() or "Failed to move file to Recycle Bin")
        return
    if plat == "osx":
        code, _, err = _run_process(["osascript", "-e", 'tell application "Finder" to delete POSIX file "{}"'.format(path)])
        if code != 0:
            raise RuntimeError((err or "").strip() or "Failed to move file to Trash")
        return
    gio = shutil.which("gio")
    if gio:
        code, _, err = _run_process([gio, "trash", path])
        if code == 0:
            return
        raise RuntimeError((err or "").strip() or "Failed to move file to trash")
    trash_put = shutil.which("trash-put")
    if trash_put:
        code, _, err = _run_process([trash_put, path])
        if code == 0:
            return
        raise RuntimeError((err or "").strip() or "Failed to move file to trash")
    raise RuntimeError("No trash command found (install gio or trash-cli)")


class MarginInsertAtCommand(sublime_plugin.TextCommand):
    def run(self, edit, point, text):
        self.view.insert(edit, int(point), text)


class MarginNewScratchCommand(sublime_plugin.WindowCommand):
    def run(self):
        view = self.window.new_file()
        _set_margin_managed(view, True)
        _scratch_id(view)
        view.assign_syntax("Packages/Text/Plain text.tmLanguage")
        view.set_name("Margin Scratch")


class MarginSearchCommand(sublime_plugin.WindowCommand):
    def run(self):
        self.window.show_input_panel("Margin search", "", self._on_query, None, None)

    def _on_query(self, query):
        if not query.strip():
            return

        def worker():
            try:
                print("Margin search: query={!r} root={!r}".format(query, _margin_root()))
                res = _run_cli_json(["search", "--query", query, "--root", _margin_root()])
            except Exception as exc:
                msg = str(exc)
                print("Margin search error: {}".format(msg))
                sublime.set_timeout(lambda m=msg: sublime.error_message(m), 0)
                return
            if not isinstance(res, list):
                print("Margin search invalid response type: {}".format(type(res)))
                sublime.set_timeout(lambda: sublime.error_message("Margin search returned invalid response."), 0)
                return
            print("Margin search results: {}".format(len(res)))
            items = [["{}:{}".format(r.get("file"), r.get("line")), "{}  {}".format(r.get("preview", ""), r.get("mtime", ""))] for r in res]

            def show_panel():
                if not items:
                    sublime.message_dialog("Margin: no results.")
                    return
                self.window.show_quick_panel(items, lambda idx: self._on_pick(idx, res))

            sublime.set_timeout(show_panel, 0)

        sublime.set_timeout_async(worker, 0)

    def _on_pick(self, idx, results):
        if idx < 0:
            return
        _open_at_result(self.window, results[idx])


class MarginPromoteCommand(sublime_plugin.WindowCommand):
    def run(self):
        view = self.window.active_view()
        if not view:
            return
        self.view = view
        self.window.show_input_panel("Optional slug", "", self._on_slug, None, None)

    def _on_slug(self, slug):
        source_view = self.view
        source_window = self.window

        def worker():
            root = _margin_root()
            _ensure_layout(root)
            ts = datetime.now().strftime("%Y%m%dT%H%M%S")
            clean_slug = re.sub(r"[^a-zA-Z0-9_-]+", "-", (slug or "").strip()).strip("-")
            name = "{}_{}.md".format(ts, clean_slug) if clean_slug else "{}.md".format(ts)
            path = os.path.join(root, "inbox", name)
            _atomic_write(path, _view_text(source_view))
            rel = os.path.relpath(path, root).replace("\\", "/")

            def done():
                if source_view and source_view.is_valid():
                    source_view.set_scratch(True)
                    source_window.focus_view(source_view)
                    source_window.run_command("close_file")
                source_window.open_file(path)
                sublime.status_message("Promoted to {}".format(rel))

            sublime.set_timeout(done, 0)

        sublime.set_timeout_async(worker, 0)


class MarginDeleteCurrentNoteCommand(sublime_plugin.WindowCommand):
    def run(self):
        view = self.window.active_view()
        if not view:
            return
        cfg = _load_config()
        root = _margin_root()
        file_path = view.file_name()
        is_margin_scratch = False
        if not file_path and _is_margin_managed(view):
            file_path = _scratch_current_path(root, view, cfg)
            is_margin_scratch = True
        if not file_path:
            sublime.error_message("No note file is associated with this view.")
            return
        if not os.path.exists(file_path):
            sublime.error_message("File not found: {}".format(file_path))
            return
        ok = sublime.ok_cancel_dialog(
            "Move this note to Recycle Bin?\n\n{}".format(file_path),
            "Move to Recycle Bin",
        )
        if not ok:
            return
        source_window = self.window
        source_view = view

        def worker():
            try:
                _move_to_trash(file_path)
            except Exception as exc:
                msg = str(exc)
                sublime.set_timeout(lambda m=msg: sublime.error_message(m), 0)
                return

            def done():
                if source_view and source_view.is_valid():
                    if is_margin_scratch:
                        source_view.settings().set("margin_managed", False)
                    source_view.set_scratch(True)
                    source_window.focus_view(source_view)
                    source_window.run_command("close_file")
                sublime.status_message("Moved to Recycle Bin")

            sublime.set_timeout(done, 0)

        sublime.set_timeout_async(worker, 0)


class MarginRunBlockCommand(sublime_plugin.WindowCommand):
    def run(self):
        global _RUNBLOCK_CONSENT
        if _RUNBLOCK_CONSENT is None:
            allowed = sublime.ok_cancel_dialog("Margin will run code blocks using local interpreters for this session. Continue?")
            if allowed:
                _RUNBLOCK_CONSENT = True
            else:
                sublime.status_message("Run Block cancelled (consent not granted)")
                return
        if _RUNBLOCK_CONSENT is not True:
            sublime.status_message("Run Block unavailable (consent required)")
            return

        view = self.window.active_view()
        if not view:
            return
        cfg = _load_config()
        root = _margin_root()
        file_path = view.file_name()
        if not file_path and _is_margin_managed(view):
            file_path = _scratch_current_path(root, view, cfg)
            if not os.path.isfile(file_path):
                sublime.error_message("Scratch buffer must be persisted first.")
                return
        if not file_path or not os.path.isfile(file_path):
            sublime.error_message("Run Block works only for buffers on disk.")
            return

        sel = view.sel()
        point = sel[0].begin() if sel else 0
        # Ensure CLI executes against latest on-disk content.
        if view.file_name():
            if view.is_dirty():
                view.run_command("save")
        else:
            try:
                _atomic_write(file_path, _view_text(view))
                view.settings().set("margin_last_autosave", time.time())
            except Exception as exc:
                sublime.error_message("Failed to persist scratch before run-block: {}".format(exc))
                return

        def worker():
            try:
                print("Margin run-block: file={!r} point={}".format(file_path, point))
                res = _run_cli_json([
                    "run-block",
                    "--file",
                    file_path,
                    "--cursor",
                    str(point),
                    "--root",
                    root,
                ])
            except Exception as exc:
                msg = str(exc)
                print("Margin run-block error: {}".format(msg))
                sublime.set_timeout(lambda m=msg: sublime.error_message(m), 0)
                return
            output = res.get("output", "")
            exit_code = int(res.get("exit_code", 1))
            insert_point = _find_fenced_block_insert_point(view, point)
            if insert_point is None:
                insert_point = int(res.get("block_end", point))
            if output.strip() == "":
                output = "[no output]"
            text = "\n\n{}".format(output.rstrip())

            def apply_insert():
                if not view or not view.is_valid():
                    return
                target = insert_point
                if target < 0 or target > view.size():
                    target = point
                view.run_command("margin_insert_at", {"point": target, "text": text})
                sublime.status_message("Run Block complete (exit {})".format(exit_code))

            sublime.set_timeout(apply_insert, 0)

        sublime.set_timeout_async(worker, 0)


class MarginSlackCaptureCommand(sublime_plugin.WindowCommand):
    MODES = ["Insert+Save", "Insert", "Open File", "Copy"]

    def run(self):
        self.window.show_input_panel("Slack channel (id or name)", "", self._on_channel, None, None)

    def _on_channel(self, channel):
        self.channel = channel.strip()
        self.window.show_input_panel("Thread ts or Slack URL", "", self._on_thread, None, None)

    def _on_thread(self, thread):
        self.thread = thread.strip()
        self.window.show_quick_panel(self.MODES, self._on_mode, selected_index=0)

    def _on_mode(self, idx):
        if idx < 0:
            return
        mode = self.MODES[idx]
        view = self.window.active_view()

        def worker():
            try:
                res = _run_cli_json([
                    "slack",
                    "capture",
                    "--channel",
                    self.channel,
                    "--thread",
                    self.thread,
                    "--root",
                    _margin_root(),
                    "--token-env",
                    "SLACK_TOKEN",
                    "--format",
                    "markdown",
                ])
            except Exception as exc:
                msg = str(exc)
                sublime.set_timeout(lambda m=msg: sublime.error_message(m), 0)
                return
            text = res.get("text", "")
            rel = res.get("saved_path", "")
            abs_path = os.path.join(_margin_root(), rel)

            def apply_result():
                if "Insert" in mode and view:
                    _insert_at_cursor(view, text)
                if mode == "Open File":
                    self.window.open_file(abs_path)
                if mode == "Copy":
                    sublime.set_clipboard(text)
                if mode == "Insert+Save":
                    if view:
                        _insert_at_cursor(view, text)
                sublime.status_message("Slack captured: {}".format(rel))

            sublime.set_timeout(apply_result, 0)

        sublime.set_timeout_async(worker, 0)


class MarginSlackCaptureFromClipboardCommand(sublime_plugin.WindowCommand):
    def run(self):
        clip = sublime.get_clipboard(1024)
        m = re.search(r"https://[^\s]*slack\.com/[^\s]+", clip)
        if not m:
            sublime.error_message("Clipboard does not contain a Slack link.")
            return
        link = m.group(0)
        chm = re.search(r"/archives/([A-Z0-9]+)/", link)
        channel = chm.group(1) if chm else ""

        def worker():
            try:
                res = _run_cli_json([
                    "slack",
                    "capture",
                    "--channel",
                    channel,
                    "--thread",
                    link,
                    "--root",
                    _margin_root(),
                    "--token-env",
                    "SLACK_TOKEN",
                    "--format",
                    "markdown",
                ])
            except Exception as exc:
                msg = str(exc)
                sublime.set_timeout(lambda m=msg: sublime.error_message(m), 0)
                return

            def done():
                view = self.window.active_view()
                if view:
                    _insert_at_cursor(view, res.get("text", ""))
                sublime.status_message("Slack captured")

            sublime.set_timeout(done, 0)

        sublime.set_timeout_async(worker, 0)


class MarginAskLlmCommand(sublime_plugin.WindowCommand):
    MODES = [
        "Ask about selection",
        "Ask about current buffer",
        "Ask with related notes",
    ]

    def run(self):
        self.client_path = _settings().get("margin_llm_client_path", "")
        self.client_args = _settings().get("margin_llm_client_args", [])
        self.max_chars = int(_settings().get("margin_llm_max_context_chars", 12000))
        if not self.client_path:
            sublime.error_message("Set margin_llm_client_path to enable Ask LLM")
            return
        self.window.show_input_panel("Ask LLM", "", self._on_question, None, None)

    def _on_question(self, question):
        self.question = question.strip()
        if not self.question:
            return
        self.window.show_quick_panel(self.MODES, self._on_mode)

    def _on_mode(self, idx):
        if idx < 0:
            return
        self.mode_idx = idx
        if idx == 2:
            self.window.show_input_panel("Related notes query", "", self._on_related_query, None, None)
            return
        self._run_with_context(None)

    def _on_related_query(self, query):
        self._run_with_context(query)

    def _run_with_context(self, related_query):
        view = self.window.active_view()
        selection_text = ""
        buffer_excerpt = ""
        related = []
        if view:
            sels = [view.substr(r) for r in view.sel() if not r.empty()]
            selection_text = "\n\n".join(sels)[: self.max_chars]
            buffer_excerpt = _view_text(view)[: self.max_chars]

        def worker():
            global _LAST_LLM_ANSWER
            if self.mode_idx == 2 and related_query:
                try:
                    rows = _run_cli_json([
                        "search",
                        "--query",
                        related_query,
                        "--limit",
                        "8",
                        "--root",
                        _margin_root(),
                    ])
                except Exception:
                    rows = []
                for row in rows:
                    related.append({
                        "path": row.get("file", ""),
                        "excerpt": row.get("preview", ""),
                    })
            payload = {
                "question": self.question,
                "selection": selection_text if self.mode_idx == 0 else "",
                "buffer_excerpt": buffer_excerpt if self.mode_idx == 1 else "",
                "related_notes": related,
            }
            fd, temp_path = tempfile.mkstemp(prefix="margin-llm-", suffix=".json")
            try:
                with os.fdopen(fd, "w", encoding="utf-8") as fh:
                    json.dump(payload, fh)
                code, out_s, err_s = _run_process([self.client_path] + list(self.client_args) + [temp_path])
                out = out_s.strip()
                if code != 0:
                    out = err_s.strip() or "LLM client failed"
                _LAST_LLM_ANSWER = out
            finally:
                os.remove(temp_path)

            def render():
                panel = self.window.create_output_panel("margin_llm")
                panel.set_read_only(False)
                panel.run_command("select_all")
                panel.run_command("right_delete")
                panel.run_command("append", {"characters": _LAST_LLM_ANSWER + "\n", "force": True, "scroll_to_end": False})
                panel.set_read_only(True)
                self.window.run_command("show_panel", {"panel": "output.margin_llm"})

            sublime.set_timeout(render, 0)

        sublime.set_timeout_async(worker, 0)


class MarginInsertLastLlmAnswerCommand(sublime_plugin.WindowCommand):
    def run(self):
        view = self.window.active_view()
        if not view:
            return
        if not _LAST_LLM_ANSWER:
            sublime.status_message("No LLM answer available")
            return
        _insert_at_cursor(view, _LAST_LLM_ANSWER + "\n")


class MarginEventListener(sublime_plugin.EventListener):
    def on_load_async(self, view):
        file_name = view.file_name() or ""
        root = _margin_root()
        current_dir = os.path.join(root, "scratch", "current")
        if file_name.startswith(current_dir + os.sep):
            _set_margin_managed(view, True)
            view.settings().set("margin_file_backed_opened", True)
            stem = os.path.basename(file_name).split(".")[0]
            view.settings().set("margin_scratch_id", stem)

    def on_pre_close(self, view):
        if not _is_margin_managed(view):
            return
        sublime.set_timeout_async(lambda: _persist_view_if_needed(view, time.time(), force=True), 0)
