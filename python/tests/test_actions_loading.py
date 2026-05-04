"""Loading-only tests for the netra-actions bundle (no real Chrome).

End-to-end correctness tests live in tests/test_actions_e2e.py and require a
real Chrome via the bridge.
"""
import os
import re

from netra_fanout import Bridge


def test_bundle_file_exists():
    here = os.path.dirname(__file__)
    path = os.path.normpath(os.path.join(here, "..", "..", "js", "netra-actions.js"))
    assert os.path.isfile(path), f"missing bundle at {path}"


def test_bundle_has_version_and_namespace():
    here = os.path.dirname(__file__)
    path = os.path.normpath(os.path.join(here, "..", "..", "js", "netra-actions.js"))
    with open(path, "r", encoding="utf-8") as f:
        body = f.read()
    assert "window.__netra" in body
    assert re.search(r'VERSION\s*=\s*"\d+\.\d+\.\d+"', body), "VERSION constant missing"
    for fn in ["extractTable", "scrollToBottom", "formAutoFill", "detectFrameworks", "openShadowRoots"]:
        assert fn in body, f"missing helper: {fn}"


def test_inject_actions_uses_browser_eval(fake_bridge):
    server, url = fake_bridge

    captured = {}

    def fake_eval(p):
        captured["expression"] = p["expression"]
        captured["target_id"] = p["target_id"]
        return {"ok": True, "result": "0.1.0"}

    server.register("browser_eval", fake_eval)

    b = Bridge(url)
    version = b.inject_actions("T-1")
    assert version == "0.1.0"
    assert captured["target_id"] == "T-1"
    assert "window.__netra" in captured["expression"]
    assert "extractTable" in captured["expression"]
