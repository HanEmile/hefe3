#!/usr/bin/env python3
"""Publish web images from an Immich album to img.emile.space.

Workflow: curate photos into an album on photo.emile.space, then run this.
It downloads the originals, creates resized/stripped/content-hashed web
derivatives, rsyncs them to naraj and prints markdown to paste into a page.

Derivatives are named <sha1>-<width>w.webp, never overwritten and never
deleted from the server: URLs stay valid forever. Re-runs are idempotent
thanks to the committed manifest (published-photos.json).

Runs inside ./nixrun.sh (needs Pillow from shell.nix):

  IMMICH_API_KEY=... ./nixrun.sh \\
    'python3 tools/publish-photos.py "asia 2026"'
  python3 tools/publish-photos.py --fix-staging   # backfill dither/dims
  python3 tools/publish-photos.py --redither      # regenerate all dithers

Annotations: tag/describe photos in the Immich UI (Tags feature +
description field).  They are pulled via the API on publish, stored in
the manifest ('tags'/'desc'), embedded as XMP (dc:subject/dc:description;
pure-python RIFF/JPEG-APP1 surgery, no re-encode) into every published
file, rendered into the dimg info line at build time, and used as the
dimg alt text.  After editing annotations in Immich:

  python3 tools/publish-photos.py --sync-annotations "asia 2026"

Album -> page matching: curate an album in Immich, then

  python3 tools/publish-photos.py "asia 2026" --page ho-chi-minh-city-2025-10

inserts the gallery between <!-- photos:... --> markers in
input/<page>.md (re-runs replace the block in place).  --albums lists
every Immich album with its publish state and the pages referencing it.

After rsync, every published url of the album is HEAD-verified; missing
files trigger one rsync retry, then a warning (catches interrupted
uploads, cf. the 2026-11-15 lost-cards incident).

Each asset yields <sha1>-<width>w.webp derivatives plus a per-channel
1-bit Bayer dithered (3-bit color, 8 colors) <sha1>-dither.webp; snippets
are emitted as vokobe2 (dimg ...) ops (dither shown, real image swapped
in + fetched only on hover).  A small undithered <sha1>-card.webp (max
200 wide) is used for card-grid and backlink-gallery previews.

The API key comes from photo.emile.space -> Administration -> API Keys.
Resolution order: IMMICH_API_KEY env -> gitignored ./.env -> the agenix
secret ops/secrets/immich_api_key.age in this repo (decrypted with the
default ssh identity).  IMMICH_URL overrides the instance URL (default
https://photo.emile.space).
"""

import argparse, concurrent.futures, hashlib, io, json, os, re, struct, \
    subprocess, sys, urllib.request

from PIL import Image, ImageChops, ImageOps

BAYER8 = (
    ( 0, 32,  8, 40,  2, 34, 10, 42),
    (48, 16, 56, 24, 50, 18, 58, 26),
    (12, 44,  4, 36, 14, 46,  6, 38),
    (60, 28, 52, 20, 62, 30, 54, 22),
    ( 3, 35, 11, 43,  1, 33,  9, 41),
    (51, 19, 59, 27, 49, 17, 57, 25),
    (15, 47,  7, 39, 13, 45,  5, 37),
    (63, 31, 55, 23, 61, 29, 53, 21),
)


def ordered_dither_color(rgb):
    """Per-channel 1-bit Bayer-8 dither of an RGB image: 3-bit color
    (8-color palette), stays tiny as lossless webp."""
    return Image.merge("RGB", [ordered_dither(b) for b in rgb.split()])


def ordered_dither(gray):
    """1-bit Bayer-8 dither of an L-mode image; pure Pillow/bytes ops."""
    m, n = len(BAYER8), len(BAYER8) ** 2
    w, h = gray.size
    wrep, wrem = divmod(w, m)
    block = b"".join(
        (bytes(int(v * 256 / n) for v in row) * wrep
         + bytes(int(v * 256 / n) for v in row[:wrem]))
        for row in BAYER8)
    hrep, hrem = divmod(h, m)
    tiled = block * hrep + b"".join(
        block[i * w:(i + 1) * w] for i in range(hrem))
    mask = Image.frombytes("L", (w, h), tiled)
    diff = ImageChops.subtract(gray, mask, scale=1.0, offset=128)
    return diff.point([0] * 128 + [255] * 128, "L")


def write_dither(staging, base_file, dither_file):
    """Dithered preview (from the smallest derivative) for the (dimg ..) op.
    3-bit per-channel Bayer dither: colored but tiny (lossless webp)."""
    with Image.open(os.path.join(staging, base_file)) as im:
        im = ImageOps.exif_transpose(im)
        dithered = ordered_dither_color(im.convert("RGB"))
    dithered.save(os.path.join(staging, dither_file), "WEBP", lossless=True)


def read_exif(img):
    """EXIF exposure info for the full-res info line:
    {"shutter": "1/250"|"2.5", "iso": 100, "f": "f/2.8"} — only the keys
    that exist.  Takes an open PIL image."""
    try:
        ex = img.getexif().get_ifd(0x8769)  # EXIF IFD
    except Exception:
        return {}
    out = {}

    def num(v):
        try:
            return float(v)
        except (TypeError, ValueError, ZeroDivisionError):
            return None

    t = num(ex.get(33434))  # ExposureTime (seconds)
    if t and t > 0:
        out["shutter"] = f"1/{round(1 / t)}" if t < 1 else f"{t:g}"
    iso = ex.get(34855) or ex.get(34867)  # ISOSpeedRatings / ISOSpeed
    if isinstance(iso, (tuple, list)):
        iso = iso[0] if iso else None
    try:
        if iso:
            out["iso"] = int(iso)
    except (TypeError, ValueError):
        pass
    f = num(ex.get(33437))  # FNumber
    if f:
        out["f"] = f"f/{f:g}"
    mm = num(ex.get(37386))  # FocalLength (physical, small-sensor
    # phone lenses really are ~1.5-5mm)
    if mm:
        out["mm"] = f"{mm:g}"
    eq = num(ex.get(41989))  # FocalLengthIn35mmFilm
    if eq:
        out["eq"] = f"{eq:g}"
    dt = ex.get(36867) or ex.get(306)  # DateTimeOriginal / DateTime
    if isinstance(dt, str) and len(dt) == 19 and dt[4] == ":":
        # EXIF 'YYYY:MM:DD HH:MM:SS' -> ISO 8601
        out["dt"] = dt[:4] + "-" + dt[5:7] + "-" + dt[8:10] + "T" + dt[11:]
    return out


