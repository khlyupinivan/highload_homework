#!/usr/bin/env python3

import json
import os
import random
import statistics
import string
import sys
import threading
import time
import urllib.error
import urllib.request
from collections import Counter, defaultdict
from concurrent.futures import ThreadPoolExecutor, as_completed

BASE_URL = os.getenv("BASE_URL", "http://localhost:8080").rstrip("/")
DURATION = float(os.getenv("DURATION", "60"))
WORKERS = int(os.getenv("WORKERS", "8"))
RPS_PER_WORKER = float(os.getenv("RPS_PER_WORKER", "0"))
TIMEOUT = float(os.getenv("TIMEOUT", "5"))
ERROR_RATE = float(os.getenv("ERROR_RATE", "0.05"))
BOOM_RATE = float(os.getenv("BOOM_RATE", str(ERROR_RATE / 2)))


def rand_hex(n=8):
    return "".join(random.choices(string.hexdigits.lower()[:16], k=n))


def http_json(method, path, body=None):
    """Возвращает (status, latency_ms, parsed_body). -1 — транспортная ошибка."""
    url = f"{BASE_URL}{path}"
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(
        url=url, data=data, method=method,
        headers={"Content-Type": "application/json"},
    )
    start = time.perf_counter()
    try:
        with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
            status = resp.status
            raw = resp.read()
    except urllib.error.HTTPError as e:
        status = e.code
        raw = e.read()
    except Exception:
        return -1, (time.perf_counter() - start) * 1000.0, None
    latency = (time.perf_counter() - start) * 1000.0
    try:
        parsed = json.loads(raw) if raw else None
    except Exception:
        parsed = None
    return status, latency, parsed


def scenario_full_crud():
    """Возвращает [(op, status, latency_ms), ...]."""
    results = []
    uid = f"u_{rand_hex(12)}"
    payload = {"id": uid, "name": f"user-{uid}"}

    st, lt, _ = http_json("POST", "/users", body=payload)
    results.append(("create", st, lt))
    if st not in (200, 201):
        return results

    st, lt, _ = http_json("GET", f"/users/{uid}")
    results.append(("get", st, lt))

    upd = {"id": uid, "name": payload["name"] + "-upd"}
    st, lt, _ = http_json("PUT", f"/users/{uid}", body=upd)
    results.append(("update", st, lt))

    st, lt, _ = http_json("DELETE", f"/users/{uid}")
    results.append(("delete", st, lt))

    if random.random() < ERROR_RATE:
        ghost = f"missing_{rand_hex(12)}"
        st, lt, _ = http_json("GET", f"/users/{ghost}")
        results.append(("get_missing", st, lt))

    if random.random() < BOOM_RATE:
        st, lt, _ = http_json("GET", "/debug/boom")
        results.append(("boom", st, lt))

    return results


def worker(stop_flag, stats_lock, stats):
    interval = (1.0 / RPS_PER_WORKER) if RPS_PER_WORKER > 0 else 0.0
    next_tick = time.perf_counter()
    while not stop_flag.is_set():
        results = scenario_full_crud()
        with stats_lock:
            for op, st, lt in results:
                stats["by_op"][op].append(lt)
                stats["by_status"][st] += 1
                stats["total_ops"] += 1
                ok = 200 <= st < 300
                if not ok:
                    stats["errors"] += 1
                    stats["errors_by_op"][op] += 1

        if interval > 0:
            next_tick += interval
            sleep_for = next_tick - time.perf_counter()
            if sleep_for > 0:
                time.sleep(sleep_for)
            else:
                next_tick = time.perf_counter()


def main():
    print(f"Loadtest: url={BASE_URL}, duration={DURATION}s, workers={WORKERS}, "
          f"rps/worker={RPS_PER_WORKER or 'unlimited'}, "
          f"error_rate={ERROR_RATE:.0%}, boom_rate={BOOM_RATE:.0%}")

    st, lt, _ = http_json("GET", "/healthz")
    if st != 200:
        print(f"WARNING: /healthz вернул {st} (ожидался 200)")
        return 2

    stop_flag = threading.Event()
    stats_lock = threading.Lock()
    stats = {
        "by_op": defaultdict(list),
        "by_status": Counter(),
        "errors_by_op": Counter(),
        "total_ops": 0,
        "errors": 0,
    }

    start = time.perf_counter()

    def stopper():
        time.sleep(DURATION)
        stop_flag.set()

    threading.Thread(target=stopper, daemon=True).start()

    with ThreadPoolExecutor(max_workers=WORKERS) as pool:
        futures = [pool.submit(worker, stop_flag, stats_lock, stats) for _ in range(WORKERS)]
        for f in as_completed(futures):
            try:
                f.result()
            except Exception as exc:
                print(f"worker error: {exc!r}")

    elapsed = time.perf_counter() - start

    intended = stats["errors_by_op"]["get_missing"] + stats["errors_by_op"]["boom"]

    print()
    print("=" * 68)
    print(f"Elapsed:        {elapsed:.1f}s")
    print(f"Total ops:      {stats['total_ops']}")
    print(f"Total errors:   {stats['errors']} (intended: {intended})")
    print(f"Effective RPS:  {stats['total_ops'] / elapsed if elapsed else 0:.1f}")
    print()
    print("Status codes:")
    for code in sorted(stats["by_status"], key=lambda c: (c < 0, c)):
        print(f"  {code:>4}: {stats['by_status'][code]}")
    print()
    print("Latency by operation (ms):")
    for op, vals in stats["by_op"].items():
        if not vals:
            print(f"  {op:<12}: -")
            continue
        sv = sorted(vals)
        p50 = statistics.median(sv)
        p95 = sv[min(len(sv) - 1, int(len(sv) * 0.95))]
        p99 = sv[min(len(sv) - 1, int(len(sv) * 0.99))]
        print(f"  {op:<12}: n={len(sv):<6} p50={p50:7.1f}  p95={p95:7.1f}  "
              f"p99={p99:7.1f}  max={max(sv):7.1f}   errors={stats['errors_by_op'][op]}")
    print("=" * 68)

    unexpected = stats["errors"] - intended
    return 0 if unexpected == 0 else 1


if __name__ == "__main__":
    sys.exit(main())