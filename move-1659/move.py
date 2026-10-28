#!/usr/bin/env python3
"""Move today's tree into ADR-0125's layout (#1659), run at a checkout root.

Every step is mechanical: git mv, then path, module-path and import
rewrites. Re-run it on a fresh base rather than rebasing its output.
"""
import os
import re
import subprocess
import sys

ROOT_TO_PLATFORM = ["go.mod", "go.sum", "cmd", "internal", "api", "proto",
                    "migrations", "tests", "buf.yaml", "buf.gen.yaml",
                    "sqlc.yaml", ".mockery.yaml"]


def sh(*args):
    subprocess.run(args, check=True)


def git_files():
    out = subprocess.run(["git", "ls-files"], check=True, capture_output=True, text=True).stdout
    return [f for f in out.splitlines() if os.path.isfile(f)]


def rewrite(path, fn):
    with open(path, encoding="utf-8") as f:
        try:
            old = f.read()
        except UnicodeDecodeError:
            return False
    new = fn(old)
    if new != old:
        with open(path, "w", encoding="utf-8") as f:
            f.write(new)
        return True
    return False


# --- 1. moves -------------------------------------------------------------
# git mv into a folder that exists moves inside it, so an ignored
# leftover (a node_modules) would nest web/ under console/.
for target in ("platform/cmd", "console", "tooling"):
    if os.path.exists(target):
        sys.exit(f"move: {target} exists; remove it (git clean -fdx) and re-run")
for name in ROOT_TO_PLATFORM:
    sh("git", "mv", name, "platform/" + name)
sh("git", "mv", "web", "console")
sh("git", "mv", "scripts", "tooling")

TRAILER = os.environ.get("MOVE_TRAILER", "")


def commit(subject, body):
    if os.environ.get("MOVE_COMMIT"):
        sh("git", "add", "-A")
        sh("git", "commit", "-q", "-m", subject + "\n\n" + body + ("\n\n" + TRAILER if TRAILER else ""))


commit("refactor: move today's tree into platform/, console/ and tooling/ (#1659)",
       "Renames only, so each file's history follows it. The paths, the\n"
       "module path and the imports are rewritten in the next commit.")

# --- 2. the module path and imports ---------------------------------------
# github.com/garamsh/garam, and every package under it, becomes
# .../garam/platform; cowork's module and platform/sdk keep theirs, and
# garam-agent-operator is another repository.
MODULE = re.compile(r'github\.com/garamsh/garam(?=$|[/"`\s])(?!/(?:platform/sdk|cowork)(?=$|[/"`\s]))')


def module_path(s):
    return MODULE.sub("github.com/garamsh/garam/platform", s)


changed = 0
for f in git_files():
    if f.startswith("platform/") and not f.startswith("platform/sdk/") and (
            f.endswith((".go", ".yaml", ".yml", ".proto", ".mod")) or f.endswith(".mockery.yaml")):
        changed += rewrite(f, module_path)
print("module path rewritten in", changed, "files")
rewrite("platform/go.mod", lambda s: s.replace(
    "replace github.com/garamsh/garam/platform/sdk => ./platform/sdk",
    "replace github.com/garamsh/garam/platform/sdk => ./sdk"))

with open("go.work", "w") as f:
    f.write("go 1.27.2\n\nuse (\n\t./cowork\n\t./platform\n\t./platform/sdk\n)\n")
print("go.work rewritten")

# --- 3. path citations ----------------------------------------------------
# A citation of a moved folder is rewritten where it named a path at the
# repository root on the base commit and does not resolve inside the
# module the citing file sits in; a module-relative one (platform's own
# `internal/...` in its Go comments) is left. ADR bodies are frozen.
HEADS = ["cmd", "internal", "api", "proto", "migrations", "tests", "web", "scripts"]
TOK = re.compile(r'(?<![\w./@-])(\./)?((?:' + "|".join(HEADS) + r')/(?:[A-Za-z0-9_.\[\]()/-]*[A-Za-z0-9_\])/])?)')
BASE = sys.argv[1] if len(sys.argv) > 1 else "origin/dev"
base = set()
for p in subprocess.run(["git", "ls-tree", "-r", "--name-only", BASE],
                        capture_output=True, text=True, check=True).stdout.split():
    parts = p.split("/")
    for i in range(1, len(parts) + 1):
        base.add("/".join(parts[:i]))
MODS = ["platform/sdk", "platform", "cowork/web", "cowork/engine", "cowork", "console"]


def modroot(f):
    for m in MODS:
        if f.startswith(m + "/"):
            return m
    return ""


def new_path(t):
    head = t.split("/", 1)[0]
    rest = t.split("/", 1)[1] if "/" in t else ""
    if head == "web":
        return "console/" + rest
    if head == "scripts":
        return "tooling/" + rest
    return "platform/" + t


def balanced(t):
    while t.endswith(")") and t.count(")") > t.count("("):
        t = t[:-1]
    return t


