#!/usr/bin/env python3
"""Classify each citation of a moved folder: module-relative, root (moved), or neither."""
import os, re, subprocess, sys, collections
HEADS = ["cmd", "internal", "api", "proto", "migrations", "tests", "web", "scripts"]
TOK = re.compile(r'(?<![\w./@-])(?:\./)?((?:' + "|".join(HEADS) + r')/[A-Za-z0-9_.\[\]()/-]*[A-Za-z0-9_\])/])')
base = set()
for p in subprocess.run(["git","ls-tree","-r","--name-only","origin/dev"],capture_output=True,text=True,check=True).stdout.split():
    parts = p.split("/")
    for i in range(1, len(parts)+1): base.add("/".join(parts[:i]))
MODS = ["platform/sdk", "platform", "cowork/web", "cowork/engine", "cowork", "console"]
def modroot(f):
    for m in MODS:
        if f.startswith(m + "/"): return m
    return ""
files = subprocess.run(["git","ls-files"],capture_output=True,text=True,check=True).stdout.split()
out = collections.defaultdict(collections.Counter)
for f in files:
    if f.startswith("docs/architecture/adr/") or not os.path.isfile(f): continue
    try: s = open(f, encoding="utf-8").read()
    except Exception: continue
    m = modroot(f)
    for t in TOK.findall(s):
        t2 = t.rstrip("/")
        if m and os.path.exists(os.path.join(m, t2)): c = "module"
        elif t2 in base: c = "root"
        elif os.path.exists(t2): c = "exists-now"
        else: c = "none"
        out[c][(f, t)] += 1
want = sys.argv[1:] or ["root"]
for c in out: print(c, sum(out[c].values()), file=sys.stderr)
for c in want:
    for (f, t), n in sorted(out[c].items()): print(c, f, t, n)
