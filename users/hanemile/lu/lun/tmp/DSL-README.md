# Lun HTML/Content DSL

A comprehensive Domain-Specific Language (DSL) for generating HTML using pure Lun code. Write web pages using S-expressions and functional composition!

## 🌟 Features

- **HTML as S-Expressions**: Write `(h1 "Title")` instead of `<h1>Title</h1>`
- **Functional Composition**: Build complex pages from simple, reusable functions
- **Type-Safe**: Catch errors at generation time, not runtime
- **Zero Dependencies**: Pure Lun implementation
- **Multiple Abstraction Levels**: From low-level HTML tags to high-level page templates

## 📚 DSL Layers

### 1. HTML DSL (`html-dsl.lun`)

Low-level HTML tag generation:

```lisp
(require "html-dsl.lun")

;; Basic tags
(h1 "My Heading")                    ; => <h1>My Heading</h1>
(p "Paragraph text")                 ; => <p>Paragraph text</p>
(div (h2 "Title") (p "Content"))     ; => <div><h2>Title</h2><p>Content</p></div>

;; Lists
(ul (li "Item 1") (li "Item 2"))     ; => <ul><li>Item 1</li><li>Item 2</li></ul>

;; Links
(a "https://example.com" "Click me") ; => <a href='https://example.com'>Click me</a>

;; Complete documents
(html-doc "Page Title" 
  (style "body{margin:0}")
  (h1 "Content"))
```

### 2. Content DSL (`content-dsl.lun`)

Higher-level content creation:

```lisp
(require "content-dsl.lun")

;; Headings with levels
(heading 1 "Main Title")             ; => <h1>Main Title</h1>
(heading 2 "Subtitle")               ; => <h2>Subtitle</h2>

;; Inline formatting
(bold "Important")                   ; => <strong>Important</strong>
(italic "Emphasis")                  ; => <em>Emphasis</em>
(inline-code "code")                 ; => <code>code</code>

;; Lists
(unordered-list 
  (item "First")
  (item "Second"))

;; Blog posts
(blog-post "Title" "2025-12-09" "Author"
  (paragraph "Content here..."))

;; Simple pages with default styling
(simple-page "My Page"
  (str-concat
    (heading 1 "Welcome")
    (paragraph "Hello world!")))
```

### 3. Macro DSL (`macro-dsl.lun`)

Most natural, macro-style syntax:

```lisp
(require "macro-dsl.lun")

;; Define complete pages
(defpage "index" "Home Page" (lambda ()
  (str-concat
    (hero "Welcome" "To my site")
    (features
      (item "Feature 1")
      (item "Feature 2"))
    (section-with "About"
      (list
        (text "This is " (b "bold") " and " (i "italic"))
        (text "With a " (l "https://link.com" "link")))))))

;; Short aliases
(b "bold")        ; => <strong>bold</strong>
(i "italic")      ; => <em>italic</em>
(c "code")        ; => <code>code</code>
(l "url" "text")  ; => <a href='url'>text</a>
```

## 🚀 Quick Start

### Example 1: Simple Page

```lisp
(require "content-dsl.lun")

(define my-page
  (simple-page "My First Page"
    (str-concat
      (heading 1 "Hello Lun!")
      (paragraph "This is my first page using the DSL.")
      (unordered-list
        (item "Easy to use")
        (item "Functional")
        (item "Powerful")))))

(write-file "output.html" my-page)
```

### Example 2: Blog Post

```lisp
(require "content-dsl.lun")

(define post
  (blog-post
    "My First Post"
    "2025-12-09"
    "Your Name"
    (str-concat
      (paragraph "Welcome to my blog!")
      (heading 2 "What I'll Write About")
      (unordered-list
        (item "Functional programming")
        (item "Lisp")
        (item "Web development")))))

(write-file "post.html" (simple-page "Blog" post))
```

### Example 3: Complete Site with Macro DSL

