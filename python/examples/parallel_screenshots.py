"""Open N tabs, navigate each to a different URL, screenshot in parallel.

Usage:
    ./netra-browser --listen 127.0.0.1:7878 --token T --auto-attach &
    python examples/parallel_screenshots.py
"""
import os
import base64

from netra_fanout import Bridge, fan_out


def screenshot_task(bridge, target_id, label):
    bridge.navigate(target_id, f"https://example.com/?case={label}", wait_until="domcontentloaded")
    res = bridge.call("browser_screenshot", {"target_id": target_id})
    out = f"/tmp/{label}.png"
    with open(out, "wb") as f:
        f.write(base64.b64decode(res["png_base64"]))
    return out


def main():
    b = Bridge("http://127.0.0.1:7878", token=os.getenv("TOKEN"))
    b.attach()
    labels = ["alpha", "beta", "gamma", "delta", "epsilon"]
    results = fan_out(b, [(l, screenshot_task) for l in labels], max_concurrency=3)
    for r in results:
        if r.ok:
            print(f"{r.label}: {r.result}")
        else:
            print(f"{r.label}: FAILED — {r.error}")


if __name__ == "__main__":
    main()
