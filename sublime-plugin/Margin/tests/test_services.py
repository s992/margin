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