def read_exif_file(path):
    try:
        with Image.open(path) as im:
            return read_exif(im)
    except Exception:
        return {}


def write_card(staging, base_file, card_file):
    """Undithered thumbnail (from the smallest derivative) for card-grid
    previews; max CARD_WIDTH wide, never upscaled."""
    with Image.open(os.path.join(staging, base_file)) as im:
        im = ImageOps.exif_transpose(im)
        if im.mode not in ("RGB", "L"):
            im = im.convert("RGB")
        if im.width > CARD_WIDTH:
            im = im.resize(
                (CARD_WIDTH, round(im.height * CARD_WIDTH / im.width)),
                Image.LANCZOS)
        im.save(os.path.join(staging, card_file), "WEBP",
                quality=QUALITY, method=6)


# --- annotations (Immich tags/description -> manifest -> XMP in files) ---

def asset_annotations(base, key, aid):
    """Immich asset detail -> (sorted tag values, description).  Tags are
    hierarchical in Immich; 'value' carries the full path ('trip/asia')."""
    d = api(base, key, f"/assets/{aid}")
    tags = []
    for t in d.get("tags") or []:
        v = t.get("value") or t.get("name")
        if v:
            tags.append(v)
    desc = (d.get("exifInfo") or {}).get("description") or ""
    return sorted(set(tags)), desc.strip()


def xmp_packet(tags, desc=None, title=None):
    """Minimal XMP packet: dc:subject (keywords), dc:description, dc:title."""
    def esc(s):
        return (s.replace("&", "&amp;").replace("<", "&lt;")
                 .replace(">", "&gt;"))
    props = ""
    if title:
        props += ('<dc:title><rdf:Alt><rdf:li xml:lang="x-default">'
                  f"{esc(title)}</rdf:li></rdf:Alt></dc:title>")
    if desc:
        props += ('<dc:description><rdf:Alt><rdf:li xml:lang="x-default">'
                  f"{esc(desc)}</rdf:li></rdf:Alt></dc:description>")
    if tags:
        lis = "".join(f"<rdf:li>{esc(t)}</rdf:li>" for t in tags)
        props += f"<dc:subject><rdf:Bag>{lis}</rdf:Bag></dc:subject>"
    return ('<?xpacket begin="\ufeff" id="W5M0MpCehiHzreSzNTczkc9d"?>\n'
            '<x:xmpmeta xmlns:x="adobe:ns:meta/" x:xmptk="emile.space">\n'
            '<rdf:RDF xmlns:rdf='
            '"http://www.w3.org/1999/02/22-rdf-syntax-ns#">\n'
            '<rdf:Description rdf:about="" xmlns:dc='
            '"http://purl.org/dc/elements/1.1/">'
            f"{props}</rdf:Description>\n</rdf:RDF>\n</x:xmpmeta>\n"
            '<?xpacket end="w"?>').encode("utf-8")


def _riff_chunks(data):
    """(fourcc, payload offset, payload size) for each RIFF chunk."""
    pos = 12
    while pos + 8 <= len(data):
        size, = struct.unpack_from("<I", data, pos + 4)
        yield data[pos:pos + 4], pos + 8, size
        pos += 8 + size + (size & 1)


def _webp_canvas_size(data):
    """Canvas (w, h) from a VP8L/VP8 chunk, for synthesizing VP8X."""
    for fourcc, off, size in _riff_chunks(data):
        p = data[off:off + size]
        if fourcc == b"VP8L" and size >= 5 and p[0] == 0x2F:
            w = 1 + (((p[2] & 0x3F) << 8) | p[1])
            h = 1 + (((p[4] & 0x0F) << 10) | (p[3] << 2) | (p[2] >> 6))
            return w, h
        if fourcc == b"VP8 " and size >= 10 and p[3:6] == b"\x9d\x01\x2a":
            return (struct.unpack_from("<H", p, 6)[0] & 0x3FFF,
                    struct.unpack_from("<H", p, 8)[0] & 0x3FFF)
    return None


def webp_set_xmp(path, xmp):
    """Replace (or drop, when xmp is None) the XMP chunk of a webp file
    in place without re-encoding the image.  Returns False on non-webp.
    Containers without VP8X (plain single-frame files) get one
    synthesized, as the extended chunks require it."""
    data = open(path, "rb").read()
    if data[:4] != b"RIFF" or data[8:12] != b"WEBP":
        return False
    chunks, has_vp8x, had_xmp = [], False, False
    for fourcc, off, size in _riff_chunks(data):
        if fourcc == b"XMP ":
            had_xmp = True
            continue  # old packet dropped
        payload = bytearray(data[off:off + size])
        if fourcc == b"VP8X":
            has_vp8x = True
            if xmp is not None:
                payload[0] |= 0x04  # XMP present flag
            else:
                payload[0] &= ~0x04
        chunks.append((bytes(fourcc), bytes(payload)))
    if xmp is None and not had_xmp:
        return False  # nothing to strip: leave the file untouched
    if xmp is not None and not has_vp8x:
        wh = _webp_canvas_size(data)
        if wh is None:
            print(f"[!] {os.path.basename(path)}: no canvas size, "
                  "xmp skipped", file=sys.stderr)
            return False
        payload = bytes([0x04, 0, 0, 0]) \
            + (wh[0] - 1).to_bytes(3, "little") \
            + (wh[1] - 1).to_bytes(3, "little")
        chunks.insert(0, (b"VP8X", payload))
    if xmp is not None:
        chunks.append((b"XMP ", xmp))
    body = b""
    for fourcc, payload in chunks:
        body += fourcc + struct.pack("<I", len(payload)) + payload
        if len(payload) & 1:
            body += b"\x00"
    with open(path, "wb") as f:
        f.write(b"RIFF" + struct.pack("<I", 4 + len(body)) + b"WEBP" + body)
    return True


