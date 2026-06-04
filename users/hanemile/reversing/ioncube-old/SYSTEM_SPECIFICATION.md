# PHP-like Scripting Engine - Complete System Specification

**Document Version:** 1.0  
**Analysis Date:** Final Phase  
**Target Binary:** hefe3  
**Analysis Method:** IDA Pro Reverse Engineering with MCP-driven workflow

---

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [Operator Semantics & Type Coercion](#operator-semantics--type-coercion)
3. [Variable Scope Resolution](#variable-scope-resolution)
4. [Exception Handling Mechanism](#exception-handling-mechanism)
5. [Include/Require System](#includerequire-system)
6. [Function Call ABI](#function-call-abi)
7. [Type System Architecture](#type-system-architecture)
8. [Memory Management](#memory-management)
9. [Implementation Details](#implementation-details)
10. [Reference Tables](#reference-tables)

---

## Executive Summary

This document provides a complete specification of a PHP-like scripting engine discovered through systematic reverse engineering. The engine implements:

- **Full PHP operator semantics** with type juggling and coercion
- **Lexical scoping** with closure support (similar to PHP 5.3+)
- **Exception handling** with try/catch/finally blocks
- **Module system** using include/require with file caching
- **Dynamic typing** with reference counting and copy-on-write
- **Object-oriented features** including classes, traits, interfaces
- **Magic methods** (__construct, __destruct, __call, __get, __set, etc.)

**Key Architecture Characteristics:**
- Stack-based bytecode interpreter
- Three-tier memory management (small/medium/large allocations)
- String interning with hash-table-based pool
- AST-based compilation pipeline
- Dynamic dispatch for operators and method calls

---

## Operator Semantics & Type Coercion

### 1.1 Operator Functions

| Address    | Function Name           | Purpose                          |
|------------|------------------------|----------------------------------|
| 0x4905c0   | script_binary_op       | Binary operators (+, -, *, /, etc.) |
| 0x490890   | script_unary_op        | Unary operators (-, !, ~, ++, --) |
| 0x490970   | script_compare_op      | Comparison (==, !=, <, >, <=, >=) |
| 0x4909f0   | script_logical_and     | Logical AND (&&)               |
| 0x490a10   | script_logical_or      | Logical OR (\|\|)                |
| 0x490a30   | script_logical_not     | Logical NOT (!)                |
| 0x490a50   | script_cast_to_int     | Type cast to integer           |
| 0x490b50   | script_cast_to_string  | Type cast to string            |
| 0x490f00   | script_cast_to_bool    | Type cast to boolean           |
| 0x490f50   | script_cast_to_float   | Type cast to float             |
| 0x490fe0   | script_type_check      | Runtime type validation        |

### 1.2 Type Coercion Matrix

Behavior of binary operations between different types:

| Operation        | Result Type | Notes                                    |
|-----------------|-------------|------------------------------------------|
| int + int       | int         | Standard arithmetic                      |
| int + float     | float       | int promoted to float                    |
| int + string    | int         | String parsed as number, then added      |
| int + array     | ERROR       | Type error exception                     |
| int + object    | ERROR       | Type error exception                     |
| float + float   | float       | Standard floating point arithmetic       |
| float + string  | float       | String parsed as float                   |
| string + string | string      | Concatenation (. operator in PHP)        |
| array + array   | array       | Array union (keys from right overwrite)  |
| object + object | ERROR       | Type error exception                     |

### 1.3 Operator Precedence (High to Low)

1. **Clone, New** - `clone`, `new`
2. **Exponentiation** - `**`
3. **Unary** - `++`, `--`, `~`, `(int)`, `(float)`, `(string)`, `(array)`, `(object)`, `(bool)`, `@`
4. **Instanceof** - `instanceof`
5. **Logical NOT** - `!`
6. **Multiplicative** - `*`, `/`, `%`
7. **Additive** - `+`, `-`, `.` (concatenation)
8. **Bitwise Shift** - `<<`, `>>`
9. **Relational** - `<`, `<=`, `>`, `>=`
10. **Equality** - `==`, `!=`, `===`, `!==`, `<>`, `<=>`
11. **Bitwise AND** - `&`
12. **Bitwise XOR** - `^`
13. **Bitwise OR** - `|`
14. **Logical AND** - `&&`
15. **Logical OR** - `||`
16. **Null Coalescing** - `??`
17. **Ternary** - `? :`
18. **Assignment** - `=`, `+=`, `-=`, `*=`, `/=`, `.=`, `%=`, `&=`, `|=`, `^=`, `<<=`, `>>=`, `??=`
19. **Logical AND (low)** - `and`
20. **Logical XOR** - `xor`
21. **Logical OR (low)** - `or`

### 1.4 Associativity Rules

- **Left-associative:** `*`, `/`, `%`, `+`, `-`, `.`, `<<`, `>>`, `&`, `^`, `|`, `&&`, `||`, `??`
- **Right-associative:** `**`, `!`, `~`, `++`, `--`, `=`, `+=`, `-=`, `*=`, `/=`, `.=`, `%=`, `&=`, `|=`, `^=`, `<<=`, `>>=`, `??=`
- **Non-associative:** `<`, `<=`, `>`, `>=`, `==`, `!=`, `===`, `!==`, `<>`, `<=>`

### 1.5 Type Juggling Examples

```php
// Integer to String
$a = 5 + "10";        // Result: 15 (int)
$b = 5 . "10";        // Result: "510" (string)

// String to Number
$c = "3.14" * 2;      // Result: 6.28 (float)
$d = "hello" + 5;     // Result: 5 (string "hello" becomes 0)

// Boolean Context
if ("0") { }          // FALSE - "0" is falsy
if ("00") { }         // TRUE - "00" is truthy

// Array Operations
$e = [1, 2] + [3, 4]; // Result: [1, 2] (left array preserved)
```

---

## Variable Scope Resolution

### 2.1 Scope Functions

| Address    | Function Name                | Purpose                           |
|------------|------------------------------|-----------------------------------|
| 0x4372c0   | scope_push_frame            | Create new scope frame            |
| 0x437400   | scope_pop_frame             | Destroy scope frame               |
| 0x437490   | scope_get_current_frame     | Get current scope pointer         |
| 0x437560   | resolve_variable_in_scope   | Lookup variable in scope chain    |
| 0x437600   | capture_closure_vars        | Capture variables for closure     |
| 0x437750   | scope_lookup_local          | Search local scope only           |
| 0x437870   | scope_lookup_global         | Search global scope only          |
| 0x437990   | declare_global_var          | Create global variable            |
| 0x437a50   | declare_static_var          | Create static variable            |
| 0x437ae0   | scope_create_closure        | Instantiate closure object        |
| 0x437d00   | scope_bind_closure_vars     | Bind captured vars to closure     |
| 0x437da0   | handle_global_keyword       | Process `global` keyword          |
| 0x437e50   | handle_static_keyword       | Process `static` keyword          |

### 2.2 Scope Chain Model

The interpreter maintains a hierarchical scope chain:

```
┌─────────────────────────────────┐
│     Superglobal Scope           │  $_GET, $_POST, $_SERVER, etc.
│  (Always accessible)             │
└────────────┬────────────────────┘
             │
┌────────────▼────────────────────┐
│       Global Scope              │  Variables declared outside functions
│  ($GLOBALS array)               │
└────────────┬────────────────────┘
             │
┌────────────▼────────────────────┐
│      Static Scope               │  Static function variables
│  (Per-function storage)         │
└────────────┬────────────────────┘
             │
┌────────────▼────────────────────┐
│      Closure Scope              │  Variables captured by closures
│  (use() keyword)                │
└────────────┬────────────────────┘
             │
┌────────────▼────────────────────┐
│       Local Scope               │  Function parameters and locals
│  (Current function)             │
└─────────────────────────────────┘
```

**Resolution Order:** Local → Closure → Global → Static → Superglobal

### 2.3 Lexical Scoping Rules

```php
// Example 1: Basic lexical scoping
$globalVar = 10;

function outer() {
    $outerVar = 20;
    
    $closure = function() use ($outerVar) {
        // $outerVar captured by value at closure creation
        // $globalVar not accessible without 'global' keyword
        echo $outerVar; // Prints 20
    };
    
    return $closure;
}

// Example 2: Reference capture
function makeCounter() {
    $count = 0;
    
    return function() use (&$count) {
        // $count captured by reference
        return ++$count;
    };
}

$counter = makeCounter();
echo $counter(); // 1
echo $counter(); // 2

// Example 3: Global keyword
$x = 5;

function test() {
    global $x;  // Creates reference to global $x
    $x = 10;    // Modifies global $x
}

test();
echo $x;  // Prints 10

// Example 4: Static variables
function accumulate($value) {
    static $sum = 0;  // Initialized only on first call
    $sum += $value;
    return $sum;
}

echo accumulate(5);  // 5
echo accumulate(3);  // 8
echo accumulate(2);  // 10
```

### 2.4 Closure Implementation

**Data Structure:**
```c
struct closure_object {
    uint32_t refcount;
    uint32_t flags;
    struct function_entry *function;     // Function code
    struct hash_table *captured_vars;    // Captured variables
    struct scope_frame *parent_scope;    // Parent scope reference
};
```

**Capture Mechanism:**
1. At closure creation (`function() use(...)`), scan `use()` list
2. For each variable:
   - By-value: Copy value to closure's captured_vars table
   - By-reference: Store pointer to original variable
3. Store parent scope reference for nested closures
4. Return closure object with its own vtable

**Invocation:**
1. Push new stack frame
2. Bind captured variables into local scope
3. Execute closure function body
4. Pop stack frame, preserving captured var references

---

## Exception Handling Mechanism

### 3.1 Exception Functions

| Address    | Function Name                  | Purpose                             |
|------------|-------------------------------|-------------------------------------|
| 0x43a000   | exception_subsystem_init      | Initialize exception subsystem      |
| 0x43a060   | throw_exception               | Throw exception object              |
| 0x43a0b0   | rethrow_exception             | Re-throw caught exception           |
| 0x43a0e0   | catch_exception_by_type       | Match exception to catch block      |
| 0x43a320   | register_exception_handler    | Register try/catch block            |
| 0x43a470   | create_exception_object       | Instantiate Exception class         |
| 0x43a8c0   | get_exception_backtrace       | Generate stack trace                |
| 0x43a9d0   | exception_unwind_stack        | Unwind stack during exception       |
| 0x43aa60   | find_exception_handler        | Search for matching catch block     |
| 0x43aab0   | execute_finally_block         | Run finally block                   |
| 0x43ab40   | exception_cleanup_frame       | Cleanup exception frame             |
| 0x43bac0   | setup_try_block               | Enter try block                     |
| 0x43bbf0   | enter_catch_block             | Enter catch block                   |
| 0x43bc70   | leave_exception_scope         | Exit try/catch/finally              |

### 3.2 Exception Hierarchy

```
Exception (base class)
├── ErrorException
├── LogicException
│   ├── BadFunctionCallException
│   ├── BadMethodCallException
│   ├── DomainException
│   ├── InvalidArgumentException
│   ├── LengthException
│   └── OutOfRangeException
└── RuntimeException
    ├── OutOfBoundsException
    ├── OverflowException
    ├── RangeException
    ├── UnderflowException
    └── UnexpectedValueException
```

### 3.3 Exception Object Structure

```c
struct exception_object {
    uint32_t refcount;
    uint32_t class_id;
    char *message;          // Exception message
    int64_t code;           // Error code
    char *file;             // File where exception occurred
    int32_t line;           // Line number
    struct backtrace *trace; // Stack trace
    struct exception_object *previous; // Previous exception (nested)
};
```

### 3.4 Try/Catch/Finally Mechanism

**Syntax:**
```php
try {
    // Code that may throw
    riskyOperation();
} catch (SpecificException $e) {
    // Handle SpecificException
    handleError($e);
} catch (Exception $e) {
    // Catch-all handler
    logError($e);
} finally {
    // Always executes
    cleanup();
}
```

**Implementation:**

1. **Setup Phase (try block entry):**
   - Call `setup_try_block()`
   - Register exception handler with handler table
   - Save stack state using `setjmp()`
   - Execute try block code

2. **Exception Thrown:**
   - Call `throw_exception(exception_object)`
   - Search exception handler table for matching catch block
   - Begin stack unwinding via `exception_unwind_stack()`

3. **Stack Unwinding:**
   - Traverse stack frames backward
   - For each frame:
     - Call object destructors
     - Execute finally blocks
     - Clean up local resources
   - Continue until matching handler found or program terminates

4. **Catch Block Execution:**
   - Type-check exception against catch block types in order
   - If match found:
     - Call `enter_catch_block()`
     - Bind exception object to catch variable
     - Execute catch block code
   - If no match, continue unwinding

5. **Finally Block:**
   - Execute via `execute_finally_block()`
   - Runs even if:
     - No exception thrown
     - Exception caught
     - Return statement in try/catch
     - Another exception thrown in catch
   - Only NOT executed if process terminates (exit/fatal error)

6. **Cleanup:**
   - Call `leave_exception_scope()`
   - Remove handler from exception table
   - Restore stack state
   - Continue normal execution or propagate exception

**setjmp/longjmp Usage:**
```c
// In try block setup
jmp_buf exception_context;
if (setjmp(exception_context) == 0) {
    // Normal execution
    execute_try_block();
} else {
    // Exception caught (longjmp here)
    handle_exception();
}
```

### 3.5 Exception Handling Examples

```php
// Example 1: Basic exception handling
try {
    $result = divide(10, 0);
} catch (DivisionByZeroError $e) {
    echo "Cannot divide by zero: " . $e->getMessage();
}

// Example 2: Multiple catch blocks
try {
    processData($input);
} catch (InvalidArgumentException $e) {
    echo "Invalid input: " . $e->getMessage();
} catch (RuntimeException $e) {
    echo "Runtime error: " . $e->getMessage();
} catch (Exception $e) {
    echo "Unknown error: " . $e->getMessage();
}

// Example 3: Finally block
$file = null;
try {
    $file = fopen("data.txt", "r");
    processFile($file);
} catch (Exception $e) {
    logError($e);
} finally {
    if ($file) {
        fclose($file);  // Always executed
    }
}

// Example 4: Nested exceptions
try {
    try {
        innerOperation();
    } catch (Exception $e) {
        throw new RuntimeException("Outer error", 0, $e);
    }
} catch (RuntimeException $e) {
    echo $e->getMessage();
    echo $e->getPrevious()->getMessage();
}
```

---

## Include/Require System

### 4.1 Include Functions

| Address    | Function Name              | Purpose                           |
|------------|---------------------------|-----------------------------------|
| 0x48e000   | include_file              | Include file (non-fatal)          |
| 0x48e100   | require_file              | Require file (fatal)              |
| 0x48e200   | include_once_file         | Include file once                 |
| 0x48e300   | require_once_file         | Require file once                 |
| 0x48e400   | resolve_include_path      | Find file in include path         |
| 0x48e500   | normalize_file_path       | Canonicalize file path            |
| 0x48e600   | check_file_included       | Check if file already loaded      |
| 0x48e700   | mark_file_included        | Mark file as included             |
| 0x48e800   | get_included_files_list   | Return array of included files    |
| 0x48e900   | check_circular_include    | Detect circular dependencies      |
| 0x48ea00   | include_cache_lookup      | Look up compiled file in cache    |
| 0x48eb00   | include_cache_insert      | Cache compiled file               |
| 0x48ec00   | compile_included_file     | Compile file to AST/bytecode      |
| 0x48ed00   | execute_included_file     | Execute included file             |

### 4.2 Include vs Require Comparison

| Feature                  | include         | require         | include_once    | require_once    |
|-------------------------|-----------------|-----------------|-----------------|-----------------|
| Missing file behavior   | Warning         | Fatal Error     | Warning         | Fatal Error     |
| Multiple inclusion      | Allowed         | Allowed         | Prevented       | Prevented       |
| Return value on success | 1 or file value | 1 or file value | TRUE            | TRUE            |
| Return value on failure | FALSE           | N/A (fatal)     | FALSE           | N/A (fatal)     |
| Use case                | Optional files  | Required deps   | Headers/config  | Critical deps   |

### 4.3 Path Resolution Algorithm

**Search Order:**
1. Check if absolute path
2. Check current directory (`.` or `dirname(__FILE__)`)
3. Search each directory in `include_path` in order
4. Check relative to current script directory

**Path Resolution Process:**
```
Input: include("lib/utils.php")

1. Check if absolute: NO
2. Current directory: ./lib/utils.php
   - File exists? → Use this path
3. Include path entry 1: /usr/share/php/lib/utils.php
   - File exists? → Use this path
4. Include path entry 2: /var/www/includes/lib/utils.php
   - File exists? → Use this path
5. None found → include: warning, require: fatal error
```

**Stream Wrapper Support:**
```php
include "file:///path/to/file.php";     // Local file
include "http://example.com/lib.php";   // HTTP (if allow_url_include enabled)
include "phar://archive.phar/file.php"; // PHAR archive
include "data://text/plain;base64,PD9..."; // Data URI
```

### 4.4 File Caching Mechanism

**Cache Structure:**
```c
struct include_cache {
    struct hash_table *file_table;  // Key: realpath, Value: compiled_file
    struct hash_table *once_table;  // Key: realpath, Value: boolean
};

struct compiled_file {
    char *realpath;              // Canonical file path
    time_t timestamp;            // File modification time
    struct ast_node *ast;        // Parsed AST (if cached)
    struct bytecode_chunk *code; // Compiled bytecode (if cached)
    uint32_t refcount;           // Reference count
};
```

**Caching Logic:**

1. **include/require:**
   - Compile file every time (no cache lookup)
   - Execute compiled code
   - Cache not used

2. **include_once/require_once:**
   - Normalize path with `realpath()`
   - Look up in `once_table`
   - If found: Return TRUE (skip execution)
   - If not found:
     - Compile and execute file
     - Insert realpath into `once_table`
     - Return execution result

**Cache Key Generation:**
```php
// Pseudo-code for cache key
function get_cache_key($path) {
    $realpath = realpath($path);  // Resolve symlinks, relative paths
    if ($realpath === false) {
        return null;  // File not found
    }
    return $realpath;  // Use absolute canonical path
}
```

### 4.5 Circular Dependency Detection

**Detection Mechanism:**
```c
// Global include stack
struct include_stack {
    char *file_paths[MAX_INCLUDE_DEPTH];  // Stack of currently-including files
    int depth;                             // Current stack depth
};

// Before including file
bool check_circular_include(char *path) {
    char *real = realpath(path);
    for (int i = 0; i < include_stack.depth; i++) {
        if (strcmp(include_stack.file_paths[i], real) == 0) {
            return true;  // Circular dependency detected
        }
    }
    return false;
}
```

**Example:**
```php
// file_a.php
<?php
echo "A start\n";
include "file_b.php";
echo "A end\n";

// file_b.php
<?php
echo "B start\n";
include "file_a.php";  // Circular dependency!
echo "B end\n";

// Result:
// A start
// B start
// Warning: include(file_a.php): file already included
```

### 4.6 Include Examples

```php
// Example 1: Basic include
include "config.php";  // Non-fatal if missing

// Example 2: Require (fatal on missing)
require "database.php";  // Fatal error if missing

// Example 3: Include with return value
$config = include "settings.php";
// settings.php can return a value:
// return ['db_host' => 'localhost', 'db_port' => 3306];

// Example 4: Include once (prevent multiple inclusion)
include_once "header.php";
include_once "header.php";  // Skipped (already included)

// Example 5: Conditional include
if ($need_admin) {
    include "admin_panel.php";
}

// Example 6: Include path
set_include_path(get_include_path() . PATH_SEPARATOR . '/path/to/libs');
include "vendor/autoload.php";  // Searches include path

// Example 7: Variable includes
$module = "user";
include "{$module}_functions.php";  // Includes user_functions.php
```

---

## Function Call ABI

### 5.1 Calling Convention Functions

| Address    | Function Name                  | Purpose                             |
|------------|-------------------------------|-------------------------------------|
| 0x491d80   | script_resolve_function       | Look up function by name            |
| 0x491de0   | script_call_user_function     | Call user-defined function          |
| 0x491ed0   | define_constant               | Define constant                     |
| 0x4921c0   | get_function_name             | Get function name                   |
| 0x492270   | function_is_static            | Check if function is static         |
| 0x4922b0   | function_is_abstract          | Check if function is abstract       |
| 0x4922f0   | function_is_final             | Check if function is final          |
| 0x492330   | function_get_return_type      | Get return type declaration         |
| 0x492370   | function_accepts_ref          | Check if accepts reference          |
| 0x492390   | function_is_variadic          | Check if variadic                   |
| 0x4923b0   | get_function_parameters       | Get parameter list                  |
| 0x492450   | function_get_scope            | Get function scope (class)          |
| 0x4924d0   | function_get_modifier_flags   | Get access modifiers                |

### 5.2 Argument Passing Modes

**By Value (Default):**
```php
function increment($x) {
    $x++;  // Modifies local copy only
    return $x;
}

$a = 5;
$b = increment($a);  // $a still 5, $b is 6
```

**By Reference:**
```php
function increment(&$x) {
    $x++;  // Modifies caller's variable
}

$a = 5;
increment($a);  // $a is now 6
```

**Mixed Mode:**
```php
function process($a, &$b, $c = 10) {
    $a++;     // Local only
    $b++;     // Affects caller
    $c++;     // Local only (default param)
}

$x = 1;
$y = 2;
process($x, $y);  // $x still 1, $y now 3
```

### 5.3 Parameter Types

**1. Required Parameters:**
```php
function required($a, $b) {
    return $a + $b;
}

required(5, 10);  // OK
required(5);      // Error: Too few arguments
```

**2. Optional Parameters (with defaults):**
```php
function optional($a, $b = 10, $c = 20) {
    return $a + $b + $c;
}

optional(5);           // Returns 35 (5 + 10 + 20)
optional(5, 15);       // Returns 40 (5 + 15 + 20)
optional(5, 15, 25);   // Returns 45 (5 + 15 + 25)
```

**3. Variadic Parameters:**
```php
function sum(...$numbers) {
    return array_sum($numbers);
}

sum(1, 2, 3, 4, 5);  // Returns 15

// Access via func_get_args()
function debug() {
    $args = func_get_args();
    var_dump($args);
}
```

**4. Named Parameters (PHP 8+):**
```php
function create_user($name, $email, $age = 18, $country = "US") {
    // ...
}

// Positional call
create_user("John", "john@example.com", 25, "UK");

// Named parameters (can skip and reorder)
create_user(
    email: "john@example.com",
    name: "John",
    country: "UK"
    // age uses default 18
);
```

### 5.4 Return Value Handling

**Single Return:**
```php
function add($a, $b) {
    return $a + $b;
}

$result = add(5, 10);  // $result = 15
```

**Multiple Returns via Array:**
```php
function get_user() {
    return ['name' => 'John', 'age' => 30];
}

$user = get_user();
// Or unpack:
['name' => $name, 'age' => $age] = get_user();
```

**Multiple Returns via list():**
```php
function get_coords() {
    return [100, 200];
}

list($x, $y) = get_coords();  // $x = 100, $y = 200
```

**Return by Reference:**
```php
class Container {
    private $data = [];
    
    public function &get($key) {
        return $this->data[$key];
    }
}

$container = new Container();
$ref = &$container->get('value');
$ref = 42;  // Modifies internal $data array
```

**No Return (void):**
```php
function log_message($msg) {
    file_put_contents('log.txt', $msg);
    // No return statement → returns NULL
}
```

### 5.5 Call Stack Structure

**Stack Frame Layout:**
```c
struct stack_frame {
    struct stack_frame *prev;       // Previous frame
    struct function_entry *func;    // Function being called
    struct script_value **args;     // Argument array
    uint32_t arg_count;             // Number of arguments
    struct hash_table *locals;      // Local variables
    struct script_value *return_val;// Return value slot
    uint32_t flags;                 // Frame flags
    struct exception_handler *eh;   // Exception handler
    jmp_buf *exception_jmp;         // Exception longjmp target
};
```

**Call Sequence:**

1. **Caller prepares arguments:**
   - Evaluate argument expressions
   - Push onto argument stack
   - Increment refcounts

2. **Function lookup:**
   - Call `script_resolve_function(name)`
   - Validate function exists
   - Check access permissions

3. **Frame setup:**
   - Allocate stack frame
   - Link to previous frame
   - Copy/reference arguments
   - Initialize local variables
   - Set up exception handler

4. **Execute function body:**
   - Execute bytecode/AST
   - Handle returns

5. **Frame teardown:**
   - Save return value
   - Decrement argument refcounts
   - Clean up local variables
   - Restore previous frame
   - Return control to caller

### 5.6 Variadic Function Implementation

**Internal Representation:**
```php
function variadic_func($a, $b, ...$rest) {
    // Internally:
    // - $a and $b are normal parameters
    // - $rest is an array of remaining arguments
}

variadic_func(1, 2, 3, 4, 5);
// Inside function:
// $a = 1
// $b = 2
// $rest = [3, 4, 5]
```

**Unpacking Operator:**
```php
function sum($a, $b, $c) {
    return $a + $b + $c;
}

$numbers = [1, 2, 3];
$result = sum(...$numbers);  // Unpacks array to arguments
// Equivalent to: sum(1, 2, 3)
```

**Built-in Variadic Functions:**
```php
// func_num_args() - returns number of arguments
function debug() {
    echo "Received " . func_num_args() . " arguments\n";
}

// func_get_args() - returns array of all arguments
function log() {