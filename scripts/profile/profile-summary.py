#!/usr/bin/env python3
"""Summarize a scripts/profile-run.sh output directory: avg/peak CPU, iowait,
memory, GPU util/VRAM/power, and per-sensor peak temperatures."""
import re
import sys
from pathlib import Path


def stats(vals):
    if not vals:
        return None
    return sum(vals) / len(vals), max(vals)


def summarize_vmstat(path):
    us, sy, wa, free_kb = [], [], [], []
    header = None
    for line in path.read_text().splitlines():
        parts = line.split()
        if not parts:
            continue
        if parts[0] == "r" and "us" in parts:
            header = parts
            continue
        if header and parts[0].lstrip("-").isdigit():
            try:
                row = dict(zip(header, parts))
                us.append(int(row["us"]))
                sy.append(int(row["sy"]))
                wa.append(int(row["wa"]))
                free_kb.append(int(row["free"]))
            except (KeyError, ValueError):
                continue
    if not us:
        return
    cpu = [u + s for u, s in zip(us, sy)]
    cpu_avg, cpu_max = stats(cpu)
    wa_avg, wa_max = stats(wa)
    print(f"  CPU busy (us+sy):  avg {cpu_avg:5.1f}%   peak {cpu_max}%")
    print(f"  IO wait (wa):      avg {wa_avg:5.1f}%   peak {wa_max}%")
    print(f"  Mem free trough:   {min(free_kb) / 1024 / 1024:.1f} GiB")


def summarize_gpu(path):
    util, vram, temp, power = [], [], [], []
    total = None
    for line in path.read_text().splitlines():
        f = [x.strip() for x in line.split(",")]
        # timestamp, util.gpu, util.mem, mem.used, mem.total, temp, power, sm, mem, pstate
        if len(f) < 10:
            continue
        try:
            util.append(float(f[1]))
            vram.append(float(f[3]))
            total = float(f[4])
            temp.append(float(f[5]))
            power.append(float(f[6]))
        except ValueError:
            continue
    if not util:
        return
    u_avg, u_max = stats(util)
    v_avg, v_max = stats(vram)
    t_avg, t_max = stats(temp)
    p_avg, p_max = stats(power)
    print(f"  GPU util:          avg {u_avg:5.1f}%   peak {u_max:.0f}%")
    print(f"  GPU VRAM:          avg {v_avg / 1024:5.1f} GiB peak {v_max / 1024:.1f} / {total / 1024:.1f} GiB")
    print(f"  GPU temp:          avg {t_avg:5.1f}C   peak {t_max:.0f}C")
    print(f"  GPU power:         avg {p_avg:5.1f}W   peak {p_max:.0f}W")


def summarize_iostat(path):
    # iostat -xz blocks: a "Device ..." header line names the columns; data
    # rows follow until the next blank line. Track avg/peak %util and peak
    # write await per device.
    cols = None
    per_dev = {}  # dev -> {"util": [], "w_await": [], "wkbs": []}
    for line in path.read_text().splitlines():
        parts = line.split()
        if not parts:
            continue
        if parts[0] == "Device":
            cols = {name: i for i, name in enumerate(parts)}
            continue
        if cols and parts[0] not in ("avg-cpu:", "Linux") and len(parts) == len(cols):
            try:
                d = per_dev.setdefault(parts[0], {"util": [], "w_await": [], "wkbs": []})
                d["util"].append(float(parts[cols["%util"]]))
                d["w_await"].append(float(parts[cols["w_await"]]))
                d["wkbs"].append(float(parts[cols["wkB/s"]]))
            except (KeyError, ValueError):
                continue
    if not per_dev:
        return
    # Report the busiest device by peak %util.
    dev, d = max(per_dev.items(), key=lambda kv: max(kv[1]["util"], default=0))
    u_avg, u_max = stats(d["util"])
    print(f"  Disk {dev + ':':<13} util avg {u_avg:.0f}% peak {u_max:.0f}%   "
          f"w_await peak {max(d['w_await']):.1f}ms   write peak {max(d['wkbs']) / 1024:.0f} MiB/s")


