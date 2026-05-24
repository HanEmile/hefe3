#!/usr/bin/env bash
# WCAG 2.1 AA contrast checker for tools/status-board/main.go.
#
# Extracts every :root colour variable and every color:/background:
# pair from the inline <style> block in tools/status-board/main.go,
# resolves var(--name) references, computes the WCAG 2.1 relative
# luminance and contrast ratio of each foreground/background pair
# that's actually used together, and prints a table.
#
# AA thresholds (WCAG 2.1):
# - normal text:  >= 4.5 : 1
# - large text:   >= 3.0 : 1   (>= 18px, or >= 14px && font-weight >= 700)
#
# Usage:
#   bash tools/status-board/check-contrast.sh            # dark (default)
#   bash tools/status-board/check-contrast.sh --mode=light
#
# Exit 0 = all combinations pass AA. Exit 1 = at least one fails.
set -euo pipefail

MODE=dark
for arg in "$@"; do
  case "$arg" in
    --mode=light) MODE=light ;;
    --mode=dark)  MODE=dark ;;
    -h|--help)
      sed -n '2,17p' "$0"; exit 0 ;;
    *) echo "unknown arg: $arg" >&2; exit 2 ;;
  esac
done

HERE="$(cd "$(dirname "$0")" && pwd)"
SRC="${HERE}/main.go"
if [ ! -f "$SRC" ]; then
  echo "main.go not found at $SRC" >&2; exit 2
fi

exec python3 - "$SRC" "$MODE" <<'PY'
import re, sys

src_path, mode = sys.argv[1], sys.argv[2]
with open(src_path) as f:
    src = f.read()

# Extract the HTML inline <style>...</style> block (the largest one).
styles = re.findall(r"<style>(.*?)</style>", src, re.S)
if not styles:
    print("no <style> blocks found", file=sys.stderr); sys.exit(2)
css = max(styles, key=len)

# Strip /* ... */ comments.
css_nc = re.sub(r"/\*.*?\*/", "", css, flags=re.S)

# Parse :root { ... } blocks. Support multiple (default + media query).
root_re = re.compile(r":root\s*\{([^}]*)\}", re.S)
roots = root_re.findall(css_nc)

# Locate :root inside `@media (prefers-color-scheme: light)` for light mode.
# Use a balanced-brace walk because @media blocks contain nested { ... } rules.
def extract_media_light(s):
    m = re.search(r"@media\s*\(prefers-color-scheme:\s*light\)\s*\{", s)
    if not m: return None
    i = m.end(); depth = 1; start = i
    while i < len(s) and depth > 0:
        if s[i] == "{": depth += 1
        elif s[i] == "}": depth -= 1
        i += 1
    return s[start:i-1] if depth == 0 else None

light_media_body = extract_media_light(css_nc)
light_root = None
if light_media_body is not None:
    rm = root_re.search(light_media_body)
    if rm:
        light_root = rm.group(1)
# alias for downstream code
m = (light_media_body is not None)
class _M:  # shim so `m.group(1)` still works below
    def __init__(self, body): self._b = body
    def group(self, i): return self._b
m_obj = _M(light_media_body) if light_media_body is not None else None

# The "default" :root is the first one outside any @media block.
# Quick approach: take the first :root in css with media block removed.
def strip_media_blocks(s):
    out = []; i = 0
    while i < len(s):
        m2 = re.search(r"@media[^{]*\{", s[i:])
        if not m2:
            out.append(s[i:]); break
        out.append(s[i:i+m2.start()])
        j = i + m2.end(); depth = 1
        while j < len(s) and depth > 0:
            if s[j] == "{": depth += 1
            elif s[j] == "}": depth -= 1
            j += 1
        i = j
    return "".join(out)
css_no_media = strip_media_blocks(css_nc)
default_root_m = root_re.search(css_no_media)
if not default_root_m:
    print("no default :root found", file=sys.stderr); sys.exit(2)

def parse_vars(block):
    out = {}
    for line in block.splitlines():
        line = line.strip().rstrip(";")
        m = re.match(r"(--[\w-]+)\s*:\s*(.+)$", line)
        if m:
            out[m.group(1)] = m.group(2).strip()
    return out

vars_default = parse_vars(default_root_m.group(1))
vars_light = parse_vars(light_root) if light_root else {}

if mode == "light":
    if not vars_light:
        print("ERROR: --mode=light requested but no @media (prefers-color-scheme: light) :root block found", file=sys.stderr)
        sys.exit(1)
    cvars = dict(vars_default)
    cvars.update(vars_light)
else:
    cvars = dict(vars_default)

def hex_to_rgb(h):
    h = h.strip().lstrip("#")
    if len(h) == 3:
        h = "".join(c*2 for c in h)
    if len(h) != 6:
        return None
    return tuple(int(h[i:i+2], 16) for i in (0,2,4))

def resolve(val, seen=None):
    seen = seen or set()
    val = val.strip().rstrip(";").strip()
    # Strip !important and similar.
    val = re.sub(r"\s*!important\s*$", "", val).strip()
    m = re.match(r"var\((--[\w-]+)(?:\s*,\s*([^)]+))?\)\s*$", val)
    if m:
        name = m.group(1)
        if name in seen: return None
        seen = seen | {name}
        if name in cvars:
            return resolve(cvars[name], seen)
        if m.group(2):
            return resolve(m.group(2), seen)
        return None
    if val.startswith("#"):
        return hex_to_rgb(val)
    # named colors / gradients: skip
    return None

