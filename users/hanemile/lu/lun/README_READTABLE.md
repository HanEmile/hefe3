# Readtable and Reader Macros

## Overview

The `lun` interpreter now supports a readtable system with reader macros, allowing you to extend the syntax and create custom parsing behaviors.

## Features Implemented

### 1. Readtable System
- A readtable maps characters to reader macro functions
- Reader macros are invoked during tokenization
- Default readtable includes `#` for reader macros

### 2. Reader Macros

#### `#include "filename.lun"`
Includes and evaluates another file. The file is loaded and executed in the current environment.

Example:
```lisp
#include "utils.lun"
```

#### `#require "filename.lun"`
Similar to `#include`, but only loads the file once. Subsequent `#require` calls for the same file are ignored.

Example:
```lisp
#require "common.lun"
#require "common.lun"  ;; This is ignored
```

#### `#markdown ... #endmarkdown`
Converts markdown content to lun code. Currently wraps the content in a string.

Example:
```lisp
#markdown
# Hello World
This is **markdown** content.
#endmarkdown
```

### 3. Multiple File Execution

You can now run multiple files from the command line:

```bash
cargo run file1.lun file2.lun file3.lun
```

Files are evaluated in order, and the environment is shared between them.

### 4. File Tracking

The environment tracks which files have been loaded to prevent duplicate loading with `#require`.

## Usage Examples

### Basic Include
```lisp
;; main.lun
#include "config.lun"
(+ 1 2 3)
```

### Wiki-style Structure
```lisp
;; page.lun
#require "header.lun"
#require "sidebar.lun"

#markdown
# Page Title
Content goes here.
#endmarkdown

#require "footer.lun"
```

### Multiple Files
```bash
# Run multiple files
cargo run init.lun main.lun cleanup.lun
```

## Implementation Details

- Reader macros are processed during tokenization
- Special tokens (`__INCLUDE__`, `__REQUIRE__`) are converted to AST nodes
- File loading is handled in the evaluator as special forms
- The environment tracks loaded files to prevent duplicates

## Future Enhancements

Potential additions:
- More reader macros (e.g., `#json`, `#yaml`)
- Custom readtable registration from lun code
- Better markdown-to-lun conversion
- Reader macro composition
- Syntax extensions for wiki features





