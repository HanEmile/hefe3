---
title: Building a Static Site Generator in Lun
date: 2025-12-03
author: Emile
tags: lun, programming, tutorial
---

# Building a Static Site Generator in Lun

Today I want to share how I built this entire wiki system using nothing but Lun code.

## The Architecture

The wiki generator follows a simple pipeline:

1. **Read** markdown files from the content directory
2. **Parse** frontmatter metadata (title, date, tags)
3. **Transform** markdown to HTML
4. **Generate** index pages (blog listing, project listing)
5. **Write** output HTML files

## Key Functions

The core of the system relies on these Lun functions:

- `list-dir` - Lists files in a directory
- `read-file` - Reads file contents
- `write-file` - Writes generated HTML
- `str-split`, `str-replace` - String manipulation

## Challenges

The main challenge was working within Lun's minimal feature set. I had to implement:
- String parsing for frontmatter
- Template system for HTML generation
- Recursive directory traversal

But that's what makes it fun!
