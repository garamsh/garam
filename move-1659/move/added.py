import re, subprocess, sys
"""Keep the violation lines the move itself added: file:line inside a + hunk of base..HEAD."""
out = subprocess.run(["git","diff","-U0","-M",sys.argv[1]],capture_output=True,text=True).stdout
added, f = {}, None
for l in out.split("\n"):
    if l.startswith("+++ "): f = l[6:] if l.startswith("+++ b/") else None
    m = re.match(r'^@@ -\S+ \+(\d+)(?:,(\d+))? @@', l)
    if m and f:
        s, n = int(m.group(1)), int(m.group(2) or 1)
        added.setdefault(f, set()).update(range(s, s+n))
# A JSDoc line is a comment the checker does not reach (its marker is
# none of the three), so the lines the move pushed past 72 are found here.
if len(sys.argv) > 2 and sys.argv[2] == "--jsdoc":
    for f, nums in added.items():
        if not f.endswith((".ts", ".tsx", ".mts")):
            continue
        lines = open(f, encoding="utf-8").read().split("\n")
        for n in sorted(nums):
            l = lines[n - 1] if n <= len(lines) else ""
            if re.match(r'^\s*\* ', l) and len(l) > 72:
                print(f"  {f}:{n}: {len(l)} characters, jsdoc")
    sys.exit(0)
for raw in sys.stdin:
    m = re.match(r'^\s*(\S+?):(\d+): ', raw)
    if m and int(m.group(2)) in added.get(m.group(1), ()): print(raw, end="")
