import json

import sublime

from margin_settings import load_config, safe_float, safe_int


def test_safe_int_and_safe_float_clamp_and_default():
    assert safe_int("12", 5) == 12
    assert safe_int("-1", 5, minimum=1) == 1
    assert safe_int("bad", 5) == 5

    assert safe_float("3.5", 1.0) == 3.5
    assert safe_float("-2", 1.0, minimum=0.0) == 0.0
    assert safe_float("bad", 1.0) == 1.0


def test_load_config_merges_valid_json(tmp_path):
    sublime._set_settings_data({"margin_root": str(tmp_path)})
    payload = {"autosave_interval_seconds": 11, "force_markdown_extension": False}
    (tmp_path / "config.json").write_text(json.dumps(payload), encoding="utf-8")

    cfg = load_config()

    assert cfg["autosave_interval_seconds"] == 11
    assert cfg["force_markdown_extension"] is False


def test_load_config_ignores_invalid_shapes_and_json(tmp_path):
    sublime._set_settings_data({"margin_root": str(tmp_path)})

    (tmp_path / "config.json").write_text("[]", encoding="utf-8")
    cfg_shape = load_config()
    assert cfg_shape["autosave_interval_seconds"] == 5

    (tmp_path / "config.json").write_text("{bad json", encoding="utf-8")
    cfg_bad_json = load_config()
    assert cfg_bad_json["autosave_interval_seconds"] == 5
