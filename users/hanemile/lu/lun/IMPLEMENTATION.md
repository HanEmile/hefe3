# Implementation Guide: What Can Be Moved to lun.lun

## Overview

The `lun` interpreter has a hybrid architecture:
- **Rust layer**: Handles system calls, I/O, and low-level operations
- **Lisp layer (`lun.lun`)**: Can implement higher-level functions and utilities

## What MUST Stay in Rust

These operations require system calls or external libraries and **cannot** be moved to `lun.lun`:

### Filesystem Operations
- `read-file` - Requires `std::fs::read_to_string`
- `write-file` - Requires `std::fs::write`
- `list-dir` - Requires `std::fs::read_dir`

### Network Operations
- `http-get` - Requires `ureq` HTTP client library
- `http-post` - Requires `ureq` HTTP client library

### String Operations (Primitives)
- `str-concat` - Efficient string concatenation
- `str-length` - String length calculation

## What CAN Be Moved to lun.lun

These are higher-level functions that can be implemented using the Rust primitives:

### Mathematical Functions
- `square`, `abs`, `max`, `min` - Already implemented in `lun.lun`
- `pow`, `sqrt`, `sin`, `cos` - Can be implemented using arithmetic
- Statistical functions - Can be built from arithmetic operations

### Utility Functions
- Wrapper functions around filesystem operations
- Data transformation functions
- Composition functions

### Example: What's Already in lun.lun

```lisp
;; Mathematical helpers (implemented in Lisp)
(define square (lambda (x) (* x x)))
(define abs (lambda (x) (if (< x 0) (- 0 x) x)))
(define max (lambda (a b) (if (> a b) a b)))
(define min (lambda (a b) (if (< a b) a b)))
```

## Current Limitations

1. **Function definitions work** - You can define functions using `lambda` or `(define name (lambda ...))`
2. **Conditionals work** - `if` special form is available
3. **Comparisons work** - `=`, `!=`, `<`, `>` operators are available
4. **String operations** - Basic string functions work, but string manipulation is limited

## Future Enhancements

To move more to `lun.lun`, we would need to add:
- More string manipulation functions (substring, replace, etc.)
- List operations (map, filter, reduce)
- Error handling mechanisms
- More control flow (while loops, etc.)

## Summary

**Rust provides**: System I/O, network, and primitive operations
**lun.lun provides**: Higher-level abstractions, mathematical utilities, and composed functions

The architecture allows you to build a rich standard library in `lun.lun` while keeping the Rust core minimal and focused on system-level operations.