def in_base(t):
    t = t.rstrip("/")
    if t in base:
        return True
    # A migration cited by its number names the files it prefixes.
    return t.startswith("migrations/") and any(b.startswith(t) for b in base)


def citations(f):
    m = modroot(f)

    def sub(mo):
        dot, t = mo.group(1) or "", balanced(mo.group(2))
        tail = mo.group(2)[len(t):]
        if m and os.path.exists(os.path.join(m, t.rstrip("/"))):
            return mo.group(0)
        if dot and m:
            return mo.group(0)
        # web/ moves whole, so a path under it moves with it whether git
        # tracks it or not: .env.local, .next and node_modules.
        if not in_base(t) and not t.startswith("web/"):
            return mo.group(0)
        return dot + new_path(t) + tail
    def cmd(s):
        # A command naming the folder it runs in: pnpm --dir web, git -C web.
        return s if m else re.sub(r'(--dir|-C) web\b(?!/)', r'\1 console', s)
    return lambda s: cmd(TOK.sub(sub, s))


changed = 0
for f in git_files():
    if f.startswith("docs/architecture/adr/"):
        continue
    changed += rewrite(f, citations(f))
print("path citations rewritten in", changed, "files")

# --- 4. what the citation sweep cannot read ------------------------------
# Lines whose path is relative to a directory a command changes into, or
# that name a moved root file. Each old text must occur exactly once.
EDITS = {}


def edit(path, old, new, count=1):
    EDITS.setdefault(path, []).append((old, new, count))


def apply_edits():
    errors = []
    for path, pairs in EDITS.items():
        with open(path, encoding="utf-8") as f:
            s = f.read()
        for old, new, count in pairs:
            n = s.count(old)
            if n == 0 and new in s:
                continue  # the citation sweep already wrote it
            if n != count:
                errors.append(f"{path}: expected {count} of {old!r}, found {n}")
                continue
            s = s.replace(old, new)
        with open(path, "w", encoding="utf-8") as f:
            f.write(s)
    if errors:
        sys.exit("\n".join(errors))
    print("edits applied to", len(EDITS), "files")


exec(open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "move_edits.py")).read())
apply_edits()

# --- 4b. line width ------------------------------------------------------
# A longer path can carry a line past 72. Each paragraph a rewrite did
# that to is rewrapped from that line on; only whitespace moves.
HERE = os.path.dirname(os.path.abspath(__file__))
for _ in range(3):
    report = subprocess.run(["tooling/check-line-width.sh"], capture_output=True, text=True).stdout
    new = subprocess.run([sys.executable, os.path.join(HERE, "move/added.py"), BASE], input=report,
                         capture_output=True, text=True, check=True).stdout
    new += subprocess.run([sys.executable, os.path.join(HERE, "move/added.py"), BASE, "--jsdoc"],
                          capture_output=True, text=True, check=True).stdout
    if not new:
        break
    subprocess.run([sys.executable, os.path.join(HERE, "move/reflow.py")], input=new, text=True, check=True)
print("line width: the move leaves", len(new.splitlines()), "new violations")
# prettier owns the layout of the TypeScript a path rewrite lengthened.
changed_ts = [f[len("console/"):] for f in subprocess.run(["git", "diff", "--name-only", "-M", BASE, "--", "console/"],
              capture_output=True, text=True, check=True).stdout.split() if f.endswith((".ts", ".tsx"))]
sh("pnpm", "--dir", "console", "install", "--frozen-lockfile", "--silent")
sh("pnpm", "--dir", "console", "exec", "prettier", "--write", "--log-level", "warn", *changed_ts)
commit("refactor: rewrite the module path, imports and path citations for the move (#1659)",
       "Mechanical: the module becomes github.com/garamsh/garam/platform,\n"
       "and each citation of a moved path names where it now is. ADR bodies\n"
       "are frozen and keep the paths they were written against.")

# --- 5. generated output ----------------------------------------------
# protoc-gen-go writes each file's go_package into its descriptor.
sh("make", "gen")
commit("chore: regenerate for the platform module path (#1659)",
       "What make gen writes once the go_package options name the new module\n"
       "path: the descriptors protoc-gen-go embeds carry it.")

# --- 6. the web workspace ----------------------------------------------
# ADR-0125 decision 3. A command run inside a package still finds that
# package's own pnpm-workspace.yaml first, so each keeps its lockfile
# and its allowBuilds; a recursive command run here reads this one.
with open("pnpm-workspace.yaml", "w") as f:
    f.write("""# Every web package (ADR-0125), for a recursive pnpm run from here. A
# command run inside a package reads that package's own
# pnpm-workspace.yaml first, which keeps its lockfile and allowBuilds.
packages:
  - console
  - cowork/engine
  - cowork/web
""")
commit("build: name every web package in a root pnpm workspace (#1659)",
       "ADR-0125 decision 3. Each package keeps its own lockfile: pnpm run\n"
       "inside one reads that package's pnpm-workspace.yaml first.")