```lisp
(require "macro-dsl.lun")

(defpage "index" "My Site" (lambda ()
  (str-concat
    ;; Hero section
    (hero "My Awesome Site" "Built with Lun")
    
    ;; Features
    (features
      (item (str-concat (b "Fast") " - Static HTML generation"))
      (item (str-concat (b "Simple") " - Pure Lun code"))
      (item (str-concat (b "Powerful") " - Full Lisp expressiveness")))
    
    ;; Content
    (section-with "About"
      (list
        (text "This site is built using the Lun Content DSL.")
        (text "Check out the " (l "https://github.com" "source code") "!")))
    
    ;; Blog
    (heading 2 "Latest Posts")
    (blog-post "First Post" "2025-12-09" "Me"
      (text "Welcome to my new site!")))))
```

## 📖 API Reference

### HTML DSL Functions

#### Structure
- `html(content...)` - HTML root element
- `head(content...)` - Head section
- `body(content...)` - Body section
- `title(text)` - Page title

#### Headings
- `h1(text)` through `h6(text)` - Heading levels 1-6

#### Text
- `p(text)` - Paragraph
- `strong(text)` - Bold text
- `em(text)` - Italic text
- `code(text)` - Inline code

#### Lists
- `ul(items...)` - Unordered list
- `ol(items...)` - Ordered list
- `li(text)` - List item

#### Links & Media
- `a(href, text)` - Link
- `img(src, alt)` - Image

#### Layout
- `div(content...)` - Division
- `section(content...)` - Section
- `article(content...)` - Article
- `nav(content...)` - Navigation
- `header(content...)` - Header
- `footer(content...)` - Footer

### Content DSL Functions

#### Content Creation
- `heading(level, text)` - Heading with level 1-6
- `paragraph(text)` - Paragraph
- `bold(text)` - Bold text
- `italic(text)` - Italic text
- `inline-code(text)` - Inline code
- `link(url, text)` - Hyperlink

#### Lists
- `item(text)` - List item
- `unordered-list(items...)` - UL with items
- `ordered-list(items...)` - OL with items

#### Templates
- `simple-page(title, content)` - Page with default styling
- `page(title, css, content)` - Custom page
- `blog-post(title, date, author, content)` - Blog post structure
- `card(title, content)` - Card component

### Macro DSL Functions

#### Page Definition
- `defpage(name, title, content-fn)` - Define and generate a page

#### Shortcuts
- `b(text)` - Bold (alias for `bold`)
- `i(text)` - Italic (alias for `italic`)
- `c(text)` - Code (alias for `inline-code`)
- `l(url, text)` - Link (alias for `link`)

#### Builders
- `hero(title, subtitle)` - Hero section
- `features(items...)` - Feature list
- `section-with(title, items)` - Section with heading
- `list-of(items...)` - Quick unordered list
- `text(parts...)` - Paragraph with inline formatting

## 🎯 Design Philosophy

1. **Composability**: Every function returns a string that can be combined with others
2. **Functional**: Pure functions, no side effects (except file I/O)
3. **Layered**: Multiple abstraction levels for different use cases
4. **Lispy**: Embrace S-expressions and functional composition
5. **Practical**: Designed for real-world static site generation

## 💡 Tips & Tricks

### Combining Multiple Elements

Use `str-concat` to combine elements:

```lisp
(str-concat
  (h1 "Title")
  (p "First paragraph")
  (p "Second paragraph"))
```

### Reusable Components

Define your own components:

```lisp
(define my-card (lambda (title text)
  (div
    (h3 title)
    (p text))))

(my-card "Card 1" "Content 1")
(my-card "Card 2" "Content 2")
```

### Dynamic Content

Use Lun's list processing:

```lisp
(define make-items (lambda (items)
  (if (empty? items)
      ""
      (str-concat
        (li (first items))
        (make-items (rest items))))))

(ul (make-items (list "A" "B" "C")))
```

## 📝 Examples

See the `example-*.lun` files for complete working examples:

- `example-page.lun` - Basic Content DSL usage
- `example-macro.lun` - Macro DSL with all features

## 🔧 Implementation Notes

- All HTML is generated as strings
- No runtime JavaScript required
- Output is pure static HTML
- Works with any Lun installation

## 📄 License

MIT License - Use freely!

---

**Built with ❤️ using Lun**
