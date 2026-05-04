import time

from netra_fanout import Bridge, fan_out


def test_fan_out_runs_each_task_with_unique_target(fake_bridge):
    server, url = fake_bridge
    counter = {"n": 0}

    def make_tab(_):
        counter["n"] += 1
        return {"ok": True, "target_id": f"T-{counter['n']}"}

    closed = []
    server.register("browser_new_tab", make_tab)
    server.register("browser_close_tab", lambda p: (closed.append(p["target_id"]) or {"ok": True}))

    seen = []

    def task(bridge, target_id, label):
        seen.append((label, target_id))
        return f"done-{label}"

    b = Bridge(url)
    results = fan_out(b, [("a", task), ("b", task), ("c", task)], max_concurrency=3)

    assert [r.label for r in results] == ["a", "b", "c"]
    assert all(r.ok for r in results)
    assert {r.result for r in results} == {"done-a", "done-b", "done-c"}
    assert len({r.target_id for r in results}) == 3  # unique tabs
    assert sorted(closed) == sorted([r.target_id for r in results])  # all closed


def test_fan_out_isolates_failures(fake_bridge):
    server, url = fake_bridge
    counter = {"n": 0}
    server.register(
        "browser_new_tab",
        lambda p: ({"ok": True, "target_id": f"T-{(counter.update(n=counter['n']+1) or counter['n'])}"}),
    )
    server.register("browser_close_tab", lambda p: {"ok": True})

    def boom(bridge, target_id, label):
        if label == "fails":
            raise ValueError("kaboom")
        return "ok"

    b = Bridge(url)
    results = fan_out(b, [("ok1", boom), ("fails", boom), ("ok2", boom)], max_concurrency=2)
    by_label = {r.label: r for r in results}
    assert by_label["ok1"].ok and by_label["ok1"].result == "ok"
    assert by_label["ok2"].ok and by_label["ok2"].result == "ok"
    assert not by_label["fails"].ok
    assert "kaboom" in by_label["fails"].error


def test_fan_out_respects_max_concurrency(fake_bridge):
    server, url = fake_bridge
    counter = {"n": 0}
    server.register(
        "browser_new_tab",
        lambda p: ({"ok": True, "target_id": f"T-{(counter.update(n=counter['n']+1) or counter['n'])}"}),
    )
    server.register("browser_close_tab", lambda p: {"ok": True})

    inflight = {"now": 0, "peak": 0}
    lock = __import__("threading").Lock()

    def slow(bridge, target_id, label):
        with lock:
            inflight["now"] += 1
            inflight["peak"] = max(inflight["peak"], inflight["now"])
        time.sleep(0.05)
        with lock:
            inflight["now"] -= 1
        return label

    b = Bridge(url)
    fan_out(b, [(str(i), slow) for i in range(8)], max_concurrency=3)
    assert inflight["peak"] <= 3


def test_fan_out_close_false_leaves_tabs_open(fake_bridge):
    server, url = fake_bridge
    counter = {"n": 0}
    server.register(
        "browser_new_tab",
        lambda p: ({"ok": True, "target_id": f"T-{(counter.update(n=counter['n']+1) or counter['n'])}"}),
    )
    closed = []
    server.register("browser_close_tab", lambda p: (closed.append(p) or {"ok": True}))

    b = Bridge(url)
    fan_out(b, [("x", lambda *_: "ok")], close_tabs=False)
    assert closed == []


def test_fan_out_empty_returns_empty(fake_bridge):
    _, url = fake_bridge
    b = Bridge(url)
    assert fan_out(b, []) == []
