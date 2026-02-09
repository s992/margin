import json
import os
import re
import tempfile
import time
from datetime import datetime

import sublime
import sublime_plugin

try:
    from .margin_security import resolve_within_root
    from .margin_services import move_to_trash, run_cli_json, run_process
    from .margin_settings import (
        LLM_TIMEOUT_SECONDS,
        load_config,
        log,
        margin_root,
        safe_int,
        settings,
    )
    from .margin_storage import (
        atomic_write,
        is_margin_managed,
        persist_view_if_needed,
        scratch_current_path,
        scratch_id,
        set_margin_managed,
        view_text,
    )
except ImportError:
    from margin_security import resolve_within_root
    from margin_services import move_to_trash, run_cli_json, run_process
    from margin_settings import (
        LLM_TIMEOUT_SECONDS,
        load_config,
        log,
        margin_root,
        safe_int,
        settings,
    )
    from margin_storage import (
        atomic_write,
        is_margin_managed,
        persist_view_if_needed,
        scratch_current_path,
        scratch_id,
        set_margin_managed,
        view_text,
    )


RUNBLOCK_CONSENT = None
LAST_LLM_ANSWER = ""


def open_at_result(window, result):
    root = margin_root()
    rel = result.get("file") or result.get("path")
    if not rel:
        sublime.error_message("Margin search result missing file path.")
        return
    try:
        line = safe_int(result.get("line", 1), 1)
        col = safe_int(result.get("col", 1), 1)
        target = resolve_within_root(root, rel)
    except ValueError as exc:
        sublime.error_message(str(exc))
        return
    window.open_file("{}:{}:{}".format(target, line, col), sublime.ENCODED_POSITION)


def insert_at_cursor(view, text):
    if not view:
        return
    sel = view.sel()
    point = sel[0].begin() if sel else view.size()
    view.run_command("margin_insert_at", {"point": point, "text": text})


def find_fenced_block_insert_point(view, point):
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


class MarginInsertAtCommand(sublime_plugin.TextCommand):
    def run(self, edit, point, text):
        self.view.insert(edit, int(point), text)


class MarginNewScratchCommand(sublime_plugin.WindowCommand):
    def run(self):
        view = self.window.new_file()
        set_margin_managed(view, True)
        scratch_id(view)
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
                log("search query={!r} root={!r}".format(query, margin_root()))
                res = run_cli_json(["search", "--query", query, "--root", margin_root()])
            except Exception as exc:
                msg = str(exc)
                log("search error: {}".format(msg))
                sublime.set_timeout(lambda m=msg: sublime.error_message(m), 0)
                return
            if not isinstance(res, list):
                log("search invalid response type: {}".format(type(res)))
                sublime.set_timeout(
                    lambda: sublime.error_message("Margin search returned invalid response."), 0
                )
                return
            log("search results: {}".format(len(res)))
            items = [
                [
                    "{}:{}".format(r.get("file"), r.get("line")),
                    "{}  {}".format(r.get("preview", ""), r.get("mtime", "")),
                ]
                for r in res
            ]

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
        open_at_result(self.window, results[idx])


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
            root = margin_root()
            ts = datetime.now().strftime("%Y%m%dT%H%M%S")
            clean_slug = re.sub(r"[^a-zA-Z0-9_-]+", "-", (slug or "").strip()).strip("-")
            name = "{}_{}.md".format(ts, clean_slug) if clean_slug else "{}.md".format(ts)
            path = os.path.join(root, "inbox", name)
            atomic_write(path, view_text(source_view))
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
        cfg = load_config()
        root = margin_root()
        file_path = view.file_name()
        is_margin_scratch = False
        if not file_path and is_margin_managed(view):
            file_path = scratch_current_path(root, view, cfg)
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
                move_to_trash(file_path)
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
        global RUNBLOCK_CONSENT
        if RUNBLOCK_CONSENT is None:
            allowed = sublime.ok_cancel_dialog(
                "Margin will run code blocks using local interpreters for this session. Continue?"
            )
            if allowed:
                RUNBLOCK_CONSENT = True
            else:
                sublime.status_message("Run Block cancelled (consent not granted)")
                return
        if RUNBLOCK_CONSENT is not True:
            sublime.status_message("Run Block unavailable (consent required)")
            return

        view = self.window.active_view()
        if not view:
            return
        cfg = load_config()
        root = margin_root()
        file_path = view.file_name()
        if not file_path and is_margin_managed(view):
            file_path = scratch_current_path(root, view, cfg)
            if not os.path.isfile(file_path):
                sublime.error_message("Scratch buffer must be persisted first.")
                return
        if not file_path or not os.path.isfile(file_path):
            sublime.error_message("Run Block works only for buffers on disk.")
            return

        sel = view.sel()
        point = sel[0].begin() if sel else 0
        if view.file_name():
            if view.is_dirty():
                view.run_command("save")
        else:
            try:
                atomic_write(file_path, view_text(view))
                view.settings().set("margin_last_autosave", time.time())
            except Exception as exc:
                sublime.error_message("Failed to persist scratch before run-block: {}".format(exc))
                return

        def worker():
            try:
                log("run-block file={!r} point={}".format(file_path, point))
                res = run_cli_json(
                    [
                        "run-block",
                        "--file",
                        file_path,
                        "--cursor",
                        str(point),
                        "--root",
                        root,
                    ]
                )
            except Exception as exc:
                msg = str(exc)
                log("run-block error: {}".format(msg))
                sublime.set_timeout(lambda m=msg: sublime.error_message(m), 0)
                return
            output = res.get("output", "")
            exit_code = int(res.get("exit_code", 1))
            insert_point = find_fenced_block_insert_point(view, point)
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
                res = run_cli_json(
                    [
                        "slack",
                        "capture",
                        "--channel",
                        self.channel,
                        "--thread",
                        self.thread,
                        "--root",
                        margin_root(),
                        "--token-env",
                        "SLACK_TOKEN",
                        "--format",
                        "markdown",
                    ]
                )
            except Exception as exc:
                msg = str(exc)
                sublime.set_timeout(lambda m=msg: sublime.error_message(m), 0)
                return
            text = res.get("text", "")
            rel = res.get("saved_path", "")
            abs_path = ""
            if rel:
                try:
                    abs_path = resolve_within_root(margin_root(), rel)
                except ValueError as exc:
                    sublime.set_timeout(lambda m=str(exc): sublime.error_message(m), 0)
                    return

            def apply_result():
                if "Insert" in mode and view:
                    insert_at_cursor(view, text)
                if mode == "Open File":
                    self.window.open_file(abs_path)
                if mode == "Copy":
                    sublime.set_clipboard(text)
                if mode == "Insert+Save":
                    if view:
                        insert_at_cursor(view, text)
                sublime.status_message("Slack captured: {}".format(rel))

            sublime.set_timeout(apply_result, 0)

        sublime.set_timeout_async(worker, 0)


