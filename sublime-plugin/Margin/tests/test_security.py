import pytest

from margin_security import resolve_within_root


def test_resolve_within_root_accepts_relative_path(tmp_path):
    root = tmp_path / "root"
    root.mkdir()
    child = root / "inbox" / "note.md"
    child.parent.mkdir()
    child.write_text("x", encoding="utf-8")

    resolved = resolve_within_root(str(root), "inbox/note.md")

    assert resolved == str(child.resolve())


def test_resolve_within_root_rejects_escape(tmp_path):
    root = tmp_path / "root"
    root.mkdir()

    with pytest.raises(ValueError):
        resolve_within_root(str(root), "../outside.txt")
