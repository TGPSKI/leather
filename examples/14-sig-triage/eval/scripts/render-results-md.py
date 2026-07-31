#!/usr/bin/env python3
"""Render the shareable results pages from the tools of record.

Writes results/MATRIX.md (the arm-by-arm leaderboard, via table.py) and
results/VERDICTS.md (every declared paired comparison, via paired-verdicts.py)
as fenced snapshots, so a reader following a link sees the numbers without
cloning and running anything. No scoring logic lives here — both pages are
verbatim captures of the scripts that ARE the scoring surface, stamped with
the commit they were generated at.

Regenerate after any archive change:

    python3 eval/scripts/render-results-md.py
"""
import os
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
EX = os.path.normpath(os.path.join(HERE, "..", ".."))
RESULTS = os.path.join(EX, "eval", "results")


def run(cmd, env=None):
    e = dict(os.environ)
    if env:
        e.update(env)
    r = subprocess.run(cmd, cwd=EX, env=e, capture_output=True, text=True, timeout=600)
    if r.returncode != 0:
        sys.stderr.write(r.stderr)
        raise SystemExit(f"render-results-md: {' '.join(cmd)} exited {r.returncode}")
    return r.stdout.rstrip("\n")


def commit():
    return run(["git", "rev-parse", "--short", "HEAD"])


def page(title, intro, body, regen):
    return (
        f"# {title}\n\n"
        f"> Generated snapshot — do not hand-edit. Produced by\n"
        f"> `{regen}` at commit `{commit()}`;\n"
        f"> regenerate with `python3 eval/scripts/render-results-md.py`.\n\n"
        f"{intro}\n\n"
        "```text\n"
        f"{body}\n"
        "```\n"
    )


def main():
    matrix = run(["python3", "eval/scripts/table.py"], env={"NOCOLOR": "1"})
    with open(os.path.join(RESULTS, "MATRIX.md"), "w") as f:
        f.write(page(
            "Results matrix — every archived cell",
            "Accuracy per archived cell (accept-set, abstention-aware — the\n"
            "`sigeval` scorer of record), with the variable each arm isolates.\n"
            "Cells are read against their declared comparison arm, never against\n"
            "the leaderboard: see [VERDICTS.md](VERDICTS.md) for the paired\n"
            "inference and [README.md](README.md) for how to read any number\n"
            "here (means with spread, the ±6-row null band, the failing gate).\n\n"
            "**The two rigs are separate machines with different serving\n"
            "stacks** (context window, tool-call parser, prefix caching,\n"
            "concurrent sequences). A `35b-*` row next to a `4b-*` row differs\n"
            "by model *and* by serving profile, so the gap between them is not\n"
            "a scale coefficient. Every registered contrast is within-rig on\n"
            "4B. Durations are comparable within a rig only — the 4B serves one\n"
            "sequence at a time, the 35B eight. See\n"
            "[eval/README.md](../README.md#what-is-serving-that-endpoint).",
            matrix,
            "eval/scripts/table.py",
        ))

    verdicts = run(["python3", "eval/scripts/paired-verdicts.py"])
    with open(os.path.join(RESULTS, "VERDICTS.md"), "w") as f:
        f.write(page(
            "Paired verdicts — every declared comparison",
            "McNemar's exact test on the discordant issues for each declared\n"
            "arm pair, from archives and manifests (never runner logs).\n"
            "RESOLVED means p < 0.05 on the paired flips; anything inside the\n"
            "±6-row null band is reported unresolved — *the experiment could\n"
            "not tell*, not \"no change\". Confounds are flagged from manifest\n"
            "diffs, not narrated away.",
            verdicts,
            "eval/scripts/paired-verdicts.py",
        ))

    # The registered battery is the headline result and had no rendered page
    # at all — a reader following a link saw only the exploratory atlas and
    # none of the replication that makes it a claim rather than a draw.
    confirm = run(["python3", "eval/scripts/confirmatory-verdicts.py"])
    with open(os.path.join(RESULTS, "CONFIRMATORY.md"), "w") as f:
        f.write(page(
            "Confirmatory verdicts — the six registered contrasts",
            "The pre-registered battery, executed exactly as registered at main\n"
            "commit `96cc418` **before any confirmatory cell ran** "
            "([registration](../ablation/preregistration.md)).\n"
            "Eleven arms × 3 replications on the 4B, wave-ordered; McNemar exact\n"
            "on the discordant issues per contrast, per wave and pooled;\n"
            "Holm–Bonferroni across the six primaries at α=0.05.\n\n"
            "**Five of six resolve.** The primary estimator is an issue-clustered\n"
            "permutation test (Amendment 2): repeats of the same 250 issues are not\n"
            "independent trials, so the pooled McNemar shown below for continuity\n"
            "overstates significance and is *not* the verdict.\n\n"
            "Three effects moved under scrutiny, all against the author's interest:\n"
            "depth −9.2 → −5.2 and retrieval payload +6.4 → +3.0 under replication,\n"
            "and retrieval payload from RESOLVED to **UNRESOLVED** under the\n"
            "clustered estimator. Those corrections are the point of the exercise\n"
            "and are left visible rather than restated.",
            confirm,
            "eval/scripts/confirmatory-verdicts.py",
        ))

    print(f"wrote MATRIX.md, VERDICTS.md and CONFIRMATORY.md at {commit()}")


if __name__ == "__main__":
    main()
