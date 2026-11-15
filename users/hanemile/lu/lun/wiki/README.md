# Lun Wiki - Static Site Generator

A complete static site generator written **entirely in Lun**, a minimal Lisp dialect!

## 🌟 Features

- **100% Pure Lun**: Every line of the generator is written in Lun
- **Blog System**: Automatically generates blog post listings
- **Project Showcase**: Creates beautiful project cards
- **Static Pages**: Supports about, events, and custom pages
- **Beautiful Design**: Modern, responsive CSS with gradient backgrounds
- **Zero Dependencies**: No external libraries or frameworks

## 📁 Structure

```
wiki/
├── content/          # Source content
│   ├── blog/        # Blog posts in markdown
│   ├── projects/    # Project descriptions
│   ├── about.md     # About page
│   └── events.md    # Events page
├── output/          # Generated HTML files
└── wiki.lun         # The generator (pure Lun!)
```

## 🚀 Usage

Generate the wiki:

```bash
cargo run wiki/wiki.lun
```

Then open `wiki/output/index.html` in your browser!

## 🎨 What Makes This Special

This wiki demonstrates:

1. **Functional Programming**: Pure functions, recursion, higher-order functions
2. **String Manipulation**: Using Lun's string functions to build HTML
3. **File I/O**: Reading markdown files and writing HTML
4. **List Processing**: Mapping over file lists to generate pages

## 📝 Adding Content

### Blog Posts

Create a new markdown file in `wiki/content/blog/`:

```markdown
---
title: My New Post
date: 2025-12-10
author: Your Name
---

# My New Post

Content goes here...
```

### Projects

Create a new markdown file in `wiki/content/projects/`:

```markdown
---
title: My Project
status: active
tech: Lun, Rust
---

# My Project

Description...
```

Then regenerate the wiki!

## 🔧 Technical Details

The generator uses these Lun features:

- `list-dir`: List files in a directory
- `read-file`: Read file contents
- `write-file`: Write generated HTML
- `str-concat`, `str-replace`: String manipulation
- Recursive list processing
- Lambda functions and closures

## 🎯 Implementation Highlights

- **No multiple `define` in lambdas**: Lun returns the value of the first `define`, so we structure code accordingly
- **Recursive list processing**: Using `map`-style recursion to process file lists
- **Functional composition**: Building complex HTML from simple functions

## 📚 Learn More

This wiki is part of the Lun language project, demonstrating real-world functional programming in a minimal Lisp dialect.

## 📄 License

MIT License - Feel free to use and modify!

---

**Generated with ❤️ using Lun**
