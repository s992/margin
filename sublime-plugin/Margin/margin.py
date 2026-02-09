try:
    from . import margin_commands  # noqa: F401
    from .margin_storage import ensure_tick, stop_tick
except ImportError:
    import margin_commands  # noqa: F401
    from margin_storage import ensure_tick, stop_tick


def plugin_loaded():
    ensure_tick()


def plugin_unloaded():
    stop_tick()