JPEG_XMP_HDR = b"http://ns.adobe.com/xap/1.0/\x00"


def jpeg_xmp_bytes(data, xmp):
    """JPEG data with the XMP APP1 segment replaced (or dropped); None
    for non-JPEG input.  Metadata-only edit, no re-encode."""
    if data[:2] != b"\xff\xd8":
        return None
    out, pos = bytearray(data[:2]), 2
    if xmp is not None:
        payload = JPEG_XMP_HDR + xmp
        out += b"\xff\xe1" + struct.pack(">H", len(payload) + 2) + payload
    while pos + 4 <= len(data):
        if data[pos] != 0xFF:
            break  # entropy-coded data without SOS header: keep as-is
        marker = data[pos + 1]
        if marker in (0xD8, 0x01) or 0xD0 <= marker <= 0xD7:
            out += data[pos:pos + 2]
            pos += 2
            continue
        length, = struct.unpack_from(">H", data, pos + 2)
        if marker == 0xDA:  # SOS: scan data follows to EOF
            out += data[pos:]
            pos = len(data)
            break
        if not (marker == 0xE1
                and data[pos + 4:pos + 4 + len(JPEG_XMP_HDR)]
                == JPEG_XMP_HDR):
            out += data[pos:pos + 2 + length]
        pos += 2 + length
    out += data[pos:]
    return bytes(out)


def jpeg_set_xmp(path, xmp):
    raw = open(path, "rb").read()
    if xmp is None and JPEG_XMP_HDR not in raw:
        return False  # nothing to strip: leave the file untouched
    data = jpeg_xmp_bytes(raw, xmp)
    if data is None or data == raw:
        return data == raw
    with open(path, "wb") as f:
        f.write(data)
    return True


def entry_embed_xmp(entry):
    """Embed the entry's annotations as XMP into every local derivative
    and the staged original.  Returns number of files written."""
    tags, desc = entry.get("tags") or [], entry.get("desc")
    xmp = xmp_packet(tags, desc, entry.get("name")) if tags or desc else None
    n = 0
    for f in entry["files"] + [dither_file(entry), card_file(entry)]:
        p = os.path.join(STAGING, f)
        if os.path.exists(p) and webp_set_xmp(p, xmp):
            n += 1
    orig = entry.get("orig")
    if orig:  # usually flushed already; embeds on first publish
        p = os.path.join(ORIG_STAGING, orig)
        if os.path.exists(p):
            if orig.lower().endswith((".jpg", ".jpeg")):
                n += jpeg_set_xmp(p, xmp)
            else:
                print(f"[!] {orig}: xmp embed unsupported for this format",
                      file=sys.stderr)
    return n


def sync_annotations(manifest, base, key, album=None):
    """--sync-annotations: re-pull tags/description from Immich for every
    manifest entry (optionally one album), re-embed XMP where they
    changed, rsync.  Covers annotations edited after the first publish."""
    ids = [aid for aid, e in manifest.items()
           if not album or e.get("album") == album]
    print(f"[ ] syncing annotations for {len(ids)} assets"
          + (f" of album '{album}'" if album else ""), flush=True)
    changed = 0
    for i, aid in enumerate(ids, 1):
        e = manifest[aid]
        try:
            tags, desc = asset_annotations(base, key, aid)
        except Exception as ex:
            print(f"[!] {e.get('main', aid)}: {ex}", file=sys.stderr)
            continue
        if tags != (e.get("tags") or []) or desc != (e.get("desc") or ""):
            if tags:
                e["tags"] = tags
            else:
                e.pop("tags", None)
            if desc:
                e["desc"] = desc
            else:
                e.pop("desc", None)
            changed += 1
        if i % 100 == 0:
            print(f"[ ] {i}/{len(ids)} ({changed} changed)", flush=True)
            with open(MANIFEST, "w") as f:
                json.dump(manifest, f, indent=2, sort_keys=True)
    for aid in ids:
        entry_embed_xmp(manifest[aid])
    with open(MANIFEST, "w") as f:
        json.dump(manifest, f, indent=2, sort_keys=True)
    print(f"[+] {changed}/{len(ids)} annotation sets changed")
    if changed:
        upload_staging()
    else:
        print("[+] nothing to upload")


def upload_staging():
    print("[ ] uploading ...", flush=True)
    subprocess.run(["rsync", "-a", "--checksum", STAGING + "/",
                    RSYNC_TARGET], check=True)
    subprocess.run(["ssh", "root@medano",
                    "chmod -R +r /grave/img.emile.space/"], check=True)


def verify_upload(entries):
    """HEAD-probe every published file; interrupted rsyncs silently leave
    gaps otherwise.  Returns the missing filenames."""
    names = []
    for e in entries:
        names += e["files"] + [dither_file(e), card_file(e)]
        if e.get("orig"):
            names.append(e["orig"])

    def head(f):
        req = urllib.request.Request(f"{URL_BASE}/{f}", method="HEAD")
        try:
            urllib.request.urlopen(req, timeout=20)
            return None
        except Exception:
            return f

    with concurrent.futures.ThreadPoolExecutor(8) as pool:
        missing = [f for f in pool.map(head, names) if f]
    return sorted(set(missing))


