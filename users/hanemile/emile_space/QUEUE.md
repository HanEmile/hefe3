# publish queue — 2026-11-15, all done ~15:20

## done

- [x] `--fetch-originals` — all 663 originals published
- [x] `--migrate-names` (re-run) — 612 stems resolved via api, renamed
  locally, input/*.md urls rewritten, manifest saved
- [x] `ssh root@medano bash -s < img-migrate-remote.sh` — 3671 moves,
  0 bare-hash files left on the server
- [x] `--fix-staging` — dims/dither/card/exif backfilled for all 1889
  entries (5 legitimately EXIF-less assets, e.g. edits/screenshots)
- [x] `--redither` + rsync — all 1889 dithers regenerated in color
- [x] `./publish.sh` — site rebuilt and synced
- [x] verified live: color dithers + EXIF info lines
  (`[full-res: 11.3 MB] · 1/200s | ISO 50 | f/4.5`) on old and new
  pages; slugged orig urls (e.g. 2024-11-asia-…-orig.jpeg) resolve

## remaining notes

- bare-sha urls (https://img.emile.space/<sha>-…) are gone; any
  external deep links to them 404 now
- img-migrate-remote.sh kept for audit; safe to delete

# follow-ups — 2026-11-15, later that day

## done

- [x] lost-cards incident (~15:30): only 418/1072 `-card.webp` files of
  the 2025-09-asia album were actually on the server (interrupted
  upload), so bookthumb previews 404'd.  fixed via targeted
  `rsync --include='*-card.webp'` to medano + chmod, verified
  1072/1072 serve 200
- [x] annotation pipeline (opencode session): Immich tags/description
  -> manifest (`tags`/`desc`) -> XMP embedded into every published file
  (pure-python webp RIFF / jpeg APP1, no re-encode) -> info line on the
  site.  new flags: `--sync-annotations [album]`, `--albums`,
  `--page PAGE` (inserts `<!-- photos:slug -->`-marked gallery blocks
  into input/*.md).  post-upload url verification now guards the
  lost-cards failure mode.  api key lives in gitignored `./.env`
- [x] first real `--sync-annotations "2025-09-Asia"` (~15:50):
  8/1072 assets annotated (Big Buddha wiki link + 7 temple
  descriptions), xmp re-embedded + uploaded, verified live:
  exiftool on fetched url shows Description, rebuilt site shows the
  info lines on hong-kong-2025-11 + chiang-mai-2025-09

## open

- [ ] `./publish.sh` — deploy the rebuilt site (output/ has the
  annotation info lines; only rebuilt locally so far)

## known gaps / ideas

- `-orig.jpeg` files published before the annotation feature carry no
  XMP (only new albums get it at publish time)
- descriptions render on their own line below the info line
  (`p.photodesc`); urls in them are still plain text, could be made
  clickable
- img-migrate-remote.sh still safe to delete
