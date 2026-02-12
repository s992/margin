from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
CASES_PATH = REPO_ROOT / "scripts/tests/install_contract_cases.json"


def _load_cases() -> list[dict[str, object]]:
    return json.loads(CASES_PATH.read_text(encoding="utf-8"))


def _selected_installers() -> list[tuple[str, list[str]]]:
    requested = os.environ.get("MARGIN_INSTALLER", "all").strip().lower()
    installers: list[tuple[str, list[str]]] = []

    if requested in ("all", "sh"):
        bash = shutil.which("bash")
        if bash:
            installers.append(("sh", [bash, str(REPO_ROOT / "scripts/install.sh")]))
        elif requested == "sh":
            pytest.fail("MARGIN_INSTALLER=sh requested but bash is not available")

    if requested in ("all", "ps1"):
        pwsh = shutil.which("pwsh")
        if pwsh:
            installers.append(("ps1", [pwsh, "-NoProfile", "-File", str(REPO_ROOT / "scripts/install.ps1")]))
        elif requested == "ps1":
            pytest.fail("MARGIN_INSTALLER=ps1 requested but pwsh is not available")

    if not installers:
        pytest.skip("No installer runtime available (bash/pwsh)", allow_module_level=True)
    return installers


def _parse_fields(output: str) -> dict[str, str]:
    fields: dict[str, str] = {}
    for line in output.splitlines():
        if not line.startswith("margin-install: "):
            continue
        payload = line[len("margin-install: ") :]
        if payload.startswith("host="):
            for token in payload.split():
                if "=" in token:
                    k, v = token.split("=", 1)
                    fields[k] = v
            continue
        if "=" in payload:
            k, v = payload.split("=", 1)
            fields[k] = v
    return fields


CASES = _load_cases()
INSTALLERS = _selected_installers()


@pytest.mark.parametrize("installer_name,installer_cmd", INSTALLERS, ids=[name for name, _ in INSTALLERS])
@pytest.mark.parametrize("case", CASES, ids=[case["name"] for case in CASES])
def test_install_contract(installer_name: str, installer_cmd: list[str], case: dict[str, object]) -> None:
    cmd = [*installer_cmd, *case["args"]]
    proc = subprocess.run(
        cmd,
        cwd=REPO_ROOT,
        check=False,
        capture_output=True,
        text=True,
    )
    output = f"{proc.stdout}\n{proc.stderr}"
    assert proc.returncode == case["expect_exit"], (
        f"{installer_name} case={case['name']} expected exit={case['expect_exit']} "
        f"got={proc.returncode}\n{output}"
    )

    expected_fields = case.get("expect", {})
    if expected_fields:
        parsed = _parse_fields(output)
        for key, expected in expected_fields.items():
            assert parsed.get(key) == expected, (
                f"{installer_name} case={case['name']} key={key} expected={expected!r} "
                f"got={parsed.get(key)!r}\n{output}"
            )

    expected_error = case.get("expect_error_contains")
    if expected_error:
        assert re.search(re.escape(str(expected_error)), output), (
            f"{installer_name} case={case['name']} missing expected error text {expected_error!r}\n{output}"
        )
