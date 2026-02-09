import sys
import types
from pathlib import Path


class FakeSettings:
    def __init__(self):
        self.data = {}

    def get(self, key, default=None):
        return self.data.get(key, default)

    def set(self, key, value):
        self.data[key] = value


_FAKE_SETTINGS = FakeSettings()
_LAST_ERROR = None
_LAST_STATUS = None
_CLIPBOARD = ""


def _set_settings_data(data):
    _FAKE_SETTINGS.data = dict(data)


def _error_message(message):
    global _LAST_ERROR
    _LAST_ERROR = message


def _status_message(message):
    global _LAST_STATUS
    _LAST_STATUS = message


def _get_last_error():
    return _LAST_ERROR


def _get_last_status():
    return _LAST_STATUS


def _set_clipboard(value):
    global _CLIPBOARD
    _CLIPBOARD = value


def _get_clipboard(_limit=1024):
    return _CLIPBOARD


class _Region:
    def __init__(self, a, b):
        self.a = a
        self.b = b


class _TextCommand:
    pass


class _WindowCommand:
    pass


class _EventListener:
    pass


sublime = types.ModuleType("sublime")
sublime.load_settings = lambda _name: _FAKE_SETTINGS
sublime.platform = lambda: "linux"
sublime.windows = lambda: []
sublime.set_timeout = lambda fn, _delay=0: fn()
sublime.set_timeout_async = lambda fn, _delay=0: fn()
sublime.error_message = _error_message
sublime.status_message = _status_message
sublime.message_dialog = lambda _msg: None
sublime.ok_cancel_dialog = lambda _msg, _ok_title=None: True
sublime.Region = _Region
sublime.ENCODED_POSITION = 0
sublime.get_clipboard = _get_clipboard
sublime.set_clipboard = _set_clipboard
sublime._set_settings_data = _set_settings_data
sublime._get_last_error = _get_last_error
sublime._get_last_status = _get_last_status

sublime_plugin = types.ModuleType("sublime_plugin")
sublime_plugin.TextCommand = _TextCommand
sublime_plugin.WindowCommand = _WindowCommand
sublime_plugin.EventListener = _EventListener

sys.modules["sublime"] = sublime
sys.modules["sublime_plugin"] = sublime_plugin

PLUGIN_DIR = Path(__file__).resolve().parents[1]
if str(PLUGIN_DIR) not in sys.path:
    sys.path.insert(0, str(PLUGIN_DIR))
