---
title: order
type: page
visibility: public
---

# order

Lisp-powered personal wiki. Files are `.md` that look like markdown but are valid Lisp.

## Build & Run

    nix-build -A order            # build binary
    nix-build -A build-site       # build wrapper script
    ./scripts/run.sh private      # import hefe3 + build + serve
    ./scripts/run.sh public --serve

## Test

    nix-build -A test-visibility  # visibility ACL test (checks all pages)
    nix-build -A test-bench       # benchmark (5000 pages)
    ./scripts/test-visibility.sh  # run directly

## Content

`.md` files with YAML frontmatter + markdown body. Lines starting with `(` are Lisp.

## Queries

    (table :type "vm" :columns (:title :ip :services) :limit 10)
    (listing :type "event" :sort-desc :date :show (:title :date))
    (group-by :type "vm" :by :network :show (:title :services))
    (table :where (types "device" "router") :columns (:title :location))
    (table :where (has "network" "default") :columns (:title :ip))
    (children "nas" :via "part-of" :show (:title :type))
    (graph "fiber-uplink")        (trace-path "nas" "router")
    (shelf-view "shelf-a" :cols 4 :rows 10)
    (all-types)                   (count-objects :type "vm")

## Features

- Incremental builds (~0.4s for 5000 pages), full rebuild ~1s
- Tiered visibility: `public`, `internal`, `private` with `(begin-visible "level")` sections
- Every property value auto-generates a page with backlinks (tags, types, locations, etc.)
- SVG topology graphs, trace paths, shelf views with box highlighting
- Client-side search, numbered TOC, code blocks, images
- Nix infrastructure import from hefe3 monorepo
- 4500+ automated visibility tests
