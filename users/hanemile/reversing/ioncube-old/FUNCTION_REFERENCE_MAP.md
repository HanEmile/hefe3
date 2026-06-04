# Function Reference Map - PHP-like Scripting Engine

Quick reference guide for all identified functions organized by subsystem.

---

## 1. OPERATOR SEMANTICS (0x490000-0x492000)

### Expression Evaluation
| Address  | Function Name          | Description                                    |
|----------|------------------------|------------------------------------------------|
| 0x490190 | script_eval_expr       | Evaluate expression tree/node                  |
| 0x4902d0 | script_get_opcode      | Get opcode from instruction                    |
| 0x4902f0 | script_inc_ref         | Increment value reference count                |
| 0x490340 | script_dec_ref         | Decrement value reference count                |

### Binary Operators
| Address  | Function Name          | Operators Handled                              |
|----------|------------------------|------------------------------------------------|
| 0x4905c0 | script_binary_op       | +, -, *, /, %, ., &, \|, ^, <<, >>             |

### Unary Operators
| Address  | Function Name          | Operators Handled                              |
|----------|------------------------|------------------------------------------------|
| 0x490890 | script_unary_op        | -, !, ~, ++, --                                |

### Comparison Operators
| Address  | Function Name          | Operators Handled                              |
|----------|------------------------|------------------------------------------------|
| 0x490970 | script_compare_op      | ==, !=, <, >, <=, >=, ===, !==                |

### Logical Operators
| Address  | Function Name          | Operator                                       |
|----------|------------------------|------------------------------------------------|
| 0x4909f0 | script_logical_and     | && (logical AND)                               |
| 0x490a10 | script_logical_or      | \|\| (logical OR)                              |
| 0x490a30 | script_logical_not     | ! (logical NOT)                                |

### Type Casting
| Address  | Function Name          | Target Type                                    |
|----------|------------------------|------------------------------------------------|
| 0x490a50 | script_cast_to_int     | Cast to integer                                |
| 0x490b50 | script_cast_to_string  | Cast to string                                 |
| 0x490f00 | script_cast_to_bool    | Cast to boolean                                |
| 0x490f50 | script_cast_to_float   | Cast to float                                  |
| 0x490fe0 | script_type_check      | Validate type at runtime                       |

### Property Access
| Address  | Function Name          | Description                                    |
|----------|------------------------|------------------------------------------------|
| 0x4911b0 | script_get_property    | Get object property value                      |
| 0x491240 | script_set_property    | Set object property value                      |

---

## 2. VARIABLE SCOPE RESOLUTION (0x437000-0x438000)

### Scope Frame Management
| Address  | Function Name                | Description                              |
|----------|------------------------------|------------------------------------------|
| 0x4372c0 | scope_push_frame            | Create new scope frame on stack          |
| 0x437400 | scope_pop_frame             | Remove and cleanup scope frame           |
| 0x437490 | scope_get_current_frame     | Get pointer to current scope             |

### Variable Resolution
| Address  | Function Name                | Description                              |
|----------|------------------------------|------------------------------------------|
| 0x437560 | resolve_variable_in_scope   | Lookup variable in scope chain           |
| 0x437750 | scope_lookup_local          | Search local scope only                  |
| 0x437870 | scope_lookup_global         | Search global scope only                 |
| 0x4385e0 | variable_exists_in_scope    | Check if variable exists in scope        |
| 0x438740 | get_superglobal             | Access superglobal variables             |

### Closure Support
| Address  | Function Name                | Description                              |
|----------|------------------------------|------------------------------------------|
| 0x437600 | capture_closure_vars        | Capture variables for closure (use)      |
| 0x437ae0 | scope_create_closure        | Instantiate closure object               |
| 0x437d00 | scope_bind_closure_vars     | Bind captured vars to closure            |
| 0x437f70 | closure_capture_by_value    | Capture variable by value                |
| 0x4380f0 | closure_capture_by_ref      | Capture variable by reference            |
| 0x437ee0 | scope_resolve_upvalue       | Resolve upvalue in closure               |

### Global/Static Variables
| Address  | Function Name                | Description                              |
|----------|------------------------------|------------------------------------------|
| 0x437990 | declare_global_var          | Create global variable                   |
| 0x437a50 | declare_static_var          | Create static function variable          |
| 0x437da0 | handle_global_keyword       | Process 'global $var' declaration        |
| 0x437e50 | handle_static_keyword       | Process 'static $var' declaration        |

### Scope Chain Operations
| Address  | Function Name                | Description                              |
|----------|------------------------------|------------------------------------------|
| 0x438250 | scope_chain_lookup          | Walk scope chain for variable            |

