#!/usr/bin/env python3
"""Rewrap, from each named line to its paragraph's end, text a path rewrite pushed past 72.

usage: reflow.py < "file:line" lines (check-line-width's violations)

Only whitespace moves: the words and their order are kept, and a code
span is one word, so `git diff --word-diff` shows nothing for it.
"""
import re
import sys
import collections

WIDTH = 72
COMMENT = re.compile(r'^(\s*(?:#|//|--|\*(?!/))\s?)(.*)$')
ITEM = re.compile(r'^(\s*)([-*+]|\d+[.)])(\s+)')


def width(s):
    # A tab counts as one character, as the checker counts it.
    return len(s)


def words(text):
    out, cur, tick = [], "", False
    for ch in text:
        if ch == "`":
            tick = not tick
        if ch == " " and not tick:
            if cur:
                out.append(cur)
            cur = ""
        else:
            cur += ch
    if cur:
        out.append(cur)
    return out


def wrap(first_prefix, cont_prefix, ws):
    lines, cur, prefix = [], "", first_prefix
    for w in ws:
        if cur and width(prefix + cur + " " + w) > WIDTH:
            lines.append(prefix + cur)
            cur, prefix = w, cont_prefix
        else:
            cur = (cur + " " + w) if cur else w
    if cur:
        lines.append(prefix + cur)
    return lines


def block_md(lines, i):
    """The lines from i to the paragraph's end, the first's prefix, the rest's."""
    line = lines[i]
    m = ITEM.match(line)
    lead = re.match(r'^\s*', line).group(0)
    if m:
        first, cont = m.group(0), " " * len(m.group(0))
        text = [line[len(first):]]
    else:
        first = cont = lead
        text = [line[len(lead):]]
    j = i + 1
    while j < len(lines):
        nxt = lines[j]
        if not nxt.strip() or ITEM.match(nxt) or nxt.lstrip().startswith(("#", "|", "```", ">", "<")):
            break
        if re.match(r'^\s*', nxt).group(0) != cont:
            break
        text.append(nxt[len(cont):])
        j += 1
    return j, first, cont, " ".join(text)


def block_code(lines, i):
    m = COMMENT.match(lines[i])
    if not m:
        return None
    prefix = m.group(1)
    if not prefix.endswith(" "):
        return None
    body = m.group(2)
    im = ITEM.match(body)
    first, cont = prefix, prefix
    if im:
        first = prefix + im.group(0)
        cont = prefix + " " * len(im.group(0))
        body = body[len(im.group(0)):]
    else:
        lead = re.match(r'^\s*', body).group(0)
        first = cont = prefix + lead
        body = body[len(lead):]
    text, j = [body], i + 1
    while j < len(lines):
        nm = COMMENT.match(lines[j])
        if not nm or not nm.group(2).strip():
            break
        if not lines[j].startswith(cont) or ITEM.match(lines[j][len(prefix):]):
            break
        rest = lines[j][len(cont):]
        if rest.startswith(" "):
            break
        text.append(rest)
        j += 1
    return j, first, cont, " ".join(text)


def main():
    todo = collections.defaultdict(set)
    for raw in sys.stdin:
        m = re.match(r'^\s*(\S+?):(\d+): ', raw)
        if m:
            todo[m.group(1)].add(int(m.group(2)))
    for f, nums in todo.items():
        with open(f, encoding="utf-8") as fh:
            lines = fh.read().split("\n")
        doc = f.endswith(".md")
        # Bottom up, so an earlier rewrap leaves later numbers in place.
        for n in sorted(nums, reverse=True):
            i = n - 1
            got = block_md(lines, i) if doc else block_code(lines, i)
            if not got:
                print(f"{f}:{n}: not a paragraph line, left", file=sys.stderr)
                continue
            j, first, cont, text = got
            new = wrap(first, cont, words(text))
            bare = [new[0][len(first):]] + [x[len(cont):] for x in new[1:]]
            if words(" ".join(bare)) != words(text):
                print(f"{f}:{n}: rewrap would change the words, left", file=sys.stderr)
                continue
            lines[i:j] = new
        with open(f, "w", encoding="utf-8") as fh:
            fh.write("\n".join(lines))


main()