def summarize_mpstat(path):
    # mpstat -P ALL: per-sample rows for "all" plus one per core. Busy =
    # 100 - %idle. Report system average, peak single-core busy, and peak
    # iowait from the "all" rows.
    cols = None
    all_busy, all_wait, core_busy = [], [], []
    for line in path.read_text().splitlines():
        parts = line.split()
        if len(parts) < 4 or parts[0] in ("Linux", "Average:"):
            continue
        if "%idle" in parts:
            cols = {name: i - len(parts) for i, name in enumerate(parts)}  # from end
            continue
        if cols is None:
            continue
        try:
            idle = float(parts[cols["%idle"]])
            busy = 100.0 - idle
        except (ValueError, IndexError):
            continue
        cpu = parts[cols["CPU"]]
        if cpu == "all":
            all_busy.append(busy)
            try:
                all_wait.append(float(parts[cols["%iowait"]]))
            except (ValueError, KeyError):
                pass
        else:
            core_busy.append(busy)
    if not all_busy:
        return
    a_avg, a_max = stats(all_busy)
    line = f"  CPU (mpstat):      avg {a_avg:5.1f}%   peak {a_max:.0f}%"
    if core_busy:
        line += f"   busiest single core peak {max(core_busy):.0f}%"
    print(line)
    if all_wait and max(all_wait) > 0:
        print(f"  iowait (mpstat):   avg {sum(all_wait) / len(all_wait):5.1f}%   peak {max(all_wait):.1f}%")


def summarize_pidstat(path):
    # pidstat -urd interleaves three section types, each introduced by its
    # own header. Field positions are taken from the header, indexed from the
    # end so the timestamp column width doesn't matter. Command is always last.
    section = None
    idx = {}
    cpu = {}   # command -> [%CPU samples]
    wr = {}    # command -> [kB_wr/s samples]
    rss = {}   # command -> max RSS kB
    for line in path.read_text().splitlines():
        parts = line.split()
        if len(parts) < 5 or parts[0] in ("Linux", "Average:"):
            continue
        if "%CPU" in parts and "Command" in parts:
            section, idx = "cpu", {"%CPU": parts.index("%CPU") - len(parts)}
            continue
        if "RSS" in parts and "Command" in parts:
            section, idx = "mem", {"RSS": parts.index("RSS") - len(parts)}
            continue
        if "kB_wr/s" in parts and "Command" in parts:
            section, idx = "io", {"kB_wr/s": parts.index("kB_wr/s") - len(parts)}
            continue
        if section is None:
            continue
        cmd = parts[-1]
        try:
            if section == "cpu":
                cpu.setdefault(cmd, []).append(float(parts[idx["%CPU"]]))
            elif section == "mem":
                rss[cmd] = max(rss.get(cmd, 0.0), float(parts[idx["RSS"]]))
            elif section == "io":
                wr.setdefault(cmd, []).append(float(parts[idx["kB_wr/s"]]))
        except (ValueError, IndexError):
            continue
    if not cpu:
        return
    top_cpu = sorted(cpu.items(), key=lambda kv: -max(kv[1]))[:3]
    frags = [f"{c} peak {max(v):.0f}% (rss {rss.get(c, 0) / 1024 / 1024:.1f}G)" for c, v in top_cpu]
    print(f"  Top proc CPU:      {'   '.join(frags)}")
    if wr:
        top_wr = sorted(wr.items(), key=lambda kv: -max(kv[1]))[:3]
        frags = [f"{c} peak {max(v) / 1024:.1f} MiB/s" for c, v in top_wr if max(v) > 0]
        if frags:
            print(f"  Top proc disk wr:  {'   '.join(frags)}")


