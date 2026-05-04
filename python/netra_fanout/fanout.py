"""Fan-out orchestrator.

Opens N tabs, runs one callable per tab in a thread pool, joins, returns
results aligned with the input task list. Each callable is responsible for
its own bridge calls — fan-out only handles the tab lifecycle and parallelism.
"""
from __future__ import annotations

from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from typing import Any, Callable, Iterable, Optional

from .bridge import Bridge, RPCError, BridgeError


# Task signature: (bridge, target_id, label) -> result
TaskFn = Callable[["Bridge", str, str], Any]


@dataclass
class FanOutResult:
    """One row in the fan-out result list, aligned with the input order."""

    label: str
    target_id: Optional[str]
    ok: bool
    result: Any = None
    error: Optional[str] = None


def fan_out(
    bridge: Bridge,
    tasks: Iterable[tuple[str, TaskFn]],
    *,
    max_concurrency: int = 4,
    open_url: str = "about:blank",
    close_tabs: bool = True,
) -> list[FanOutResult]:
    """Run each (label, fn) against its own freshly-opened tab in parallel.

    Args:
        bridge: a connected Bridge. Caller is responsible for `meta_attach`.
        tasks: pairs of (label, fn). `fn(bridge, target_id, label) -> Any`.
        max_concurrency: thread pool size.
        open_url: starting URL for each tab. Override per-task by navigating
            inside the callable.
        close_tabs: close each tab after its task finishes (success or failure).
            Set False to leave tabs open for inspection.

    Returns:
        A list of FanOutResult in input order. Each row indicates ok/error
        independently — one task failing does not abort the others.
    """
    task_list = list(tasks)
    if not task_list:
        return []

    # Open all tabs up front. Sequential because Target.createTarget against
    # one Chrome doesn't parallelize meaningfully, and ordering keeps labels
    # aligned with target_ids in case anything goes sideways.
    tab_ids: list[Optional[str]] = []
    for label, _ in task_list:
        try:
            tid = bridge.new_tab(open_url)
            tab_ids.append(tid)
        except (RPCError, BridgeError) as e:
            tab_ids.append(None)
            # Open failure for this label is recorded in the result row below;
            # we still try to open the rest.
            _ = e

    results: list[Optional[FanOutResult]] = [None] * len(task_list)

    def _run_one(i: int):
        label, fn = task_list[i]
        tid = tab_ids[i]
        if tid is None:
            results[i] = FanOutResult(label=label, target_id=None, ok=False, error="failed to open tab")
            return
        try:
            out = fn(bridge, tid, label)
            results[i] = FanOutResult(label=label, target_id=tid, ok=True, result=out)
        except (RPCError, BridgeError) as e:
            results[i] = FanOutResult(label=label, target_id=tid, ok=False, error=str(e))
        except Exception as e:  # noqa: BLE001 — surface user-code errors
            results[i] = FanOutResult(label=label, target_id=tid, ok=False, error=f"{type(e).__name__}: {e}")
        finally:
            if close_tabs and tid is not None:
                try:
                    bridge.close_tab(tid)
                except (RPCError, BridgeError):
                    pass  # best effort

    with ThreadPoolExecutor(max_workers=max(1, max_concurrency)) as pool:
        for _ in pool.map(_run_one, range(len(task_list))):
            pass

    return [r for r in results if r is not None]
