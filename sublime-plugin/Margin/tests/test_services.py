import subprocess

import margin_services


class FakeProc:
    def __init__(self):
        self.calls = 0
        self.killed = False
        self.returncode = 0

    def communicate(self, _in_bytes=None, timeout=None):
        self.calls += 1
        if self.calls == 1:
            raise subprocess.TimeoutExpired(cmd=["fake"], timeout=timeout)
        return b"out", b"err"

    def kill(self):
        self.killed = True


def test_run_process_timeout(monkeypatch):
    proc = FakeProc()
    monkeypatch.setattr(margin_services.subprocess, "Popen", lambda *args, **kwargs: proc)

    code, out, err = margin_services.run_process(["fake"], timeout_s=1)

    assert code == 124
    assert out == "out"
    assert "timed out" in err.lower()
    assert proc.killed is True


def test_move_to_trash_uses_send2trash_when_available(monkeypatch, tmp_path):
    target = tmp_path / "note.md"
    target.write_text("x", encoding="utf-8")
    called = {}

    def fake_send2trash(path):
        called["path"] = path

    monkeypatch.setattr(margin_services, "_send2trash", fake_send2trash)
    margin_services.move_to_trash(str(target))

    assert called["path"] == str(target)


def test_move_to_trash_falls_back_when_send2trash_fails(monkeypatch, tmp_path):
    target = tmp_path / "note.md"
    target.write_text("x", encoding="utf-8")

    monkeypatch.setattr(margin_services, "_send2trash", lambda _path: (_ for _ in ()).throw(RuntimeError("fail")))
    monkeypatch.setattr(margin_services.sublime, "platform", lambda: "linux")
    monkeypatch.setattr(margin_services.shutil, "which", lambda name: "/usr/bin/gio" if name == "gio" else None)

    calls = []

    def fake_run_process(cmd, stdin_text=None, timeout_s=None):
        calls.append((cmd, stdin_text, timeout_s))
        return 0, "", ""

    monkeypatch.setattr(margin_services, "run_process", fake_run_process)

    margin_services.move_to_trash(str(target))

    assert calls
    assert calls[0][0] == ["/usr/bin/gio", "trash", str(target)]
