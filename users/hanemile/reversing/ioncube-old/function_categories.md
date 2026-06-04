# Function Category Mapping

This document provides a systematic categorization of renamed and discovered functions in the binary to guide remaining reverse engineering work.

## File Operations (file_ops)

### Core File I/O
- `copy_file_data` (0x412fa0) - Copy file content between paths
- `process_single_file` (0x416d40) - Process individual file transfer
- `scan_directory` (0x416c50) - Recursively scan directory contents
- `build_dest_path` (0x416c10) - Construct destination path
- `create_backup_name` (0x4181f0) - Generate backup filename

### File List Management
- `file_list_create` (0x41c260) - Initialize new file list
- `file_list_add_entry` (0x41c320) - Add entry to file list
- `file_list_sort` (0x41c220) - Sort file list
- `file_list_filter` (0x41be90) - Filter file list by criteria
- `file_list_compare_names` (0x41bea0) - Compare entries by name
- `file_list_compare_times` (0x41beb0) - Compare entries by timestamp
- `file_list_find_entry` (0x41c1d0) - Find entry in list
- `file_list_iterate` (0x41c000) - Iterate over list entries
- `file_list_free` (0x41bfe0) - Free file list memory
- `file_list_get_path` (0x41c0c0) - Get full path from entry
- `file_list_get_name` (0x41c010) - Get name from entry
- `file_list_get_full_path` (0x41c160) - Get complete path with base
- `file_list_get_parent` (0x41c3e0) - Get parent directory
- `file_list_init` (0x41bdf0) - Initialize file list structure
- `file_list_walk_tree` (0x41bec0) - Walk directory tree

### File Attributes
- `apply_file_attrs` (0x468c80) - Apply file attributes
- `apply_file_attrs_from_stat` (0x412de0) - Apply attrs from stat buffer
- `set_file_ownership` (0x413350) - Set file owner/group
- `copy_file_permissions` (0x415690) - Copy permission bits
- `update_file_times` (0x417360) - Update file timestamps

### Sync Operations
- `sync_directory` (0x41d000) - Synchronize directory
- `sync_files_main` (0x41d060) - Main sync entry point
- `sync_file_entry` (0x4179f0) - Sync individual file entry
- `sync_directory_entry` (0x416f30) - Sync directory entry
- `delete_file_entry` (0x416db0) - Delete file entry
- `delete_unlinked_file` (0x417eb0) - Delete unlinked file
- `delete_file_or_dir` (0x417e00) - Delete file or directory

### Path Operations
- `compare_paths` (0x417d60) - Compare two paths
- `check_path_contains` (0x4171d0) - Check if path contains another
- `validate_path_not_recursive` (0x415380) - Validate path isn't recursive
- `path_combine` (0x414b80) - Combine path components
- `resolve_path_wrapper` (0x414550) - Resolve path to absolute
- `handle_symlink` (0x4175a0) - Handle symbolic link

### Directory Operations
- `mkdir_recursive` (0x414aa0) - Create directory recursively
- `create_directory` (0x414a00) - Create single directory

### File Handles
- `file_handle_close` (0x41bcf0) - Close file handle
- `file_handle_open_write` (0x41bd60) - Open file for writing

## List Operations (list_ops)

### Dynamic Lists
- `add_string_to_list` (0x416ad0) - Add string to dynamic list
- (Note: More list operations need to be identified and renamed)

## String Utilities (string_utils)

### String Manipulation
- `escape_string_for_shell` (0x414390) - Escape string for shell
- `unescape_quoted_string` (0x414570) - Unescape quoted string
- `string_append_vsprintf` (0x4130c0) - Append formatted string
- `buffer_append_vsprintf` (0x413180) - Append to buffer with formatting
- `build_command_string` (0x412b20) - Build command string

### String Objects
- `create_string_object` (0x413de0) - Create string object
- `decrypt_obfuscated_string` (0x4b3b10) - Decrypt XOR-obfuscated string

## Configuration Management (config_mgmt)

### Config Loading
- `load_config_file` (0x4ac360) - Load configuration file
- `parse_and_validate_config` - Parse and validate config
- `free_config_list` (0x4abe90) - Free config list
- `build_config_linked_list` - Build config linked list

### Config Processing
- `process_config_type_0x3F` (0x4b0620) - Process config type 0x3F
- `process_config_type_0x3D` (0x4b0650) - Process config type 0x3D
- `process_config_type_0x10` (0x4b0590) - Process config type 0x10

