# Macro and Readtable Implementation Plan

## Goal
Implement proper macros and readtables so markdown syntax becomes valid Lun.

## Architecture

```
Raw Markdown → [Readtable] → S-expressions → [Macro Expansion] → HTML DSL → [Evaluation] → HTML
```

### Example Flow:

**Input (markdown):**
```
# Index
This is text
[link](url)
```

**After Readtable:**
```lisp
(heading 1 "Index")
(par "This is text")
(par (link "link" "url"))
```

**After Macro Expansion:**
```lisp
(h1 "Index")
(p "This is text")
(p (a "url" "link"))
```

**After Evaluation:**
```html
<h1>Index</h1>
<p>This is text</p>
<p><a href="url">link</a></p>
```

## Implementation Steps

### Phase 1: Macro System (Rust)

1. **Add Macro type** (`src/types.rs`)
   ```rust
   pub enum Value {
       // ... existing variants
       Macro(Vec<String>, Box<AstNode>, Rc<RefCell<Environment>>),
   }
   ```

2. **Add defmacro** (`src/eval.rs`)
   ```rust
   fn eval_defmacro(list: &Vec<AstNode>, env: ...) -> Result<Value, LunError>
   ```

3. **Add macro expansion** (`src/eval.rs`)
   ```rust
   fn expand_macros(node: &AstNode, env: ...) -> Result<AstNode, LunError>
   ```

4. **Integrate into evaluation pipeline**
   - Expand macros before evaluation
   - Store macros in environment

### Phase 2: Readtable System (Rust)

1. **Create readtable module** (`src/readtable.rs`)
   ```rust
   pub struct Readtable {
       readers: HashMap<char, ReaderFn>,
   }
   ```

2. **Add markdown readers**
   - `#` → heading reader
   - `[` → link reader  
   - `-` → list item reader

3. **Integrate with lexer/parser**
   - Check readtable before standard parsing
   - Apply custom readers when found

### Phase 3: Markdown Readtable Definition

1. **Define in Lun** (`markdown-readtable.lun`)
   ```lisp
   (define-readtable markdown
     (#\# . read-heading)
     (#\[ . read-link)
     (#\- . read-list-item))
   ```

2. **Switch readtables**
   ```lisp
   (with-readtable markdown
     (load "content.md"))
   ```

## Current Status

- ❌ Macro system - NOT IMPLEMENTED
- ❌ Readtable system - NOT IMPLEMENTED  
- ✅ HTML DSL - COMPLETE
- ✅ Content DSL - COMPLETE

## Next Steps

1. Implement macro system in Rust
2. Implement readtable system in Rust
3. Define markdown readtable
4. Test end-to-end

This is the CORRECT approach - no string parsing, pure macro transformation!