def insert_gallery(page, slug, lines):
    """Insert/replace the album's gallery block in input/<page>.md
    between <!-- photos:slug --> markers; appended at the end when no
    markers exist yet."""
    name = page if page.endswith(".md") else page + ".md"
    path = os.path.join(ROOT, "input", name)
    if not os.path.exists(path):
        sys.exit(f"[!] page not found: {os.path.relpath(path)}")
    open_m, close_m = f"<!-- photos:{slug} -->", f"<!-- /photos:{slug} -->"
    block = open_m + "\n\n" + "\n\n".join(lines) + "\n\n" + close_m
    text = open(path).read()
    if open_m in text and close_m in text:
        text = (text.split(open_m, 1)[0] + block
                + text.split(close_m, 1)[1])
        verb = "replaced"
    elif open_m in text or close_m in text:
        sys.exit(f"[!] unbalanced photos markers in {path}")
    else:
        text = text.rstrip() + "\n\n" + block + "\n"
        verb = "appended"
    with open(path, "w") as f:
        f.write(text)
    print(f"[+] {verb} {len(lines)}-photo gallery in "
          f"{os.path.relpath(path)}")


def list_albums_status(manifest, base, key):
    """--albums: every Immich album, how many of its assets are in the
    manifest, and which pages reference it (matched via stem names)."""
    published = {}
    for e in manifest.values():
        a = e.get("album")
        if a:
            published[a] = published.get(a, 0) + 1
    stems_by_album = {}
    for e in manifest.values():
        stems_by_album.setdefault(e.get("album"), set()).add(stem_of(e))
    album_pages = {}
    input_dir = os.path.join(ROOT, "input")
    for name in sorted(os.listdir(input_dir)):
        if not name.endswith(".md"):
            continue
        text = open(os.path.join(input_dir, name)).read()
        for album, stems in stems_by_album.items():
            if any(s in text for s in stems):
                album_pages.setdefault(album, []).append(name[:-3])
    for al in sorted(api(base, key, "/albums"),
                     key=lambda a: a["albumName"].lower()):
        slug = album_slug(al["albumName"])
        n_pub = published.get(slug, 0)
        pages = ", ".join(album_pages.get(slug, [])) or "-"
        print(f"{al['albumName']!r} ({al.get('assetCount', '?')} assets): "
              f"{n_pub} published, pages: {pages}")
    if not published:
        print("(manifest empty)")

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
STAGING = os.path.join(ROOT, "img.emile.space")          # gitignored
# originals published as '<stem>-orig.<ext>' for the [full-res: n MB]
# links; kept local only until flushed to grave (they are huge)
ORIG_STAGING = os.path.join(ROOT, "img-origs")           # gitignored
MANIFEST = os.path.join(ROOT, "published-photos.json")   # committed
REMOTE_MIGRATE = os.path.join(ROOT, "img-migrate-remote.sh")
URL_BASE = "https://img.emile.space"
# naraj serves /var/www/img.emile.space via NFS from this grave dataset
RSYNC_TARGET = "root@medano:/grave/img.emile.space/"
DEFAULT_WIDTHS = "800,1600,2400"
QUALITY = 82
HASH_LEN = 12
CARD_WIDTH = 200  # max width of the undithered card-grid preview


def album_slug(name):
    """'2022-12 Tenerife El Médano' -> '2022-12-tenerife-el-medano'
    (ASCII-folded so filenames stay boring)."""
    import unicodedata
    s = unicodedata.normalize("NFKD", name).encode("ascii", "ignore").decode()
    return re.sub(r"-+", "-", re.sub(r"[^a-z0-9]+", "-", s.lower())).strip("-")


def load_api_key():
    """IMMICH_API_KEY env, then the gitignored ./.env file, then the
    agenix secret ops/secrets/immich_api_key.age in this repo
    (decrypted via `age` with the default ssh identity)."""
    key = os.environ.get("IMMICH_API_KEY")
    if key:
        return key
    env_file = os.path.join(ROOT, ".env")
    if os.path.exists(env_file):
        for line in open(env_file):
            if line.startswith("IMMICH_API_KEY="):
                return line.split("=", 1)[1].strip()
    age_file = os.path.normpath(os.path.join(
        ROOT, "..", "..", "..", "ops", "secrets", "immich_api_key.age"))
    if os.path.exists(age_file):
        for ident in ("~/.ssh/id_ed25519", "~/.ssh/id_rsa"):
            ident = os.path.expanduser(ident)
            if not os.path.exists(ident):
                continue
            try:
                r = subprocess.run(
                    ["age", "-d", "-i", ident, age_file],
                    capture_output=True, text=True, timeout=60)
            except Exception:
                continue
            if r.returncode == 0 and r.stdout.strip():
                return r.stdout.strip()
    return None


def api(base, key, path, raw=False):
    req = urllib.request.Request(f"{base}/api{path}",
                                 headers={"x-api-key": key})
    with urllib.request.urlopen(req, timeout=300) as r:
        return r.read() if raw else json.load(r)


def api_post(base, key, path, body):
    req = urllib.request.Request(
        f"{base}/api{path}", data=json.dumps(body).encode(),
        headers={"x-api-key": key, "content-type": "application/json"})
    with urllib.request.urlopen(req, timeout=300) as r:
        return json.load(r)


def list_album_assets(base, key, album_id):
    """Immich >= 3.x: album detail no longer embeds its assets; go through
    the metadata search API with an albumIds filter."""
    items, page = [], 1
    while page:
        d = api_post(base, key, "/search/metadata",
                     {"albumIds": [album_id], "page": page, "size": 250})
        a = d["assets"]
        items += a["items"]
        # nextPage comes back as a STRING ("2"); feeding it back verbatim
        # is rejected as "page must be a number" (400)
        nxt = a.get("nextPage")
        page = int(nxt) if nxt else None
    # deterministic, chronological gallery order
    items.sort(key=lambda x: (x.get("fileCreatedAt") or "", x["id"]))
    return items