### Filter Files
- `parse_filter_file` (0x41b5a0) - Parse filter/pattern file
- `match_pattern` (0x40bae0) - Match pattern against string
- `add_exclude_pattern` (0x413460) - Add exclusion pattern
- `add_include_pattern` (0x413510) - Add inclusion pattern
- `check_file_filter` (0x412a20) - Check if file matches filter

## Command Line (cmdline)

### Argument Parsing
- `parse_cmdline` (0x40ee40) - Parse command line arguments
- `parse_numeric_string` (0x4146a0) - Parse numeric argument

## Network/Protocol (network_proto)

### Protocol Initialization
- `init_protocol_handlers` (0x413e60) - Initialize protocol handlers
- `init_protocol_vtable` (0x436fd0) - Initialize protocol vtable
- `register_protocol_handler` (0x494940) - Register protocol handler
- `init_protocol_buffers` (0x438e40) - Initialize protocol buffers
- `finalize_protocol_init` (0x4948c0) - Finalize protocol initialization

### Protocol Operations
- `cleanup_protocol_pools` (0x413030) - Clean up protocol pools
- `protocol_send_data` (0x413260) - Send data via protocol
- `protocol_flush` (0x413340) - Flush protocol buffers
- `protocol_noop_1` (0x413240) - No-op protocol function 1
- `protocol_noop_2` (0x413320) - No-op protocol function 2

### Binary Protocol
- `parse_binary_protocol` (0x41d220) - Parse binary protocol messages
- `build_file_list_recursive` (0x41c470) - Build file list recursively (network)
- `process_directory_listing` (0x41ce80) - Process directory listing

## Logging and Output (logging)

### Message Functions
- `log_message_conditional` (0x414950) - Log message if verbose
- `format_and_print_message` (0x414720) - Format and print message
- `print_error_message` (0x415860) - Print error message
- `print_warning_message` (0x415820) - Print warning message
- `error_with_prefix` (0x4154f0) - Error with prefix string
- `error_exit_varargs` (0x4159c0) - Exit with error (varargs)
- `error_exit_stderr` (0x415950) - Exit with error to stderr
- `show_progress` (0x4155b0) - Show progress information
- `log_with_timestamp` (0x40b0fd) - Log with timestamp
- `log_error_internal` (0x40b11f) - Internal error logging

### Help/Display
- `display_help_or_lang_file` (0x40d090) - Display help or language file

### Formatting
- `format_size_human` (0x4144c0) - Format size in human-readable form
- `mode_flag_to_string` (0x4169f0) - Convert mode flag to string

## Memory Management (memory_mgmt)

### Allocators
- `init_memory_allocator` (0x4b04f0) - Initialize memory allocator
- `memory_allocator_table` (global) - Allocator vtable

### Slot Management
- `find_or_allocate_slot_type1` (0x4fff70) - Find/allocate slot type 1
- `find_or_allocate_slot_type2` (0x500210) - Find/allocate slot type 2
- `find_or_allocate_slot_type3` (0x500480) - Find/allocate slot type 3

## Initialization (init)

### System Init
- `early_init_routine` (0x414350) - Early initialization
- `secondary_init_routine` (0x415b00) - Secondary initialization
- `init_system_info` (0x4140c0) - Initialize system info
- `get_binary_directory_path` (0x4b0680) - Get binary directory

## License Management (license)

### License Operations
- `license_data_manager` (0x4b0880) - Manage license data
- `get_license_time` (0x4b05e0) - Get license timestamp

## Script Execution (script)

### Script Processing
- `parse_script_file` (0x412160) - Parse script file
- `execute_script_commands` (0x412330) - Execute script commands
- `eval_script_expression` (0x4128c0) - Evaluate script expression
- `execute_script_with_context` (0x413080) - Execute with context

## Utility Functions (utils)

### Error Handling
- `fatal_error` (0x40b0ae) - Fatal error handler
- `execute_with_exception_handler` (0x40b026) - Execute with exception handler

### Validation
- `check_required_var` (0x40b0cd) - Check required variable
- `check_php_script_header` (0x417f10) - Check PHP script header
- `always_return_one` (0x4b0570) - Always returns 1

### Array/Data Operations
- `get_array_element_at` (0x412a30) - Get array element at index
- `call_cleanup_handler` (0x412a40) - Call cleanup handler
- `init_stat_struct` (0x412b00) - Initialize stat structure

### Character Classification
- `classify_char_type` (0x4b0050) - Classify character type
- `read_u24_xor` (0x4b0140) - Read 24-bit XOR value

### Memory Operations
- `memcpy_n` (0x416970) - Memory copy n bytes
- `memcmp_n` (0x4169a0) - Memory compare n bytes

## Global State (globals)

