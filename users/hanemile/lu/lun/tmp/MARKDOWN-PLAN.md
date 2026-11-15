# Markdown-as-Lun Implementation Plan

## Goal
Make markdown files valid Lun code through custom readtables and macros.

## Example Flow

### Input (markdown file - valid Lun):
```markdown
# Index

This is the index page

This is the main page: [emile.space](https://emile.space)
```

### After Reading (with custom readtable):
```lisp
(heading 1 "Index")
(par "This is the index page")
(par "This is the main page: " (link "emile.space" "https://emile.space"))
```

### After Macro Expansion:
```lisp
(h1 "Index")
(p "This is the index page")
(p "This is the main page: " (a "emile.space" :href "https://emile.space"))
```

### Final HTML:
```html
<h1>Index</h1>
<p>This is the index page</p>
<p>This is the main page <a href="https://emile.space">emile.space</a></p>
```

## Implementation Steps

### Phase 1: Macro System (Rust)
1. Add `Macro` type to `types.rs`
2. Add macro storage to `Environment`
3. Add `defmacro` special form to `eval.rs`
4. Add macro expansion phase before evaluation

### Phase 2: Readtable System (Rust)
1. Create `readtable.rs` module
2. Add readtable to lexer/parser
3. Implement custom read functions for markdown syntax
4. Add `#` as heading reader macro
5. Add `[text](url)` as link reader macro

### Phase 3: Markdown Readtable
1. Define markdown readtable with custom readers
2. Implement line-based parsing
3. Handle inline markdown (links, bold, italic)
4. Switch readtables at runtime

### Phase 4: Integration
1. Add `read-markdown` function
2. Update wiki to use markdown files
3. Create example markdown pages
4. Test end-to-end flow

## Current Status

- ✅ HTML DSL implemented
- ✅ Content DSL implemented  
- ⏳ Macro system - NOT IMPLEMENTED
- ⏳ Readtable system - NOT IMPLEMENTED
- ⏳ Markdown readtable - NOT IMPLEMENTED

## Simplified Approach (Interim Solution)

Since implementing full readtables and macros in Rust is complex, we can:

1. Create a **markdown preprocessor** in Lun
2. Parse markdown files into Lun S-expressions
3. Evaluate those S-expressions
4. This gives us 90% of the functionality

### Preprocessor Flow:
```
markdown file → parse to S-exprs → evaluate → HTML
```

This is what we'll implement first, then enhance later.

## Files to Create

1. `markdown-parser.lun` - Parse markdown to S-expressions
2. `markdown-macros.lun` - Define `heading`, `par`, `link` macros
3. `example.md.lun` - Example markdown-as-lun file
4. `test-markdown.lun` - Test the system

## Next Steps

1. ✅ Create markdown parser in Lun
2. ✅ Define markdown macros
3. Create example markdown file
4. Test end-to-end
5. (Future) Implement proper readtables in Rust