def find_album(base, key, query):
    albums = api(base, key, "/albums")
    q = query.lower()
    exact = [a for a in albums if a["albumName"].lower() == q]
    if not exact:
        exact = [a for a in albums if a["id"].startswith(query)]
    if not exact:
        exact = [a for a in albums if q in a["albumName"].lower()]
    if len(exact) != 1:
        names = "\n".join(f'  {a["albumName"]}  ({a["id"][:8]})'
                          for a in albums)
        sys.exit(f"[!] album '{query}' matched {len(exact)} of:\n{names}")
    return exact[0]


def stem_of(entry):
    """'<slug>-<sha>-1600w.webp' -> '<slug>-<sha>'"""
    return re.sub(r"-\d+w\.webp$", "", entry["main"])


def dither_file(entry):
    return f"{stem_of(entry)}-dither.webp"


def card_file(entry):
    return f"{stem_of(entry)}-card.webp"


def ensure_entry(entry):
    """Backfill fields for manifest entries written by older script
    versions: dims from the local main file, dither from the smallest
    derivative. Purely local, no downloads. Returns True if usable."""
    main_path = os.path.join(STAGING, entry["main"])
    if "dims" not in entry or "exif" not in entry:
        with Image.open(main_path) as im:
            if "dims" not in entry:
                if not os.path.exists(main_path):
                    return False
                entry["dims"] = list(im.size)
            if "exif" not in entry:
                entry["exif"] = read_exif(im)
    dither_path = os.path.join(STAGING, dither_file(entry))
    if not os.path.exists(dither_path) and os.path.exists(main_path):
        write_dither(STAGING, entry["files"][0], dither_file(entry))
    card_path = os.path.join(STAGING, card_file(entry))
    if not os.path.exists(card_path) and os.path.exists(main_path):
        write_card(STAGING, entry["files"][0], card_file(entry))
    return os.path.exists(main_path)


def has_exif(path):
    try:
        with Image.open(path) as im:
            return bool(im.getexif())
    except Exception:
        return False


def migrate_names(manifest, base, key, widths, main_width):
    """--migrate-names (2026-11): move published files from the bare
    '<sha>-<w>w.webp' scheme to '<album-slug>-<sha>-<w>w.webp', backfill
    entry['album'] via the Immich API, re-encode derivatives whose files
    predate metadata preservation, and print scp/mv commands for medano."""
    assets = {}
    for entry in manifest.values():
        if "album" not in entry:
            if not assets:
                print("[ ] resolving albums via api ...", flush=True)
                for al in api(base, key, "/albums"):
                    slug = album_slug(al["albumName"])
                    for x in list_album_assets(base, key, al["id"]):
                        assets.setdefault(x["id"], slug)
            # filled below per-entry via the manifest asset id
    renamed, reencoded, remote = [], [], []
    stem_renames = {}  # old bare stem -> new '<album>-<sha>' stem
    for aid, entry in manifest.items():
        entry.setdefault("album",
                         assets.get(aid, "unknown") if assets else None)
        if not entry.get("album"):
            entry["album"] = "unknown"
        old_main = entry["main"]
        if not old_main.startswith(entry["album"] + "-"):
            old_stem = re.sub(r"-\d+w\.webp$", "", old_main)
            stem = f"{entry['album']}-{entry['sha']}"
            stem_renames[old_stem] = stem
            sizes = [re.search(r"-(\d+)w\.webp$", f).group(1)
                     for f in entry["files"]]
            entry["files"] = [f"{stem}-{s}w.webp" for s in sizes]
            entry["main"] = (f"{stem}-{main_width}w.webp"
                             if f"{stem}-{main_width}w.webp" in entry["files"]
                             else entry["files"][-1])
            old_names = [f"{entry['sha']}-{s}w.webp" for s in sizes] \
                + [f"{entry['sha']}-dither.webp",
                   f"{entry['sha']}-card.webp"]
            new_names = entry["files"] + [f"{stem}-dither.webp",
                                          f"{stem}-card.webp"]
            for o, n in zip(old_names, new_names):
                op, np_ = (os.path.join(STAGING, o),
                           os.path.join(STAGING, n))
                if os.path.exists(op):
                    os.rename(op, np_)
                    renamed.append(o)
                remote.append(f"[ ! -e {shq(o)} ] || mv -- {shq(o)} {shq(n)}")
            # originals follow the same scheme; they may not exist yet
            # for entries still awaiting --fetch-originals
            if entry.get("orig"):
                ext = os.path.splitext(entry["orig"])[1]
                o, n = (f"{old_stem}-orig{ext}", f"{stem}-orig{ext}")
                if os.path.exists(os.path.join(ORIG_STAGING, o)):
                    os.rename(os.path.join(ORIG_STAGING, o),
                              os.path.join(ORIG_STAGING, n))
                remote.append(f"[ ! -e {shq(o)} ] || mv -- {shq(o)} {shq(n)}")
                entry["orig"] = n
            ensure_entry(entry)
        main_path = os.path.join(STAGING, entry["main"])
        if os.path.exists(main_path) and not has_exif(main_path):
            for f in entry["files"]:  # publish() skips existing files
                p = os.path.join(STAGING, f)
                if os.path.exists(p):
                    os.remove(p)
            sha, files, main, dim, exif, orig = publish(
                {"id": aid, "originalFileName": entry["name"]},
                base, key, widths, main_width, entry["album"])
            entry.update(files=files, main=main, dims=list(dim), exif=exif,
                         orig=orig[0], orig_bytes=orig[1])
            reencoded.append(main)
    # pages reference the published urls: rewrite every renamed stem
    if stem_renames:
        n_pages = 0
        input_dir = os.path.join(ROOT, "input")
        for name in os.listdir(input_dir):
            if not name.endswith(".md"):
                continue
            path = os.path.join(input_dir, name)
            text = open(path).read()
            for old_stem, stem in stem_renames.items():
                text = text.replace(f"/{old_stem}-", f"/{stem}-")
            with open(path, "w") as f:
                f.write(text)
            n_pages += 1
        print(f"[+] rewrote {len(stem_renames)} stems across "
              f"{n_pages} pages")
    with open(MANIFEST, "w") as f:
        json.dump(manifest, f, indent=2, sort_keys=True)
    print(f"[+] renamed {len(renamed)} files, re-encoded "
          f"{len(reencoded)} with metadata")
    if remote:
        with open(REMOTE_MIGRATE, "w") as f:
            f.write("set -e\ncd /grave/img.emile.space\n"
                    + "\n".join(remote) + "\n")
        print(f"[+] wrote {REMOTE_MIGRATE} - review, then: "
              f"ssh root@medano bash -s < {REMOTE_MIGRATE}")