### String Obfuscation
- `string_cache_table` (0x7e6470) - String cache/decryption cache
- `string_xor_key` (0x5a0080) - 16-byte XOR key for strings

### License State
- `license_struct_1` (0x7e1ec0) - License data structure 1
- `license_struct_2` (0x7e1ec8) - License data structure 2
- `license_storage_ptr` - License storage pointer

### Program State
- `program_exit_code` - Program exit code
- `operation_status` - Current operation status
- `operation_mode` - Operation mode flag
- `global_state_flag` - Global state flag
- `config_field_1` - Configuration field 1
- `root_privilege_flag` - Root privilege flag

### Buffers and Caches
- `main_setjmp_buf` - Main setjmp buffer

## Script Engine (script_engine)

### AST Node Creation
- `create_binary_op_node` (0x461080) - Create binary operator AST node
- `create_unary_op_node` (0x461140) - Create unary operator AST node
- `create_literal_node` (0x4611c0) - Create literal value node
- `create_string_literal_node` (0x461290) - Create string literal node
- `create_list_node` (0x4613e0) - Create list node
- `alloc_node` (0x460f50) - Allocate AST node

### Script Operations
- `script_load_module` (0x490390) - Load script module
- `script_call_function` (0x4909d0) - Call script function
- `set_data_field_by_type` (0x460b20) - Set data field by type
- `get_data_field_by_type` (0x460ba0) - Get data field by type

### Memory Pool System
- `pool_init` (0x475a00) - Initialize memory pool
- `pool_cleanup_items` (0x472420) - Cleanup pool items
- `alloc_from_pool` (0x4655b0) - Allocate from memory pool
- `free_to_pool` (0x465d30) - Free to memory pool
- `alloc_large_block` (0x465560) - Allocate large block
- `alloc_huge_block` (0x465350) - Allocate huge block
- `alloc_small_1` (0x4651e0) - Allocate small block type 1
- `alloc_small_2` (0x465240) - Allocate small block type 2
- `alloc_small_3` (0x4652a0) - Allocate small block type 3
- `malloc_checked` (0x462460) - Malloc with error checking

### Data Operations
- `checksum_compare` (0x4600a0) - Compare checksums

## Remaining High-Priority Functions to Rename

### 0x403000-0x404000 Range
- Multiple sub_403* functions (likely CRT/startup code)

### 0x40b000-0x40c000 Range  
- sub_40B14E to sub_40BA10 - String/list utilities (partially done)

### 0x415000-0x418000 Range
- sub_415750 to sub_417EB0 - File/path operations (mostly complete)

### 0x49x000-0x4Bx000 Range
- ~30 scripting functions remaining in 0x490000-0x493000
- ~20 buffer/pool functions remaining in 0x470000-0x476000
- ~15 checksum/hash functions in 0x460000-0x462000

## Type Structures Defined

- `struct license_data` - License information
- `struct dynamic_list` - Dynamic list/array
- `struct string_list` - String list
- `struct config_options` - Configuration options
- `struct file_list_entry` - File list entry (416 bytes)
- `struct filter_entry` - Filter pattern entry
- `struct filter_rule` - Filter rule
- `struct cmdline_options` - Command line options
- `struct allocator_vtable` - Memory allocator vtable
- `struct string_cache_entry` - String cache entry
- `struct filter_rule_entry` - Filter rule with match/action
- `struct transfer_stats` - File transfer statistics
- `struct network_protocol_message` - Protocol message header

## Session 3 Progress Summary

### Functions Renamed This Session: 47
- Script engine: 11 functions (AST nodes, module loading)
- Memory pool system: 11 functions (allocation strategies)
- Protocol operations: 8 functions (vtable, buffers, handlers)
- String utilities: 6 functions (escaping, formatting)
- Path operations: 4 functions (combining, validation)
- File operations: 3 functions (checksums, handles)
- Utility functions: 4 functions (error handling, validation)

### Total Progress: ~177 functions renamed (estimated 45% complete)

### Structure Types Applied
- Applied `struct dynamic_list*` to `parse_filter_file` parameters
- Applied `struct cmdline_options*` to `parse_cmdline` return type

## Next Priority Areas

1. **0x490000-0x493000 Range** - Remaining script interpreter functions (operators, control flow)
2. **0x470000-0x476000 Range** - Buffer management and I/O operations
3. **0x460000-0x462000 Range** - AST evaluation and type system
4. **0x43x000 Range** - Protocol data marshalling and handlers
5. **0x40b000-0x40c000** - Remaining utility/helper functions
6. **Apply remaining structure types** to function signatures for type propagation