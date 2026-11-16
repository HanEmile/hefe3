#!/usr/bin/env python3
"""Auto-create day/hour sections for photo galleries.

Runs of (dimg ...) lines in input/*.md get organized: photos spanning
more than one calendar day are split into day sections, days with more
than HOUR_THRESHOLD photos split further into hour sections.  Heading
levels derive from the surrounding section: day headings nest one
level below the current section (## Photos -> ### day), hour headings
one below that.

Grouping uses the manifest's EXIF capture time per photo - pure
manifest work, no Immich, no re-encoding.

Idempotent: auto-generated headings (day/hour labels whose section
contains only photos) are stripped before regrouping; handwritten
sections (any text content) are never touched.

usage:
  python3 tools/gallery-sections.py [--dry-run] [page ...]
"""

import argparse, glob, importlib.util, json, os, re, sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
_spec = importlib.util.spec_from_file_location(
    "pp", os.path.join(ROOT, "tools", "publish-photos.py"))
pp = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(pp)

HOUR_THRESHOLD = 12

DIMG_RE = re.compile(r"^\(dimg " + re.escape(pp.URL_BASE) +
                     r"/(\S+)-dither\.webp ", re.M)
HEAD_RE = re.compile(r"^(#{1,6}) (\S.*)$")
AUTO_HEAD_RE = re.compile(r"^#{2,6} (\d{4}-\d{2}-\d{2}|\d{2}:\d{2})\s*$")


def group_lines(manifest_exif, run):
    """-> [(day, [(hour, [lines])])], order preserved."""
    days = {}
    for line in run:
        m = DIMG_RE.match(line)
        dt = manifest_exif.get(m.group(1), "") if m else ""
        day = dt[:10] or "unknown"
        hour = dt[11:13] + ":00" if dt else "unknown"
        days.setdefault(day, {}).setdefault(hour, []).append(line)
    return list(days.items())


def sectionize(manifest_exif, text):
    lines = text.split("\n")

    # pass 1: strip auto-generated headings whose section holds only
    # dimg lines/blanks/other auto headings (so re-runs are idempotent);
    # iterate to a fixpoint: nested day/hour headings peel outside-in
    while True:
        kept = []
        i = 0
        changed = False
        while i < len(lines):
            if AUTO_HEAD_RE.match(lines[i]):
                j = i + 1
                only_photos, n = True, 0
                while j < len(lines) and not HEAD_RE.match(lines[j]):
                    if DIMG_RE.match(lines[j]):
                        n += 1
                    elif lines[j].strip():
                        only_photos = False
                        break
                    j += 1
                if only_photos and n:
                    i += 1  # drop the heading; re-created below if needed
                    changed = True
                    continue
            kept.append(lines[i])
            i += 1
        lines = kept
        if not changed:
            break

    # pass 2: split dimg runs into day/hour sections
    out = []
    cur_level = 1
    i, n = 0, len(kept)
    while i < n:
        m = HEAD_RE.match(kept[i])
        if m:
            cur_level = len(m.group(1))
        if not DIMG_RE.match(kept[i]):
            out.append(kept[i])
            i += 1
            continue
        # collect the run of dimg lines (blanks between them allowed)
        run = []
        while i < n and (DIMG_RE.match(kept[i]) or kept[i].strip() == ""):
            if DIMG_RE.match(kept[i]):
                run.append(kept[i])
            i += 1
        days = group_lines(manifest_exif, run)
        multi_day = len([d for d, _ in days if d != "unknown"]) > 1
        for day, hours in days:
            day_level = min(6, cur_level + 1)
            hour_level = min(6, day_level + 1 if multi_day else cur_level + 1)
            if day != "unknown" and multi_day:
                out.append("#" * day_level + " " + day)
                out.append("")
            if sum(len(h) for h in hours.values()) > HOUR_THRESHOLD:
                for hour, hlines in hours.items():
                    if hour != "unknown":
                        out.append("#" * hour_level + " " + hour)
                        out.append("")
                    out.extend(hlines)
                    out.append("")
            else:
                out.extend(line for h in hours.values() for line in h)
            out.append("")
    text = re.sub(r"\n{3,}", "\n\n", "\n".join(out))
    return text if text.endswith("\n") else text + "\n"


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("pages", nargs="*")
    ap.add_argument("--dry-run", action="store_true")
    a = ap.parse_args()

    manifest = json.load(open(os.path.join(ROOT, "published-photos.json")))
    manifest_exif = {pp.stem_of(e): (e.get("exif") or {}).get("dt", "")
                     for e in manifest.values()}

    for path in sorted(glob.glob(os.path.join(ROOT, "input", "*.md"))):
        name = os.path.basename(path)[:-3]
        if a.pages and name not in a.pages:
            continue
        text = open(path).read()
        if "(dimg " not in text:
            continue
        # only pages whose photos span more than one day, or exceed the
        # hour threshold on a single day
        stems = DIMG_RE.findall(text)
        photo_days = {manifest_exif.get(s, "")[:10] for s in stems}
        photo_days.discard("")
        day_counts = {}
        for s in stems:
            d = manifest_exif.get(s, "")[:10]
            day_counts[d] = day_counts.get(d, 0) + 1
        if len(photo_days) < 2 and max(day_counts.values(),
                                       default=0) <= HOUR_THRESHOLD:
            continue
        new = sectionize(manifest_exif, text)
        if new == text:
            continue
        print(f"[{'?' if a.dry_run else '+'}] {name}: "
              f"{len(photo_days)} days, {len(stems)} photos sectionized")
        if not a.dry_run:
            with open(path, "w") as f:
                f.write(new)


if __name__ == "__main__":
    main()