class MarginSlackCaptureFromClipboardCommand(sublime_plugin.WindowCommand):
    def run(self):
        clip = sublime.get_clipboard(1024)
        match = re.search(r"https://[^\s]*slack\.com/[^\s]+", clip)
        if not match:
            sublime.error_message("Clipboard does not contain a Slack link.")
            return
        link = match.group(0)
        channel_match = re.search(r"/archives/([A-Z0-9]+)/", link)
        channel = channel_match.group(1) if channel_match else ""

        def worker():
            try:
                res = run_cli_json(
                    [
                        "slack",
                        "capture",
                        "--channel",
                        channel,
                        "--thread",
                        link,
                        "--root",
                        margin_root(),
                        "--token-env",
                        "SLACK_TOKEN",
                        "--format",
                        "markdown",
                    ]
                )
            except Exception as exc:
                msg = str(exc)
                sublime.set_timeout(lambda m=msg: sublime.error_message(m), 0)
                return

            def done():
                view = self.window.active_view()
                if view:
                    insert_at_cursor(view, res.get("text", ""))
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
        self.client_path = settings().get("margin_llm_client_path", "")
        self.client_args = settings().get("margin_llm_client_args", [])
        self.max_chars = safe_int(settings().get("margin_llm_max_context_chars", 12000), 12000)
        self.llm_timeout_s = safe_int(
            settings().get("margin_llm_timeout_seconds", LLM_TIMEOUT_SECONDS),
            LLM_TIMEOUT_SECONDS,
        )
        if not self.client_path:
            sublime.error_message("Set margin_llm_client_path to enable Ask LLM")
            return
        if not os.path.isfile(self.client_path):
            sublime.error_message("margin_llm_client_path does not point to a file")
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
            self.window.show_input_panel(
                "Related notes query", "", self._on_related_query, None, None
            )
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
            sels = [view.substr(region) for region in view.sel() if not region.empty()]
            selection_text = "\n\n".join(sels)[: self.max_chars]
            buffer_excerpt = view_text(view)[: self.max_chars]

        def worker():
            global LAST_LLM_ANSWER
            if self.mode_idx == 2 and related_query:
                try:
                    rows = run_cli_json(
                        [
                            "search",
                            "--query",
                            related_query,
                            "--limit",
                            "8",
                            "--root",
                            margin_root(),
                        ]
                    )
                except Exception:
                    rows = []
                for row in rows:
                    related.append(
                        {
                            "path": row.get("file", ""),
                            "excerpt": row.get("preview", ""),
                        }
                    )
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
                code, out_s, err_s = run_process(
                    [self.client_path] + list(self.client_args) + [temp_path],
                    timeout_s=self.llm_timeout_s,
                )
                out = out_s.strip()
                if code != 0:
                    out = err_s.strip() or "LLM client failed"
                LAST_LLM_ANSWER = out
            finally:
                if os.path.exists(temp_path):
                    os.remove(temp_path)

            def render():
                panel = self.window.create_output_panel("margin_llm")
                panel.set_read_only(False)
                panel.run_command("select_all")
                panel.run_command("right_delete")
                panel.run_command(
                    "append",
                    {"characters": LAST_LLM_ANSWER + "\n", "force": True, "scroll_to_end": False},
                )
                panel.set_read_only(True)
                self.window.run_command("show_panel", {"panel": "output.margin_llm"})

            sublime.set_timeout(render, 0)

        sublime.set_timeout_async(worker, 0)


class MarginInsertLastLlmAnswerCommand(sublime_plugin.WindowCommand):
    def run(self):
        view = self.window.active_view()
        if not view:
            return
        if not LAST_LLM_ANSWER:
            sublime.status_message("No LLM answer available")
            return
        insert_at_cursor(view, LAST_LLM_ANSWER + "\n")


class MarginEventListener(sublime_plugin.EventListener):
    def on_load_async(self, view):
        file_name = view.file_name() or ""
        root = margin_root()
        current_dir = os.path.realpath(os.path.join(root, "scratch", "current"))
        if file_name:
            try:
                resolve_within_root(current_dir, file_name)
                in_scratch_current = True
            except ValueError:
                in_scratch_current = False
        else:
            in_scratch_current = False
        if in_scratch_current:
            set_margin_managed(view, True)
            view.settings().set("margin_file_backed_opened", True)
            stem = os.path.basename(file_name).split(".")[0]
            view.settings().set("margin_scratch_id", stem)

    def on_pre_close(self, view):
        if not is_margin_managed(view):
            return
        persist_view_if_needed(view, time.time(), force=True)