def redither(manifest):
    """--redither: regenerate every -dither.webp from the smallest local
    derivative (covers manifest entries plus staging orphans)."""
    # smallest derivative per stem, from the staging dir itself so orphans
    # (derivatives without a manifest entry) are covered too
    smallest = {}  # stem -> (width, file)
    for f in os.listdir(STAGING):
        m = re.match(r"(.+)-(\d+)w\.webp$", f)
        if m and (m.group(1) not in smallest
                  or int(m.group(2)) < smallest[m.group(1)][0]):
            smallest[m.group(1)] = (int(m.group(2)), f)
    for entry in manifest.values():  # stems with dither but no derivative
        smallest.setdefault(stem_of(entry), None)
    n = 0
    for stem, sized in sorted(smallest.items()):
        if sized is None:
            continue  # nothing local to dither from
        write_dither(STAGING, sized[1], f"{stem}-dither.webp")
        n += 1
    print(f"[+] regenerated {n} dither files "
          f"({len(smallest) - n} without local derivative)")


def shq(s):
    return "'" + s.replace("'", "'\\''") + "'"


def fetch_originals(manifest, base, key):
    """--fetch-originals: one-off download+publish of the untouched
    originals for entries predating the [full-res] feature."""
    pending = [(aid, e) for aid, e in manifest.items() if "orig" not in e]
    print(f"[ ] {len(pending)} originals to fetch")
    for i, (aid, e) in enumerate(pending, 1):
        data = api(base, key, f"/assets/{aid}/original", raw=True)
        name, n = store_original(stem_of(e), e["name"], data)
        e["orig"], e["orig_bytes"] = name, n
        if i % 25 == 0:
            flush_originals()
            with open(MANIFEST, "w") as f:
                json.dump(manifest, f, indent=2, sort_keys=True)
            print(f"[ ] {i}/{len(pending)}", flush=True)
    flush_originals()
    with open(MANIFEST, "w") as f:
        json.dump(manifest, f, indent=2, sort_keys=True)
    print(f"[+] {len(pending)} originals published")


def recover_origs(manifest):
    """--recover-origs: entries whose orig upload succeeded but whose
    manifest update was lost (e.g. clobbered by a concurrent publish)
    re-derive their 'orig'/'orig_bytes' from HEAD probes, no downloads."""
    pending = [(aid, e) for aid, e in manifest.items() if not e.get("orig")]
    print(f"[ ] probing {len(pending)} orig urls ...", flush=True)
    n = 0
    for aid, e in pending:
        ext = (os.path.splitext(e["name"])[1] or ".jpg").lower()
        name = f"{stem_of(e)}-orig{ext}"
        req = urllib.request.Request(f"{URL_BASE}/{name}", method="HEAD")
        try:
            with urllib.request.urlopen(req, timeout=15) as r:
                e["orig"], e["orig_bytes"] = \
                    name, int(r.headers["content-length"])
                n += 1
        except Exception:
            pass
        if n and n % 100 == 0:
            print(f"[ ] {n} recovered", flush=True)
    with open(MANIFEST, "w") as f:
        json.dump(manifest, f, indent=2, sort_keys=True)
    print(f"[+] recovered {n}/{len(pending)} orig entries")


def reencode_metadata(manifest):
    """--reencode-metadata: derivatives from before metadata preservation
    get their original's EXIF/ICC/XMP written back (one re-encode pass,
    no resize), and entry['exif'] populated from the original.  Entries
    whose derivative already has metadata are only backfilled."""
    pending, skipped = [], 0
    for aid, e in manifest.items():
        main_path = os.path.join(STAGING, e["main"])
        if not os.path.exists(main_path):
            continue
        if has_exif(main_path):
            if "exif" not in e:
                e["exif"] = read_exif_file(main_path)
            continue
        if e.get("orig"):
            pending.append((aid, e))
    print(f"[ ] {len(pending)} assets to re-encode with metadata",
          flush=True)
    for i, (aid, e) in enumerate(pending, 1):
        try:
            with urllib.request.urlopen(f"{URL_BASE}/{e['orig']}",
                                        timeout=300) as r:
                data = r.read()
            raw = Image.open(io.BytesIO(data))
        except Exception as ex:
            print(f"[!] {e['orig']}: {ex}", flush=True)
            skipped += 1
            continue
        exif = raw.getexif()
        exif[274] = 1  # Orientation: already baked into the derivatives
        meta = {"exif": exif.tobytes()}
        for k in ("icc_profile", "xmp"):
            if raw.info.get(k):
                meta[k] = raw.info[k]
        e["exif"] = read_exif(raw)
        for f in e["files"]:
            p = os.path.join(STAGING, f)
            if not os.path.exists(p):
                continue
            with Image.open(p) as im:
                if im.mode not in ("RGB", "L"):
                    im = im.convert("RGB")
                im.save(p, "WEBP", quality=QUALITY, method=6, **meta)
        if i % 25 == 0:
            with open(MANIFEST, "w") as f:
                json.dump(manifest, f, indent=2, sort_keys=True)
            print(f"[ ] {i}/{len(pending)}", flush=True)
    with open(MANIFEST, "w") as f:
        json.dump(manifest, f, indent=2, sort_keys=True)
    print(f"[+] re-encoded {len(pending) - skipped}, "
          f"{skipped} without reachable original")
    print("[ ] uploading derivatives ...", flush=True)
    subprocess.run(["rsync", "-a", "--checksum", "--include=*w.webp",
                    "--exclude=*", STAGING + "/", RSYNC_TARGET],
                   check=True)
    subprocess.run(["ssh", "root@medano",
                    "chmod -R +r /grave/img.emile.space/"], check=True)
    print("[+] uploaded")


