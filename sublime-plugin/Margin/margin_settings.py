import json
import os

import sublime


DEFAULT_CONFIG = {
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

CLI_TIMEOUT_SECONDS = 60
LLM_TIMEOUT_SECONDS = 180


def settings():
    return sublime.load_settings("Margin.sublime-settings")


def log(message):
    print("Margin: {}".format(message))


def default_root():
    plat = sublime.platform()
    home = os.path.expanduser("~")
    if plat == "windows":
        return os.path.join(
            os.getenv("APPDATA", os.path.join(home, "AppData", "Roaming")), "Margin"
        )
    if plat == "osx":
        return os.path.join(home, "Library", "Application Support", "Margin")
    return os.path.join(home, ".local", "share", "margin")


def margin_root():
    configured = settings().get("margin_root", "")
    root = configured if configured and str(configured).strip() else default_root()
    return os.path.abspath(os.path.expanduser(str(root)))


def safe_int(value, default, minimum=1):
    try:
        parsed = int(value)
    except (TypeError, ValueError):
        return default
    if parsed < minimum:
        return minimum
    return parsed


def safe_float(value, default=0.0, minimum=0.0):
    try:
        parsed = float(value)
    except (TypeError, ValueError):
        return default
    if parsed < minimum:
        return minimum
    return parsed


def load_config():
    cfg = dict(DEFAULT_CONFIG)
    root = margin_root()
    config_path = os.path.join(root, "config.json")
    if os.path.isfile(config_path):
        try:
            with open(config_path, "r", encoding="utf-8") as fh:
                loaded = json.load(fh)
            if isinstance(loaded, dict):
                cfg.update(loaded)
            else:
                log("Ignoring invalid config.json: expected object at {}".format(config_path))
        except (OSError, ValueError) as exc:
            log("Failed to load config.json at {}: {}".format(config_path, exc))
    cfg["auto_replace_scratch_tab_with_file"] = bool(
        settings().get(
            "margin_auto_replace_scratch_tab_with_file",
            cfg.get("auto_replace_scratch_tab_with_file", True),
        )
    )
    return cfg