---

## 3. EXCEPTION HANDLING (0x43a000-0x43c000)

### Core Exception Functions
| Address  | Function Name                  | Description                            |
|----------|--------------------------------|----------------------------------------|
| 0x43a000 | exception_subsystem_init      | Initialize exception subsystem         |
| 0x43a060 | throw_exception               | Throw exception object                 |
| 0x43a0b0 | rethrow_exception             | Re-throw caught exception              |
| 0x43a0e0 | catch_exception_by_type       | Match exception to catch block type    |
| 0x43a320 | register_exception_handler    | Register try/catch block handler       |

### Exception Object Management
| Address  | Function Name                  | Description                            |
|----------|--------------------------------|----------------------------------------|
| 0x43a470 | create_exception_object       | Instantiate Exception class            |
| 0x43ac00 | exception_get_message         | Get exception message                  |
| 0x43ad80 | exception_get_code            | Get exception code                     |
| 0x43adb0 | exception_get_file            | Get file where exception occurred      |
| 0x43b0f0 | exception_get_line            | Get line number of exception           |
| 0x43b430 | exception_set_message         | Set exception message                  |
| 0x43b4a0 | exception_set_code            | Set exception code                     |

### Stack Management
| Address  | Function Name                  | Description                            |
|----------|--------------------------------|----------------------------------------|
| 0x43a8c0 | get_exception_backtrace       | Generate stack trace                   |
| 0x43a9d0 | exception_unwind_stack        | Unwind stack during exception          |
| 0x43aa60 | find_exception_handler        | Search for matching catch block        |
| 0x43bf80 | exception_stack_push          | Push exception context                 |
| 0x43c000 | exception_stack_pop           | Pop exception context                  |

### Try/Catch/Finally
| Address  | Function Name                  | Description                            |
|----------|--------------------------------|----------------------------------------|
| 0x43bac0 | setup_try_block               | Enter try block                        |
| 0x43bbf0 | enter_catch_block             | Enter catch block                      |
| 0x43aab0 | execute_finally_block         | Execute finally block                  |
| 0x43bc70 | leave_exception_scope         | Exit try/catch/finally scope           |
| 0x43ab40 | exception_cleanup_frame       | Cleanup exception frame                |

### Exception Matching
| Address  | Function Name                  | Description                            |
|----------|--------------------------------|----------------------------------------|
| 0x43b610 | exception_match_type          | Check if exception matches type        |
| 0x43b2d0 | exception_get_trace_string    | Get formatted trace string             |
| 0x43bf00 | exception_table_lookup        | Lookup handler in exception table      |

---

## 4. INCLUDE/REQUIRE SYSTEM (0x48e000-0x48f000)

### File Loading
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x48e000 | include_file              | Include file (non-fatal on missing)      |
| 0x48e100 | require_file              | Require file (fatal on missing)          |
| 0x48e200 | include_once_file         | Include file once                        |
| 0x48e300 | require_once_file         | Require file once                        |

### Path Resolution
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x48e400 | resolve_include_path      | Find file in include_path                |
| 0x48e500 | normalize_file_path       | Canonicalize file path (realpath)        |

### Caching System
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x48e600 | check_file_included       | Check if file already loaded             |
| 0x48e700 | mark_file_included        | Mark file as included                    |
| 0x48e800 | get_included_files_list   | Return array of included files           |
| 0x48ea00 | include_cache_lookup      | Look up compiled file in cache           |
| 0x48eb00 | include_cache_insert      | Cache compiled file                      |

### Compilation and Execution
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x48ec00 | compile_included_file     | Compile file to AST/bytecode             |
| 0x48ed00 | execute_included_file     | Execute included file                    |

### Dependency Management
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x48e900 | check_circular_include    | Detect circular dependencies             |

---

## 5. FUNCTION CALL ABI (0x491000-0x493000)

### Function Resolution and Invocation
| Address  | Function Name                  | Description                            |
|----------|--------------------------------|----------------------------------------|
| 0x491d80 | script_resolve_function       | Look up function by name               |
| 0x491de0 | script_call_user_function     | Call user-defined function             |
| 0x4909d0 | script_call_function          | Generic function call dispatcher       |
| 0x490390 | script_load_module            | Load module/extension                  |

### Function Metadata
| Address  | Function Name                  | Description                            |
|----------|--------------------------------|----------------------------------------|
| 0x4921c0 | get_function_name             | Get function name string               |
| 0x4923b0 | get_function_parameters       | Get parameter list                     |
| 0x492450 | function_get_scope            | Get function scope (class)             |
| 0x4924d0 | function_get_modifier_flags   | Get access modifiers (public/private)  |
| 0x492330 | function_get_return_type      | Get return type declaration            |