def summarize_psi(path):
    peaks = {}  # (resource, kind) -> max avg10
    for line in path.read_text().splitlines():
        m = re.match(r"^(cpu|io|memory) (some|full) avg10=([0-9.]+)", line)
        if m:
            key = (m.group(1), m.group(2))
            val = float(m.group(3))
            peaks[key] = max(peaks.get(key, val), val)
    if not peaks:
        return
    parts = []
    for res in ("cpu", "io", "memory"):
        some = peaks.get((res, "some"))
        full = peaks.get((res, "full"))
        if some is not None:
            frag = f"{res} some {some:.0f}%"
            if full:
                frag += f" / full {full:.0f}%"
            parts.append(frag)
    print(f"  PSI peak (avg10):  {'   '.join(parts)}")


def summarize_vllm(path):
    gauges = {"num_requests_running": [], "num_requests_waiting": [],
              "kv_cache_usage_perc": []}
    counters = {}  # name -> (first, last)
    times = []
    for line in path.read_text().splitlines():
        if line.startswith("=== "):
            times.append(int(line[4:]))
            continue
        m = re.match(r"^vllm:([a-z_]+)(?:\{([^}]*)\})? ([0-9.e+-]+)$", line)
        if not m:
            continue
        name, labels, val = m.group(1), m.group(2) or "", float(m.group(3))
        if name == "gpu_cache_usage_perc":  # older vLLM name for the same gauge
            name = "kv_cache_usage_perc"
        if name in gauges:
            gauges[name].append(val)
        elif name in ("prompt_tokens_total", "generation_tokens_total",
                      "prefix_cache_queries_total", "prefix_cache_hits_total"):
            first, _ = counters.get(name, (val, val))
            counters[name] = (first, val)
    if not times:
        return
    dur = max(times[-1] - times[0], 1)
    for name, label in [("num_requests_running", "vLLM running reqs"),
                        ("num_requests_waiting", "vLLM waiting reqs")]:
        if gauges[name]:
            a, mx = stats(gauges[name])
            print(f"  {label + ':':<19}avg {a:5.1f}    peak {mx:.0f}")
    if gauges["kv_cache_usage_perc"]:
        a, mx = stats(gauges["kv_cache_usage_perc"])
        print(f"  vLLM KV cache:     avg {a * 100:5.1f}%   peak {mx * 100:.1f}%")
    for name, label in [("prompt_tokens_total", "prompt tok/s"),
                        ("generation_tokens_total", "gen tok/s")]:
        if name in counters:
            first, last = counters[name]
            print(f"  vLLM {label + ':':<14}{(last - first) / dur:8.0f}  ({last - first:.0f} over {dur}s)")
    hits = counters.get("prefix_cache_hits_total")
    queries = counters.get("prefix_cache_queries_total")
    if hits and queries:
        dh, dq = hits[1] - hits[0], queries[1] - queries[0]
        if dq > 0:
            print(f"  vLLM prefix cache: {100 * dh / dq:.1f}% hit rate this run ({dh:.0f}/{dq:.0f} block tokens)")


def summarize_sensors(path):
    peaks = {}
    for line in path.read_text().splitlines():
        m = re.match(r"^([^:]{1,40}):\s+\+?(-?[0-9.]+)\s*°C", line)
        if m:
            label, val = m.group(1).strip(), float(m.group(2))
            peaks[label] = max(peaks.get(label, val), val)
    if not peaks:
        return
    print("  Sensor peak temps:")
    for label, val in sorted(peaks.items(), key=lambda kv: -kv[1])[:8]:
        print(f"    {label:<24} {val:.0f}C")


def main():
    out = Path(sys.argv[1])
    print(f"--- profile summary: {out.name} ---")
    for name, fn in [
        ("vmstat.log", summarize_vmstat),
        ("mpstat.log", summarize_mpstat),
        ("psi.log", summarize_psi),
        ("iostat.log", summarize_iostat),
        ("pidstat.log", summarize_pidstat),
        ("gpu.csv", summarize_gpu),
        ("vllm.prom", summarize_vllm),
        ("sensors.log", summarize_sensors),
    ]:
        p = out / name
        if p.exists() and p.stat().st_size:
            fn(p)


if __name__ == "__main__":
    main()