def luminance(rgb):
    def chan(c):
        s = c / 255.0
        return s/12.92 if s <= 0.03928 else ((s+0.055)/1.055) ** 2.4
    r,g,b = (chan(c) for c in rgb)
    return 0.2126*r + 0.7152*g + 0.0722*b

def ratio(fg, bg):
    L1, L2 = luminance(fg), luminance(bg)
    if L1 < L2: L1, L2 = L2, L1
    return (L1 + 0.05) / (L2 + 0.05)

# Now walk CSS rules. We strip the @media wrapper but track which mode each rule belongs to,
# so we only check rules whose variables resolve in current `cvars`.

# Remove :root blocks and @media block but keep media-inner rules tagged for light only.
def strip_top_root(s):
    return root_re.sub("", s)

# Extract default-mode rules: css_no_media minus :root.
default_rules_src = strip_top_root(css_no_media)

# Extract light-mode rules: inside the media block, minus :root.
light_rules_src = ""
if m_obj is not None:
    light_rules_src = strip_top_root(m_obj.group(1))

# For checking a given mode, gather rule blocks:
rules_src = default_rules_src
if mode == "light" and light_rules_src.strip():
    rules_src += "\n" + light_rules_src

# Simple rule parser: selector { decls }
rule_re = re.compile(r"([^{}]+)\{([^{}]*)\}", re.S)

# Collect rules
rules = []
for m2 in rule_re.finditer(rules_src):
    sel = m2.group(1).strip()
    body = m2.group(2)
    decls = {}
    for d in body.split(";"):
        d = d.strip()
        if not d: continue
        if ":" not in d: continue
        k, v = d.split(":", 1)
        k = k.strip().lower(); v = v.strip()
        decls[k] = v
    rules.append((sel, decls))

# Helper: find nearest enclosing background for a selector.
# We treat each selector independently. For pairing, we look at rules
# matching a more general prefix of the selector (e.g. ".legend .sw.ok"
# falls back to ".legend"). If none, fall back to body / var(--bg).
def bg_for_selector(sel):
    # parts split on whitespace; try progressively shorter prefixes.
    parts = sel.split()
    for i in range(len(parts), 0, -1):
        prefix = " ".join(parts[:i])
        # exact match on prefix (any rule with that selector)
        for s2, d2 in rules:
            for s_alt in [x.strip() for x in s2.split(",")]:
                if s_alt == prefix and "background-color" in d2:
                    return d2["background-color"]
                if s_alt == prefix and "background" in d2:
                    # background shorthand: first token might be a color
                    tok = d2["background"].split()[0]
                    if tok.startswith("#") or tok.startswith("var("):
                        return tok
        # also try without final pseudo / .modifier
        if i == len(parts):
            stripped = re.sub(r"(\.[\w-]+)+$", "", parts[i-1]).strip()
            if stripped and stripped != parts[i-1]:
                cand = " ".join(parts[:i-1] + [stripped]) if stripped else " ".join(parts[:i-1])
                for s2, d2 in rules:
                    for s_alt in [x.strip() for x in s2.split(",")]:
                        if s_alt == cand and ("background-color" in d2 or "background" in d2):
                            bg = d2.get("background-color") or d2.get("background","").split()[0]
                            if bg.startswith("#") or bg.startswith("var("):
                                return bg
    return "var(--bg)"

def font_size_px(decls_chain):
    for d in reversed(decls_chain):
        if "font-size" in d:
            m3 = re.match(r"([\d.]+)px", d["font-size"])
            if m3: return float(m3.group(1))
    return 13.0  # body default

def font_weight(decls_chain):
    for d in reversed(decls_chain):
        if "font-weight" in d:
            try: return int(d["font-weight"])
            except: pass
    return 400

pairs = []
for sel, decls in rules:
    for s_alt in [x.strip() for x in sel.split(",")]:
        if "color" in decls:
            # Pair with same-rule bg if present, else nearest enclosing bg.
            if "background-color" in decls:
                bg_val = decls["background-color"]
            elif "background" in decls:
                tok = decls["background"].split()[0]
                bg_val = tok if (tok.startswith("#") or tok.startswith("var(")) else bg_for_selector(s_alt)
            else:
                bg_val = bg_for_selector(s_alt)

            fg_val = decls["color"]
            # size/weight from this rule (no real cascade; good enough)
            size = font_size_px([decls])
            weight = font_weight([decls])
            pairs.append((s_alt, fg_val, bg_val, decls, size, weight))

# Dedup identical pairs (same selector+fg+bg)
seen = set(); uniq = []
for p in pairs:
    key = (p[0], p[1], p[2])
    if key in seen: continue
    seen.add(key); uniq.append(p)

# Print table.
print(f"WCAG AA contrast check - mode={mode}")
print(f"{'selector':40} {'fg':22} {'bg':22} {'ratio':>7}  thr  status")
print("-"*110)
fails = 0
checked = 0
for sel, fg_val, bg_val, decls, size, weight in uniq:
    fg = resolve(fg_val)
    bg = resolve(bg_val)
    if fg is None or bg is None:
        # Can't compute - skip silently (e.g. unresolved var, gradients).
        continue
    checked += 1
    r = ratio(fg, bg)
    large = (size >= 18.0) or (size >= 14.0 and weight >= 700)
    thr = 3.0 if large else 4.5
    ok = r >= thr
    status = "OK" if ok else "FAIL"
    if not ok: fails += 1
    print(f"{sel[:40]:40} {fg_val[:22]:22} {bg_val[:22]:22} {r:6.2f}:1  {thr:.1f}  {status}")

print("-"*110)
print(f"{checked} pairs checked, {fails} failure(s)")
sys.exit(1 if fails else 0)
PY
