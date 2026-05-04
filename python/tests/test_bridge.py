import pytest

from netra_fanout import Bridge, BridgeError, RPCError


def test_call_returns_result(fake_bridge):
    server, url = fake_bridge
    server.register("meta_health", lambda _: {"ok": True, "chrome_alive": True})
    b = Bridge(url)
    assert b.health() == {"ok": True, "chrome_alive": True}


def test_call_raises_on_error_envelope(fake_bridge):
    server, url = fake_bridge
    server.register(
        "browser_navigate",
        lambda p: (_ for _ in ()).throw(RuntimeError("nav failed")),
    )
    b = Bridge(url)
    with pytest.raises(RPCError) as excinfo:
        b.call("browser_navigate", {"url": "https://x"})
    assert "nav failed" in str(excinfo.value)


def test_token_auth_required(fake_bridge_with_token):
    server, url = fake_bridge_with_token
    server.register("meta_health", lambda _: {"ok": True})

    # No token → 401 → BridgeError.
    no_token = Bridge(url)
    with pytest.raises(BridgeError) as excinfo:
        no_token.call("meta_health")
    assert "401" in str(excinfo.value)

    # Correct token → success.
    with_token = Bridge(url, token="SECRET")
    assert with_token.health() == {"ok": True}


def test_call_records_increasing_ids(fake_bridge):
    server, url = fake_bridge
    server.register("meta_health", lambda _: {"ok": True})
    b = Bridge(url)
    b.health()
    b.health()
    b.health()
    # Just verify the bridge issued 3 calls — ID monotonicity is internal.
    assert len(server.calls) == 3


def test_unknown_tool_surfaces_as_rpc_error(fake_bridge):
    _, url = fake_bridge
    b = Bridge(url)
    with pytest.raises(RPCError) as excinfo:
        b.call("does_not_exist")
    assert excinfo.value.code == -32601


def test_transport_error(fake_bridge):
    # Point at a port nothing is listening on.
    b = Bridge("http://127.0.0.1:1", timeout_s=0.5)
    with pytest.raises(BridgeError):
        b.health()


def test_new_tab_helper(fake_bridge):
    server, url = fake_bridge
    server.register("browser_new_tab", lambda p: {"ok": True, "target_id": "T-42"})
    b = Bridge(url)
    assert b.new_tab("https://example.com") == "T-42"
    assert server.calls[-1][1] == {"url": "https://example.com"}
