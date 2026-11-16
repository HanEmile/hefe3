#!/usr/bin/env python3
"""Split a trip album into per-page galleries via page date ranges.

Bulk albums (e.g. '2025-09-Asia') get dumped onto the trip overview
page; the individual city pages of the trip (tagged with the trip tag,
carrying a (date "start" "end") range) each want just THEIR photos.
This tool matches album assets to pages by the asset's date falling
inside the page's range and (re)writes each page's '## Photos' section
from the publish manifest - pure manifest work, nothing re-encoded.

usage:
  IMMICH_API_KEY=... ./nixrun.sh \\
    'python3 tools/trip-galleries.py "2025-09-Asia" asia-2025'
    [--dry-run]
"""

import argparse, glob, importlib.util, json, os, re, sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
_spec = importlib.util.spec_from_file_location(
    "pp", os.path.join(ROOT, "tools", "publish-photos.py"))
pp = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(pp)
_gs_spec = importlib.util.spec_from_file_location(
    "gs", os.path.join(ROOT, "tools", "gallery-sections.py"))
gs = importlib.util.module_from_spec(_gs_spec)
_gs_spec.loader.exec_module(gs)


def page_info(path):
    """(name, (start, end), raw) for pages tagged `tagged`, else None."""
    raw = open(path).read()
    return raw


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("album")
    ap.add_argument("tag", help="trip tag shared by the city pages")
    ap.add_argument("--dry-run", action="store_true")
    a = ap.parse_args()

    key = os.environ.get("IMMICH_API_KEY")
    if not key:
        sys.exit("[!] set IMMICH_API_KEY")
    base = os.environ.get("IMMICH_URL", "https://photo.emile.space")
    manifest = json.load(open(pp.MANIFEST))

    album = pp.find_album(base, key, a.album)
    slug = pp.album_slug(album["albumName"])
    assets = [x for x in pp.list_album_assets(base, key, album["id"])
              if x["type"] == "IMAGE"]

    # city pages: tag match, date range, not the overview, not a book
    pages = []
    tag_re = re.compile(r'\(tag[^\)]*"' + re.escape(a.tag) + r'"')
    date_re = re.compile(r'\(date\s+"([0-9-]+)"(?:\s+"([0-9-]+)")?\s*\)')
    for path in sorted(glob.glob(os.path.join(ROOT, "input", "*.md"))):
        name = os.path.basename(path)[:-3]
        raw = page_info(path)
        if name == a.tag or "(book " in raw or not tag_re.search(raw):
            continue
        m = date_re.search(raw)
        if not m:
            continue
        pages.append((name, path, m.group(1), m.group(2) or m.group(1)))
    print(f"[ ] {len(pages)} date-ranged pages for tag '{a.tag}'")

    assigned = {name: [] for name, _, _, _ in pages}
    for x in assets:
        entry = manifest.get(x["id"])
        if not entry or not entry.get("album", "").startswith(slug):
            if not entry:
                print(f"[!] {x['originalFileName']} not in manifest yet, "
                      "skipped")
                continue
        day = (x.get("fileCreatedAt") or "")[:10]
        for name, _, start, end in pages:
            if start <= day <= end:
                assigned[name].append((day, x["id"], entry))
                break

    for name, path, start, end in pages:
        # chronological by EXIF capture time, fileCreatedAt as fallback
        hits = sorted(assigned[name],
                      key=lambda t: (t[2].get("exif", {}).get("dt")
                                     or t[0], t[1]))
        print(f"[ ] {name} ({start}..{end}): {len(hits)} photos")
        if a.dry_run or not hits:
            continue
        lines = []
        for _, _, e in hits:
            alt = os.path.splitext(e["name"])[0].replace('"', "'")
            w, h = e["dims"]
            lines.append(f'(dimg {pp.URL_BASE}/{pp.dither_file(e)} '
                         f'{pp.URL_BASE}/{e["main"]} {w} {h} "{alt}")')
            # [full-res] links are emitted by the build itself (render.ml
            # reads published-photos.json), not pasted into the markdown
        text = open(path).read()
        cut = text.find("\n## Photos\n")
        if cut != -1:
            text = text[:cut] + "\n"
        if not text.endswith("\n"):
            text += "\n"
        text = text + "\n## Photos\n\n" + "\n\n".join(lines) + "\n"
        # day/hour sections for the gallery (EXIF dates from the manifest)
        manifest_exif = {pp.stem_of(e): (e.get("exif") or {}).get("dt", "")
                         for e in manifest.values()}
        text = gs.sectionize(manifest_exif, text)
        with open(path, "w") as f:
            f.write(text)
        print(f"[+] {name}: gallery written")


if __name__ == "__main__":
    main()
