import sublime

import margin_commands


class WindowStub:
    def __init__(self):
        self.opened = []

    def open_file(self, target, _flags=None):
        self.opened.append(target)


class ViewSettingsStub:
    def __init__(self):
        self.data = {}

    def get(self, key, default=None):
        return self.data.get(key, default)

    def set(self, key, value):
        self.data[key] = value


class ViewStub:
    def __init__(self, file_name):
        self._file_name = file_name
        self._settings = ViewSettingsStub()

    def file_name(self):
        return self._file_name

    def settings(self):
        return self._settings


def test_open_at_result_rejects_out_of_root(tmp_path):
    sublime._set_settings_data({"margin_root": str(tmp_path)})
    window = WindowStub()

    margin_commands.open_at_result(window, {"file": "../escape.md", "line": 1, "col": 1})

    assert window.opened == []
    assert "invalid path" in (sublime._get_last_error() or "").lower()


def test_event_listener_marks_margin_scratch(tmp_path):
    root = tmp_path
    scratch_dir = root / "scratch" / "current"
    scratch_dir.mkdir(parents=True)
    file_path = scratch_dir / "abc123.md"
    file_path.write_text("x", encoding="utf-8")
    sublime._set_settings_data({"margin_root": str(root)})

    listener = margin_commands.MarginEventListener()
    view = ViewStub(str(file_path))

    listener.on_load_async(view)

    assert view.settings().get("margin_managed") is True
    assert view.settings().get("margin_file_backed_opened") is True
    assert view.settings().get("margin_scratch_id") == "abc123"