### Function Properties
| Address  | Function Name                  | Description                            |
|----------|--------------------------------|----------------------------------------|
| 0x492270 | function_is_static            | Check if function is static            |
| 0x4922b0 | function_is_abstract          | Check if function is abstract          |
| 0x4922f0 | function_is_final             | Check if function is final             |
| 0x492370 | function_accepts_ref          | Check if accepts reference param       |
| 0x492390 | function_is_variadic          | Check if variadic (...$args)           |

### Argument Handling
| Address  | Function Name                  | Description                            |
|----------|--------------------------------|----------------------------------------|
| 0x4925e0 | prepare_function_args         | Prepare arguments for call             |
| 0x492670 | validate_arg_count            | Validate argument count                |
| 0x492730 | apply_default_args            | Apply default parameter values         |

### Constants
| Address  | Function Name                  | Description                            |
|----------|--------------------------------|----------------------------------------|
| 0x491ed0 | define_constant               | Define constant                        |
| 0x4927c0 | register_script_constants     | Register built-in constants            |

---

## 6. CLASS AND OBJECT SYSTEM (0x48a000-0x495000)

### Class Management
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x493e10 | create_class_entry        | Create class entry structure             |
| 0x493090 | check_class_name_valid    | Validate class name                      |
| 0x4930f0 | register_class_alias      | Register class alias                     |
| 0x493230 | get_class_entry           | Get class entry by name                  |
| 0x4935c0 | get_object_class_name     | Get class name from object               |
| 0x493730 | autoload_class            | Trigger class autoloader                 |

### Class Relationships
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x494570 | get_parent_class          | Get parent class entry                   |
| 0x4932e0 | instanceof_check          | Check if object instanceof class         |
| 0x493480 | is_subclass_of            | Check subclass relationship              |

### Class Members
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x494620 | get_class_constants       | Get all class constants                  |
| 0x4946b0 | get_class_methods         | Get all class methods                    |
| 0x494730 | get_class_properties      | Get all class properties                 |
| 0x494770 | class_has_constant        | Check if class has constant              |
| 0x4947a0 | class_has_method          | Check if class has method                |
| 0x4947e0 | class_has_property        | Check if class has property              |

### Object Instantiation
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x48a700 | class_instantiate         | Create new object instance               |
| 0x48abc0 | class_call_constructor    | Call __construct method                  |
| 0x48aae0 | class_destruct_object     | Call __destruct and cleanup              |
| 0x48a920 | class_clone_object        | Clone object (__clone)                   |

### Class Features
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x48a030 | class_check_interface     | Check interface implementation           |
| 0x48a0f0 | class_check_trait         | Check trait usage                        |
| 0x48a230 | class_add_interface       | Add interface to class                   |
| 0x48a350 | class_add_trait           | Add trait to class                       |
| 0x48a470 | class_resolve_parent      | Resolve parent class                     |

### Property Management
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x48c200 | class_add_property        | Add property to class                    |

### Method Management
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x48b1b0 | class_register_magic_method| Register magic method                   |
| 0x48b460 | class_get_method          | Get method entry by name                 |

---

## 7. ARRAY OPERATIONS (0x495000-0x496000)

### Array Creation and Initialization
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x495200 | array_init                | Initialize array                         |

### Array Manipulation
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x495660 | array_append              | Append element to array                  |
| 0x4956f0 | array_prepend             | Prepend element to array                 |
| 0x495750 | array_pop                 | Remove and return last element           |
| 0x495770 | array_shift               | Remove and return first element          |
| 0x495790 | array_insert_at           | Insert element at index                  |
| 0x495810 | array_remove_at           | Remove element at index                  |

### Array Access
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x4959e0 | array_get_element         | Get element by key                       |
| 0x495a30 | array_set_element         | Set element by key                       |

### Array Set Operations
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x495bd0 | array_merge               | Merge arrays                             |
| 0x495d30 | array_intersect           | Array intersection                       |
| 0x495e10 | array_diff                | Array difference                         |
| 0x495eb0 | array_unique              | Remove duplicate values                  |

---

## 8. STRING OPERATIONS (0x480000-0x483000)

### String Interning
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x482100 | intern_string             | Intern string in pool                    |
| 0x4807f0 | intern_or_create_string   | Intern or create new string              |
| 0x480a90 | string_copy_intern        | Copy and intern string                   |
| 0x482440 | init_string_constants     | Initialize string constant pool          |