def fix_staging(manifest):
    """--fix-staging: run ensure_entry over the whole manifest (covers all
    albums), no Immich access."""
    n = 0
    for entry in manifest.values():
        if ensure_entry(entry):
            n += 1
    with open(MANIFEST, "w") as f:
        json.dump(manifest, f, indent=2, sort_keys=True)
    print(f"[+] ensured dims/dither/card for {n} assets")


def store_original(stem, orig_filename, data):
    """Stage the untouched original for the [full-res] link; flushed to
    grave by flush_originals()."""
    os.makedirs(ORIG_STAGING, exist_ok=True)
    ext = (os.path.splitext(orig_filename)[1] or ".jpg").lower()
    name = f"{stem}-orig{ext}"
    with open(os.path.join(ORIG_STAGING, name), "wb") as f:
        f.write(data)
    return name, len(data)


def flush_originals():
    """rsync staged originals, then delete only the snapshotted batch.
    Files added by a concurrently running publish after the snapshot are
    left for the next flush (and must never be deleted un-uploaded)."""
    if not os.path.isdir(ORIG_STAGING):
        return
    batch = os.listdir(ORIG_STAGING)
    if not batch:
        return
    print("[ ] uploading originals ...", flush=True)
    subprocess.run(["rsync", "-a", ORIG_STAGING + "/", RSYNC_TARGET],
                   check=True)
    for f in batch:
        try:
            os.remove(os.path.join(ORIG_STAGING, f))
        except OSError:
            pass


def publish(asset, base, key, widths, main_width, slug):
    """Returns (sha_hex, made, main, main_dims, exif, orig) for one asset.
    Files are named <album-slug>-<sha>-<width>w.webp (+ '-dither.webp'),
    and the 'real' derivatives keep the original's EXIF/XMP/ICC metadata."""
    data = api(base, key, f"/assets/{asset['id']}/original", raw=True)
    sha = hashlib.sha1(data).hexdigest()[:HASH_LEN]
    stem = f"{slug}-{sha}"
    raw = Image.open(io.BytesIO(data))
    # metadata to preserve (orientation is baked in by exif_transpose
    # below, so the tag must be reset to '1' to not apply twice)
    exif = raw.getexif()
    exif[274] = 1  # Orientation: normal
    meta = {"exif": exif.tobytes()}
    for key_ in ("icc_profile", "xmp"):
        if raw.info.get(key_):
            meta[key_] = raw.info[key_]
    img = ImageOps.exif_transpose(raw)
    if img.mode not in ("RGB", "L"):
        img = img.convert("RGB")
    made, dims = [], {}
    for w in widths:
        if w >= img.width and made:
            continue  # never upscale; the first width keeps originals
        w = min(w, img.width)
        h = round(img.height * w / img.width)
        out = f"{stem}-{w}w.webp"
        path = os.path.join(STAGING, out)
        if not os.path.exists(path):
            resized = img if w == img.width else img.resize(
                (w, h), Image.LANCZOS)
            resized.save(path, "WEBP", quality=QUALITY, method=6, **meta)
        made.append(out)
        dims[out] = (w, h)
    main = (next((f for f in made
                  if f.startswith(f"{stem}-{main_width}w")), made[-1]))
    dither = f"{stem}-dither.webp"
    if not os.path.exists(os.path.join(STAGING, dither)):
        write_dither(STAGING, made[0], dither)
    card = f"{stem}-card.webp"
    if not os.path.exists(os.path.join(STAGING, card)):
        write_card(STAGING, made[0], card)
    exif = read_exif(raw)
    orig = store_original(stem, asset["originalFileName"], data)
    return sha, made, main, dims[main], exif, orig


