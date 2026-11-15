#!/usr/bin/env python3
"""Generate 5000 interlinked .md test files for benchmarking order."""

import os
import random
import sys

NUM_FILES = 5000
OUTPUT_DIR = sys.argv[1] if len(sys.argv) > 1 else "/tmp/order-bench/input"

TYPES = ["note", "event", "project", "person", "idea", "log", "recipe", "book", "paper"]
TAGS = ["math", "physics", "cs", "art", "music", "cooking", "travel", "ccc",
        "hacking", "crypto", "infra", "lisp", "rust", "nix", "linux",
        "networking", "radio", "electronics", "photography", "writing"]
LOCATIONS = ["Berlin", "Hamburg", "Leipzig", "Munich", "Zurich", "Vienna",
             "Amsterdam", "Paris", "London", "Tokyo", "NYC", "SF"]
VISIBILITIES = ["public", "public", "public", "internal", "internal", "private"]

WORDS = ["alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta",
         "iota", "kappa", "lambda", "mu", "nu", "xi", "omicron", "pi", "rho",
         "sigma", "tau", "upsilon", "phi", "chi", "psi", "omega", "quantum",
         "neural", "fractal", "entropy", "lattice", "manifold", "tensor",
         "kernel", "daemon", "socket", "packet", "cipher", "protocol",
         "voltage", "resonance", "spectrum", "orbit", "flux", "gradient"]


def random_date():
    year = random.randint(2015, 2026)
    month = random.randint(1, 12)
    day = random.randint(1, 28)
    return f"{year:04d}-{month:02d}-{day:02d}"


def make_id(i):
    return f"page-{i:05d}"


def make_title(i):
    w1 = WORDS[i % len(WORDS)]
    w2 = WORDS[(i * 7 + 3) % len(WORDS)]
    return f"{w1.capitalize()} {w2}"


def random_links(i, count=3):
    """Generate links to random other pages."""
    targets = set()
    for _ in range(count):
        t = random.randint(0, NUM_FILES - 1)
        if t != i:
            targets.add(t)
    return targets


def generate_file(i, all_ids):
    pid = make_id(i)
    title = make_title(i)
    typ = TYPES[i % len(TYPES)]
    vis = random.choice(VISIBILITIES)
    date = random_date()
    tags = ", ".join(random.sample(TAGS, random.randint(1, 4)))
    location = random.choice(LOCATIONS) if typ == "event" else ""

    # Generate links to other pages (2-5 links per page)
    link_targets = random_links(i, random.randint(2, 5))
    link_lines = []
    for t in link_targets:
        tid = make_id(t)
        ttitle = make_title(t)
        link_lines.append(f"Related: [{ttitle}](/{tid}/)")

    # Build frontmatter
    lines = ["---"]
    lines.append(f"title: {title}")
    lines.append(f"type: {typ}")
    lines.append(f"visibility: {vis}")
    lines.append(f"date: {date}")
    lines.append(f"tags: {tags}")
    if location:
        lines.append(f"location: {location}")
    lines.append("---")
    lines.append("")
    lines.append(f"# {title}")
    lines.append("")
    lines.append(f"This is page {i} of type {typ}.")
    lines.append("")
    lines.append("## Details")
    lines.append("")
    lines.append(f"Created on {date}. Tags: {tags}.")
    lines.append("")
    if link_lines:
        lines.append("## Links")
        lines.append("")
        lines.extend(link_lines)
        lines.append("")

    return "\n".join(lines) + "\n"


def generate_index(all_ids):
    """Generate a README.md with queries."""
    lines = [
        "---",
        "title: Home",
        "type: page",
        "visibility: public",
        "---",
        "",
        "# Bench Home",
        "",
        ":::toc",
        "",
        "## Recent Events",
        "",
        '(table :type "event" :columns (:title :date :location) :sort-desc :date)',
        "",
        "## All Projects",
        "",
        '(listing :type "project" :show (:title :date))',
        "",
        "## Timeline",
        "",
        "(timeline :show (:title :type :last-edited))",
        "",
    ]
    return "\n".join(lines) + "\n"


def main():
    os.makedirs(OUTPUT_DIR, exist_ok=True)

    all_ids = [make_id(i) for i in range(NUM_FILES)]

    # Generate all pages
    for i in range(NUM_FILES):
        path = os.path.join(OUTPUT_DIR, f"{make_id(i)}.md")
        with open(path, "w") as f:
            f.write(generate_file(i, all_ids))

    # Generate index
    path = os.path.join(OUTPUT_DIR, "README.md")
    with open(path, "w") as f:
        f.write(generate_index(all_ids))

    # Generate a minimal style.css
    style_path = os.path.join(OUTPUT_DIR, "style.css")
    with open(style_path, "w") as f:
        f.write("body { font-family: monospace; max-width: 80ch; margin: 0 auto; }\n")
        f.write("pre { white-space: pre-wrap; }\n")
        f.write(".query-table { border-collapse: collapse; }\n")
        f.write(".query-table th, .query-table td { border: 1px solid #ccc; padding: 0.25em; }\n")

    print(f"Generated {NUM_FILES + 1} files in {OUTPUT_DIR}")


if __name__ == "__main__":
    main()
