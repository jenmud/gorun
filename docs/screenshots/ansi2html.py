#!/usr/bin/env python3
"""Turn a tmux pane capture (tmux capture-pane -e -p) into an HTML page that looks like
the terminal, for headless Chromium to screenshot. Standard library only.

    ansi2html.py capture.txt page.html
"""

import html
import re
import sys

FG, BG = "#d4d4d8", "#0b0b0d"

# the 16 basic colours, close to a typical dark terminal theme
BASIC = [
    "#1f1f23", "#ef4444", "#22c55e", "#f59e0b", "#3b82f6", "#a855f7", "#06b6d4", "#d4d4d8",
    "#52525b", "#f87171", "#4ade80", "#fbbf24", "#60a5fa", "#c084fc", "#22d3ee", "#fafafa",
]


def xterm256(n):
    if n < 16:
        return BASIC[n]
    if n < 232:
        n -= 16
        steps = [0, 95, 135, 175, 215, 255]
        return "#%02x%02x%02x" % (steps[n // 36], steps[(n // 6) % 6], steps[n % 6])
    v = 8 + (n - 232) * 10
    return "#%02x%02x%02x" % (v, v, v)


class Style:
    def __init__(self):
        self.reset()

    def reset(self):
        self.fg = self.bg = None
        self.bold = self.faint = self.italic = self.underline = self.reverse = False

    def apply(self, params):
        codes = [int(p) if p else 0 for p in params.split(";")] if params else [0]
        i = 0
        while i < len(codes):
            c = codes[i]
            if c == 0:
                self.reset()
            elif c == 1:
                self.bold = True
            elif c == 2:
                self.faint = True
            elif c == 3:
                self.italic = True
            elif c == 4:
                self.underline = True
            elif c == 7:
                self.reverse = True
            elif c == 22:
                self.bold = self.faint = False
            elif c == 23:
                self.italic = False
            elif c == 24:
                self.underline = False
            elif c == 27:
                self.reverse = False
            elif 30 <= c <= 37:
                self.fg = BASIC[c - 30]
            elif 90 <= c <= 97:
                self.fg = BASIC[c - 90 + 8]
            elif 40 <= c <= 47:
                self.bg = BASIC[c - 40]
            elif 100 <= c <= 107:
                self.bg = BASIC[c - 100 + 8]
            elif c == 39:
                self.fg = None
            elif c == 49:
                self.bg = None
            elif c in (38, 48) and i + 1 < len(codes):
                if codes[i + 1] == 5 and i + 2 < len(codes):
                    colour = xterm256(codes[i + 2])
                    i += 2
                elif codes[i + 1] == 2 and i + 4 < len(codes):
                    colour = "#%02x%02x%02x" % tuple(codes[i + 2:i + 5])
                    i += 4
                else:
                    colour = None
                if c == 38:
                    self.fg = colour
                else:
                    self.bg = colour
            i += 1

    def css(self):
        fg, bg = self.fg, self.bg
        if self.reverse:
            fg, bg = bg or BG, fg or FG
        rules = []
        if fg:
            rules.append("color:" + fg)
        if bg:
            rules.append("background:" + bg)
        if self.bold:
            rules.append("font-weight:bold")
        if self.faint:
            rules.append("opacity:.6")
        if self.italic:
            rules.append("font-style:italic")
        if self.underline:
            rules.append("text-decoration:underline")
        return ";".join(rules)


SGR = re.compile(r"\x1b\[([0-9;]*)m")
OTHER = re.compile(r"\x1b\[[0-9;?]*[A-Za-ln-z]|\x1b\][^\x07]*\x07")


def convert(text):
    style = Style()
    out = []
    for line in text.rstrip("\n").split("\n"):
        line = OTHER.sub("", line.replace("\t", "        "))
        parts, pos = [], 0
        for m in SGR.finditer(line):
            parts.append((line[pos:m.start()], style.css()))
            style.apply(m.group(1))
            pos = m.end()
        parts.append((line[pos:], style.css()))
        spans = "".join(
            '<span style="%s">%s</span>' % (css, html.escape(t)) if css else html.escape(t)
            for t, css in parts
            if t
        )
        out.append(spans)
        style.reset()  # tmux restates the style at the start of every line
    return "\n".join(out)


PAGE = """<!doctype html><meta charset="utf-8"><style>
html,body{margin:0;background:%(bg)s}
.term{display:inline-block;padding:28px 32px;background:%(bg)s;color:%(fg)s;
  font:15px/1.45 "JetBrainsMono Nerd Font","JetBrains Mono",monospace;white-space:pre;
  font-variant-ligatures:none}
</style><div class="term">%(body)s</div>
"""

if __name__ == "__main__":
    src, dst = sys.argv[1], sys.argv[2]
    with open(src, encoding="utf-8", errors="replace") as f:
        body = convert(f.read())
    with open(dst, "w", encoding="utf-8") as f:
        f.write(PAGE % {"bg": BG, "fg": FG, "body": body})