### String Comparison
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x480600 | string_compare            | Compare two strings                      |
| 0x481180 | string_compare_function   | String comparison with options           |

### String Hash Operations
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x480570 | string_get_hash           | Get string hash value                    |
| 0x480650 | string_hash_lookup        | Lookup string in hash table              |
| 0x480730 | string_hash_insert        | Insert string into hash table            |
| 0x481d60 | string_hash_function      | Hash function for strings                |

### String Manipulation
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x480b60 | string_copy_function      | Copy string                              |
| 0x480ea0 | string_concatenate        | Concatenate strings                      |
| 0x481520 | string_release            | Release string (decrement refcount)      |
| 0x482620 | string_to_lower           | Convert to lowercase                     |
| 0x4826f0 | string_to_upper           | Convert to uppercase                     |
| 0x482780 | string_trim               | Trim whitespace                          |

### String Search
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x482830 | string_find_char          | Find character in string                 |
| 0x4828d0 | string_find_substring     | Find substring                           |
| 0x482910 | string_replace            | Replace substring                        |
| 0x482990 | string_split              | Split string                             |

### String Formatting
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x482a00 | string_format             | Format string (printf-style)             |
| 0x482d80 | string_escape             | Escape special characters                |
| 0x482fe0 | string_unescape           | Unescape escape sequences                |

---

## 9. HASH TABLE OPERATIONS (0x471000-0x478000)

### Hash Table Core
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x472730 | hash_table_init           | Initialize hash table                    |
| 0x472a60 | hash_table_destroy        | Destroy hash table                       |
| 0x471fd0 | hash_table_lookup         | Lookup value by key                      |
| 0x472270 | hash_table_insert         | Insert key-value pair                    |
| 0x4722e0 | hash_table_remove         | Remove key-value pair                    |
| 0x472350 | hash_table_resize         | Resize hash table                        |

### Hash Table Iteration
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x472d80 | hash_table_iterate        | Iterate over hash table                  |

### Hash Functions
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x472f20 | hash_compute              | Compute hash value                       |
| 0x473310 | hash_string               | Hash string value                        |

### Specialized Lookups
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x476c40 | hashtable_find            | Find entry in hash table                 |
| 0x476de0 | hashtable_exists          | Check if key exists                      |
| 0x4748f0 | hashtable_delete          | Delete entry from hash table             |
| 0x476950 | hashtable_insert_or_update| Insert or update entry                   |

---

## 10. MEMORY MANAGEMENT (0x462000-0x476000)

### Memory Pool Operations
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x475a00 | pool_init                 | Initialize memory pool                   |
| 0x475960 | pool_reset                | Reset pool to initial state              |
| 0x4758a0 | pool_clear_all            | Clear all pools                          |
| 0x475ab0 | pool_set_limit            | Set pool size limit                      |
| 0x475820 | pool_get_stats            | Get pool statistics                      |

### Pool Allocation
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x4655b0 | alloc_from_pool           | Allocate from pool                       |
| 0x475120 | pool_alloc_large          | Allocate large block                     |
| 0x475b60 | pool_alloc_aligned        | Allocate aligned memory                  |
| 0x475ef0 | pool_realloc              | Reallocate pool memory                   |

### Pool Deallocation
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x465d30 | free_to_pool              | Free memory to pool                      |
| 0x4753a0 | pool_free_large           | Free large block                         |
| 0x475fe0 | pool_free_all             | Free all pool memory                     |
| 0x475670 | pool_compact_large        | Compact large blocks                     |
| 0x472420 | pool_cleanup_items        | Cleanup pool items                       |

### Size-Class Allocators
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x463060 | alloc_size_16             | Allocate 16-byte block                   |
| 0x463010 | alloc_size_24             | Allocate 24-byte block                   |
| 0x462fc0 | alloc_size_32             | Allocate 32-byte block                   |
| 0x462f70 | alloc_size_48             | Allocate 48-byte block                   |
| 0x462f20 | alloc_size_64             | Allocate 64-byte block                   |
| 0x462ed0 | alloc_size_96             | Allocate 96-byte block                   |
| 0x462e80 | alloc_size_128            | Allocate 128-byte block                  |
| 0x462e30 | alloc_size_192            | Allocate 192-byte block                  |
| 0x462de0 | alloc_size_256            | Allocate 256-byte block                  |
| 0x462d90 | alloc_size_384            | Allocate 384-byte block                  |
| 0x462d40 | alloc_size_512            | Allocate 512-byte block                  |
| 0x462ce0 | alloc_size_768            | Allocate 768-byte block                  |
| 0x462c80 | alloc_size_1024           | Allocate 1024-byte block                 |
| 0x462c20 | alloc_size_1280           | Allocate 1280-byte block                 |
| 0x462bc0 | alloc_size_1536           | Allocate 1536-byte block                 |
| 0x462b60 | alloc_size_1792           | Allocate 1792-byte block                 |
| 0x462b00 | alloc_size_2048           | Allocate 2048-byte block                 |
| 0x462aa0 | alloc_size_2560           | Allocate 2560-byte block                 |
| 0x462a40 | alloc_size_3072           | Allocate 3072-byte block                 |