def main():
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument("album", nargs="?", help="album name, name fragment or id")
    p.add_argument("--fix-staging", action="store_true",
                   help="only backfill dims/dither files from the local "
                        "staging dir for all known assets (no Immich)")
    p.add_argument("--redither", action="store_true",
                   help="regenerate all -dither.webp previews in the "
                        "staging dir from the smallest derivatives "
                        "(no Immich, no upload)")
    p.add_argument("--migrate-names", action="store_true",
                   help="one-off: rename all files to '<album>-<sha>-*', "
                        "re-encode derivatives lacking EXIF metadata")
    p.add_argument("--fetch-originals", action="store_true",
                   help="one-off: publish '<stem>-orig.<ext>' originals "
                        "for all entries missing one")
    p.add_argument("--recover-origs", action="store_true",
                   help="re-derive 'orig' manifest fields from HEAD probes "
                        "(after a lost manifest update; no downloads)")
    p.add_argument("--reencode-metadata", action="store_true",
                   help="re-encode derivatives lacking EXIF metadata from "
                        "their published originals (one-off)")
    p.add_argument("--format", choices=("dimg", "markdown", "html"),
                   default="dimg",
                   help="snippet output format (default dimg)")
    p.add_argument("--widths", default=DEFAULT_WIDTHS,
                   help=f"comma-separated target widths "
                        f"(default {DEFAULT_WIDTHS})")
    p.add_argument("--main-width", type=int, default=1600,
                   help="width used as the 'real' hover image (default 1600)")
    p.add_argument("--no-upload", action="store_true",
                   help="only build derivatives + snippets, skip rsync")
    p.add_argument("--force", action="store_true",
                   help="re-process assets already in the manifest")
    p.add_argument("--sync-annotations", action="store_true",
                   help="re-pull Immich tags/description into the manifest "
                        "and re-embed XMP (all entries; the album argument "
                        "restricts to one album), re-upload on changes")
    p.add_argument("--albums", action="store_true",
                   help="list Immich albums with publish state and the "
                        "pages referencing them, then exit")
    p.add_argument("--page", metavar="PAGE",
                   help="insert/replace the album's gallery between "
                        "<!-- photos:slug --> markers in input/PAGE.md "
                        "instead of printing snippets")
    a = p.parse_args()

    key = load_api_key()
    widths = sorted(int(w) for w in a.widths.split(","))
    os.makedirs(STAGING, exist_ok=True)

    manifest = {}
    if os.path.exists(MANIFEST):
        manifest = json.load(open(MANIFEST))

    if a.fix_staging:
        fix_staging(manifest)
        return

    if a.redither:
        redither(manifest)
        return

    if a.migrate_names:
        if not key:
            sys.exit("[!] --migrate-names needs IMMICH_API_KEY")
        migrate_names(manifest,
                      os.environ.get("IMMICH_URL",
                                     "https://photo.emile.space"),
                      key, widths, a.main_width)
        return

    if a.recover_origs:
        recover_origs(manifest)
        return

    if a.reencode_metadata:
        reencode_metadata(manifest)
        return

    if a.fetch_originals:
        if not key:
            sys.exit("[!] --fetch-originals needs IMMICH_API_KEY")
        fetch_originals(manifest,
                        os.environ.get("IMMICH_URL",
                                       "https://photo.emile.space"),
                        key)
        return

    if not (a.album or a.sync_annotations or a.albums):
        p.error("album is required")
    if not key:
        sys.exit("[!] set IMMICH_API_KEY (photo.emile.space -> "
                 "Administration -> API Keys)")
    base = os.environ.get("IMMICH_URL", "https://photo.emile.space")

    if a.albums:
        list_albums_status(manifest, base, key)
        return

    if a.sync_annotations:
        slug = None
        if a.album:
            slug = album_slug(find_album(base, key, a.album)["albumName"])
        sync_annotations(manifest, base, key, slug)
        return

    album = find_album(base, key, a.album)
    assets = [x for x in list_album_assets(base, key, album["id"])
              if x["type"] == "IMAGE"]
    print(f"[ ] album '{album['albumName']}': {len(assets)} images, "
          f"{len(manifest)} previously published")

    order = []
    for i, asset in enumerate(assets, 1):
        entry = manifest.get(asset["id"])
        if a.force or not entry or entry.get("widths") != widths:
            print(f"[ ] {i}/{len(assets)} {asset['originalFileName']}",
                  flush=True)
            slug = album_slug(album["albumName"])
            sha, files, main, dim, exif, orig = publish(
                asset, base, key, widths, a.main_width, slug)
            entry = {"sha": sha, "files": files, "main": main,
                     "name": asset["originalFileName"], "widths": widths,
                     "dims": list(dim), "album": slug, "exif": exif,
                     "orig": orig[0], "orig_bytes": orig[1]}
            try:  # annotations are best-effort, never block a publish
                tags, desc = asset_annotations(base, key, asset["id"])
                if tags:
                    entry["tags"] = tags
                if desc:
                    entry["desc"] = desc
            except Exception as ex:
                print(f"[!] {asset['originalFileName']}: annotations: "
                      f"{ex}", file=sys.stderr)
            entry_embed_xmp(entry)  # orig is still staged locally here
            manifest[asset["id"]] = entry
            with open(MANIFEST, "w") as f:
                json.dump(manifest, f, indent=2, sort_keys=True)
            if i % 25 == 0:
                flush_originals()  # bound local disk use
        else:
            ensure_entry(entry)  # backfill dims/dither from local staging
        order.append(entry)
    print(f"[+] {len(order)} derivatives ready in {os.path.relpath(STAGING)}")

    if not a.no_upload:
        upload_staging()
        flush_originals()
        print("[+] uploaded to " + URL_BASE)
        print("[ ] verifying published urls ...", flush=True)
        missing = verify_upload(order)
        if missing:
            print(f"[!] {len(missing)} files missing after upload, "
                  "retrying rsync once ...", flush=True)
            upload_staging()
            missing = verify_upload(order)
        if missing:
            print(f"[!] still missing {len(missing)} files, e.g.:",
                  file=sys.stderr)
            for f in missing[:10]:
                print(f"      {URL_BASE}/{f}", file=sys.stderr)
        else:
            print("[+] all urls verified")

    print()
    lines, skipped = [], 0
    for e in order:
        if not ensure_entry(e):  # dither/dims unavailable: skip snippet
            skipped += 1
            continue
        alt = (e.get("desc")
               or os.path.splitext(e["name"])[0]).replace('"', "'")
        dither = dither_file(e)
        w, h = e["dims"]
        if a.format == "dimg":
            # [full-res] links are emitted by the build itself (render.ml
            # reads published-photos.json), not pasted into the markdown
            lines.append(f'(dimg {URL_BASE}/{dither} '
                         f'{URL_BASE}/{e["main"]} {w} {h} "{alt}")')
        elif a.format == "html":
            srcset = ", ".join(
                f"{URL_BASE}/{f} {f.split('-')[-1].removesuffix('.webp')}"
                for f in e["files"])
            lines.append(
                f'<img src="{URL_BASE}/{e["main"]}" srcset="{srcset}" '
                f'sizes="(max-width: 768px) 100vw, 768px" '
                f'loading="lazy" alt="{alt}">')
        else:
            lines.append(f"![{alt}]({URL_BASE}/{e['main']})")
    if a.page:
        insert_gallery(a.page, album_slug(album["albumName"]), lines)
    else:
        print("\n\n".join(lines))
    if skipped:
        print(f"[!] {skipped} asset(s) skipped: no local derivatives",
              file=sys.stderr)


if __name__ == "__main__":
    main()
