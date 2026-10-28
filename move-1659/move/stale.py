#!/usr/bin/env python3
"""Count path citations, outside ADR bodies, that name no path in the tree.

usage: stale.py   (run at a checkout root)

A citation is a backquoted or bare token that starts with one of the
folders below and holds a slash. It resolves when the path exists from
the repository root, or from the module root of the file citing it.
"""
import os, re, subprocess, sys, collections
HEADS = ["platform", "console", "tooling", "cowork", "deploy", "docs", ".github",
         "cmd", "internal", "api", "proto", "migrations", "tests", "web", "scripts"]
TOK = re.compile(r'(?<![\w./@:-])((?:' + "|".join(re.escape(h) for h in HEADS) + r')/[A-Za-z0-9_.\[\]()/-]*[A-Za-z0-9_\]/])')
MODS = ["platform/sdk", "platform", "cowork/web", "cowork/engine", "cowork", "console"]
files = subprocess.run(["git", "ls-files"], capture_output=True, text=True, check=True).stdout.split()
tree = set()
for p in files:
    parts = p.split("/")
    for i in range(1, len(parts) + 1):
        tree.add("/".join(parts[:i]))


def resolves(m, t, cds=()):
    t = t.rstrip("/")
    while t.endswith(")") and t.count(")") > t.count("("):
        t = t[:-1]
    for r in list(cds) + ([m] if m else []) + [""]:
        q = os.path.join(r, t) if r else t
        if q in tree or any(x.startswith(q) for x in tree if q.split("/")[-1][:1].isdigit()):
            return True
    return False


stale = collections.Counter()
total = 0
for f in files:
    if f.startswith("docs/architecture/adr/") or not os.path.isfile(f):
        continue
    try:
        s = open(f, encoding="utf-8").read()
    except (UnicodeDecodeError, IsADirectoryError):
        continue
    m = next((x for x in MODS if f.startswith(x + "/")), "")
    for line in s.split("\n"):
        # A command line that changes directory first names its paths
        # from there.
        cds = re.findall(r'\bcd ([\w./-]+) &&', line)
        for t in TOK.findall(line):
            total += 1
            if not resolves(m, t, cds):
                stale[(f, t)] += 1
for (f, t), n in sorted(stale.items()):
    print(f"{f}: {t}" + (f" ({n})" if n > 1 else ""))
print(f"stale: {sum(stale.values())} of {total} citations name no path", file=sys.stderr)
