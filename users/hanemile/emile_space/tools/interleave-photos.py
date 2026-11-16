#!/usr/bin/env python3
"""Interleave a page's '## Photos' gallery under its per-day headings.

Pages like lanzarote-fuerteventura carry '## YYYY-MM-DD' sections for
each day of the trip and a single '## Photos' section at the bottom.
This moves every (dimg ...) line from the Photos section to the end of
the matching date section, by the photo's fileCreatedAt date (Immich,
via the publish manifest's asset ids).  Photos whose date has no
section stay at the bottom.

usage:
  IMMICH_API_KEY=... ./nixrun.sh \
    'python3 tools/interleave-photos.py [--dry-run] [page ...]'
"""

import argparse, glob, importlib.util, json, os, re, sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
_spec = importlib.util.spec_from_file_location(
    "pp", os.path.join(ROOT, "tools", "publish-photos.py"))
pp = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(pp)

DIMG_RE = re.compile(r"^\(dimg " + re.escape(pp.URL_BASE) +
                     r"/(\S+)-dither\.webp ")
DAY_RE = re.compile(r"^## (\d{4}-\d{2}-\d{2})\s*$")
PHOTOS_RE = re.compile(r"^## Photos\s*$", re.M)


def parse_page(text):
    """-> (sections, photos-lines) where sections is a list of
    (day-or-None, [body-lines]) and photos-lines the (dimg ...) lines
    found in the '## Photos' section.  Heading lines stay in their
    section's body."""
    sections, photos = [], []
    cur_day, cur = None, []
    in_photos = False
    for line in text.split("\n"):
        m = DAY_RE.match(line)
        if m:
            sections.append((cur_day, cur))
            cur_day, cur, in_photos = line.split("## ")[1], [line], False
            continue
        if PHOTOS_RE.match(line):
            sections.append((cur_day, cur))
            cur_day, cur, in_photos = None, [], True
            continue
        m2 = DIMG_RE.match(line)
        if in_photos and m2:
            photos.append((m2.group(1), line))
            continue
        cur.append(line)
    sections.append((cur_day, cur))
    return sections, photos


def rebuild(sections, by_day, unassigned):
    out = []
    for day, body in sections:
        for line in body:
            out.append(line)
        if day and by_day.get(day):
            while out and out[-1] == "":
                out.pop()
            out.append("")
            out.extend(by_day[day])
            out.append("")
    if unassigned:
        out.extend(["", "## Photos", ""])
        out.extend(unassigned)
        out.append("")
    text = "\n".join(out)
    text = re.sub(r"\n{3,}", "\n\n", text)
    if not text.endswith("\n"):
        text += "\n"
    return text


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("pages", nargs="*",
                    help="page names (default: all with day headings "
                         "and a Photos section)")
    ap.add_argument("--dry-run", action="store_true")
    a = ap.parse_args()

    key = os.environ.get("IMMICH_API_KEY")
    if not key:
        sys.exit("[!] set IMMICH_API_KEY")
    base = os.environ.get("IMMICH_URL", "https://photo.emile.space")
    manifest = json.load(open(pp.MANIFEST))

    stem_of = {}
    date_of = {}
    for aid, e in manifest.items():
        stem = pp.stem_of(e)
        stem_of[stem] = (aid, e.get("album"))

    for path in sorted(glob.glob(os.path.join(ROOT, "input", "*.md"))):
        name = os.path.basename(path)[:-3]
        if a.pages and name not in a.pages:
            continue
        text = open(path).read()
        if not PHOTOS_RE.search(text):
            continue
        sections, photos = parse_page(text)
        days = [d for d, _ in sections if d]
        if len(days) < 2 or not photos:
            continue

        # fetch the date of every involved album once
        need = {stem_of[s][1] for s, _ in photos
                if s in stem_of and stem_of[s][1]}
        for slug in need:
            if slug in date_of:
                continue
            albums = pp.api(base, key, "/albums")
            match = [al for al in albums
                     if pp.album_slug(al["albumName"]) == slug]
            if len(match) != 1:
                print(f"[!] album slug '{slug}' matched {len(match)}; "
                      "skipping its photos")
                date_of[slug] = {}
                continue
            dates = {}
            for x in pp.list_album_assets(base, key, match[0]["id"]):
                dates[x["id"]] = (x.get("fileCreatedAt") or "")[:10]
            date_of[slug] = dates

        # per-day order: EXIF capture time, manifest order as fallback
        def exif_dt(stem):
            info = stem_of.get(stem)
            e = info and manifest.get(info[0])
            return (e or {}).get("exif", {}).get("dt") or "~"

        by_day, unassigned = {}, []
        for stem, line in photos:
            info = stem_of.get(stem)
            d = (info and date_of.get(info[1], {}).get(info[0])) or ""
            if d and d in days:
                by_day.setdefault(d, []).append((exif_dt(stem), line))
            else:
                unassigned.append(line)
        by_day = {d: [line for _, line in sorted(v)]
                  for d, v in by_day.items()}
        moved = sum(len(v) for v in by_day.values())
        print(f"[{'?' if a.dry_run else '+'}] {name}: {moved} photos "
              f"interleaved into {len(by_day)} days, "
              f"{len(unassigned)} left in Photos")
        for d in days:
            if by_day.get(d):
                print(f"      {d}: {len(by_day[d])}")
        if not a.dry_run and moved:
            with open(path, "w") as f:
                f.write(rebuild(sections, by_day, unassigned))


if __name__ == "__main__":
    main()