### Heap Operations
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x4630b0 | heap_allocate             | Allocate from heap                       |
| 0x463170 | heap_deallocate           | Free heap memory                         |
| 0x463230 | heap_reallocate           | Reallocate heap memory                   |

### Size Class Management
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x4620b0 | init_size_classes         | Initialize size classes                  |
| 0x462480 | compute_size_class        | Compute size class for allocation        |
| 0x462500 | get_size_class_index      | Get size class index                     |
| 0x4625a0 | round_up_to_size_class    | Round size up to class                   |

### Memory Utilities
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x462460 | malloc_checked            | Malloc with error checking               |
| 0x462440 | realloc_checked           | Realloc with error checking              |
| 0x462410 | calloc_checked            | Calloc with error checking               |

---

## 11. BUFFER I/O (0x470000-0x471000)

### Buffer Operations
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x470ce0 | buffer_init               | Initialize buffer                        |
| 0x470d50 | buffer_alloc              | Allocate buffer                          |
| 0x470e10 | buffer_free               | Free buffer                              |
| 0x470ed0 | buffer_clear              | Clear buffer contents                    |
| 0x470b40 | buffer_resize             | Resize buffer                            |

### Buffer I/O
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x4703d0 | buffer_read               | Read from buffer                         |
| 0x470750 | buffer_write              | Write to buffer                          |
| 0x470810 | buffer_flush              | Flush buffer                             |
| 0x470fa0 | buffer_peek               | Peek at buffer contents                  |

---

## 12. AST AND COMPILATION (0x460000-0x462000)

### AST Node Creation
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x460f50 | alloc_node                | Allocate AST node                        |
| 0x461840 | ast_alloc_node            | Allocate AST node (alt)                  |
| 0x461080 | create_binary_op_node     | Create binary operator node              |
| 0x461140 | create_unary_op_node      | Create unary operator node               |
| 0x4611c0 | create_literal_node       | Create literal value node                |
| 0x461290 | create_string_literal_node| Create string literal node               |
| 0x4613e0 | create_list_node          | Create list node                         |

### AST Manipulation
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x461500 | ast_add_child             | Add child node to AST node               |
| 0x4615e0 | ast_get_child             | Get child node from AST node             |
| 0x461730 | ast_copy_node             | Copy AST node                            |
| 0x4617e0 | ast_free_node             | Free AST node                            |
| 0x461690 | ast_set_line_number       | Set line number for node                 |

### AST Optimization
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x461c00 | ast_optimize              | Optimize AST                             |
| 0x461fb0 | ast_fold_constants        | Constant folding optimization            |

### Compilation
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x460100 | parse_class_declaration   | Parse class declaration                  |
| 0x460c20 | compile_method_body       | Compile method body                      |
| 0x460dc0 | compile_property_init     | Compile property initializer             |

---

## 13. UTILITY FUNCTIONS

### Comparison Functions
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x4600a0 | checksum_compare          | Compare checksums                        |
| 0x46ffe0 | value_compare             | Compare script values                    |

### List Operations
| Address  | Function Name              | Description                              |
|----------|----------------------------|------------------------------------------|
| 0x473740 | list_append               | Append to list                           |
| 0x473a80 | list_prepend              | Prepend to list                          |
| 0x473dc0 | list_remove               | Remove from list                         |
| 0x474030 | list_find                 | Find in list                             |
| 0x474100 | list_iterate              | Iterate over list                        |
| 0x474370 | list_clear                | Clear list                               |
| 0x474590 | list_contains             | Check if list contains item              |

---

## QUICK LOOKUP BY ADDRESS RANGE

| Address Range     | Subsystem                          |
|-------------------|-----------------------------------|
| 0x437000-0x438000 | Variable Scope Resolution         |
| 0x43a000-0x43c000 | Exception Handling                |
| 0x460000-0x462000 | AST and Compilation               |
| 0x462000-0x466000 | Memory Management                 |
| 
