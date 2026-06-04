# Complete Type Definitions - MCP Declarations

## Overview

This document contains all type definitions that have been declared in the IDA database using the MCP `declare_type` function. These types significantly improve code readability and provide structure to the binary analysis.

---

## Declaration Status

**Date**: 2024  
**Method**: MCP `declare_type(decls)` function  
**Types Declared**: 12 structures  
**Global Variables Typed**: 4  
**Status**: ✅ Complete

---

## Core Data Structures

### 1. struct license_data

**Size**: 40 bytes (0x28)  
**Purpose**: License/registration data with time-based validation

```c
struct license_data {
    uint64_t field0;           // +0x00: License component 1
    uint64_t field1;           // +0x08: License component 2
    uint64_t field2;           // +0x10: License component 3
    uint64_t timestamp_xor;    // +0x18: XOR-encrypted timestamp for validation
    uint64_t field4;           // +0x20: License component 5
};
```

**Key Usage**:
- Global: `license_struct_1` (0x7E1EC8)
- Global: `license_struct_2` (0x7E1EC0)
- Validation: `(timestamp_xor ^ 0x64E693C3) - 83544 > current_time`

**MCP Declaration**:
```
struct license_data { 
    uint64_t field0; 
    uint64_t field1; 
    uint64_t field2; 
    uint64_t timestamp_xor; 
    uint64_t field4; 
};
```

---

### 2. struct dynamic_list

**Size**: 24 bytes (0x18)  
**Purpose**: Generic dynamic array with automatic growth

```c
struct dynamic_list {
    int32_t count;        // +0x00: Current number of elements
    int32_t capacity;     // +0x04: Maximum capacity before realloc
    int32_t grow_size;    // +0x08: Number of elements to grow by
    int32_t padding;      // +0x0C: Alignment padding
    void *data;           // +0x10: Pointer to data array
};
```

**Behavior**:
- When `count == capacity`, grows by `grow_size` elements
- Uses `memory_allocator_table` for allocation/reallocation
- Generic container used throughout the binary

**Common Usage**:
```c
struct dynamic_list *list = malloc(sizeof(struct dynamic_list));
list->count = 0;
list->capacity = 32;
list->grow_size = 32;
list->data = allocate(list->capacity * element_size);
```

**MCP Declaration**:
```
struct dynamic_list { 
    int32_t count; 
    int32_t capacity; 
    int32_t grow_size; 
    int32_t padding; 
    void *data; 
};
```

---

### 3. struct string_list

**Size**: 24 bytes (0x18)  
**Purpose**: Specialized list for managing string arrays

```c
struct string_list {
    int32_t count;        // +0x00: Number of strings
    int32_t capacity;     // +0x04: Maximum capacity
    int32_t grow_size;    // +0x08: Growth increment
    int32_t padding;      // +0x0C: Alignment padding
    char **strings;       // +0x10: Array of string pointers
};
```

**Usage Examples**:
- Command-line argument lists
- Include/exclude pattern lists
- File path lists
- Filter string lists

**MCP Declaration**:
```
struct string_list { 
    int32_t count; 
    int32_t capacity; 
    int32_t grow_size; 
    int32_t padding; 
    char **strings; 
};
```

---

### 4. struct config_options

**Size**: ~336 bytes (varies by padding)  
**Purpose**: Main program configuration structure holding all options

```c
struct config_options {
    void *cmd_string;              // +0x00: Command string builder result
    int32_t verbose_level;         // +0x08: Verbosity level (0-9)
    int32_t padding1;              // +0x0C: Padding
    void *source_path;             // +0x10: Source file/directory path
    void *filter_data;             // +0x18: Filter data structure
    void *list_mgr;                // +0x20: List manager
    void *string_list;             // +0x28: String list
    void *remote_command;          // +0x30: Remote command to execute
    int32_t preserve_user;         // +0x38: Preserve user ownership (bool)
    int32_t preserve_group;        // +0x3C: Preserve group ownership (bool)
    int32_t preserve_perms;        // +0x40: Preserve permissions (bool)
    int32_t uid_override;          // +0x44: Override UID (-1 = none)
    int32_t gid_override;          // +0x48: Override GID (-1 = none)
    int32_t chmod_mode;            // +0x4C: Chmod mode bits (0777 format)
    int32_t compress_level;        // +0x50: Compression level (0-9)
    int32_t padding2;              // +0x54: Padding
    void *compress_method;         // +0x58: Compression method string
    void *remote_shell;            // +0x60: Remote shell command (ssh/rsh)
    void *ssh_options;             // +0x68: SSH command-line options
    void *rsh_command;             // +0x70: RSH command path
    void *link_dest;               // +0x78: Link destination for --link-dest
    int32_t delete_mode;           // +0x80: Delete extraneous files (bool)
    int32_t update_only;           // +0x84: Update only (skip existing newer)
    int32_t backup_mode;           // +0x88: Create backups (bool)
    int32_t backup_numbered;       // +0x8C: Use numbered backups (.1, .2, etc)
    int32_t backup_suffix;         // +0x90: Backup suffix type
    int32_t preserve_times;        // +0x94: Preserve modification times
    int32_t preserve_owner;        // +0x98: Preserve owner/group
    int32_t padding3;              // +0x9C: Padding
    void *exclude_from_file;       // +0xA0: File containing exclude patterns
    void *include_list;            // +0xA8: Include pattern list
    int32_t include_count;         // +0xB0: Number of include patterns
    int32_t include_capacity;      // +0xB4: Include list capacity
    void *exclude_list;            // +0xB8: Exclude pattern list
    int32_t exclude_count;         // +0xC0: Number of exclude patterns
    int32_t exclude_capacity;      // +0xC4: Exclude list capacity
    void *filter_list;             // +0xC8: Filter pattern list
    int32_t filter_count;          // +0xD0: Number of filters
    int32_t filter_capacity;       // +0xD4: Filter list capacity
    void *pattern_list;            // +0xD8: Additional pattern list
    int32_t pattern_count;         // +0xE0: Pattern count
    int32_t pattern_capacity;      // +0xE4: Pattern capacity
    void *remote_list;             // +0xE8: Remote file list
    void *string_list2;            // +0xF0: Secondary string list
    void *symlink_dest;            // +0xF8: Symlink destination handling
    int32_t copy_links;            // +0x100: Copy symlinks as files
    int32_t safe_links;            // +0x104: Only copy safe symlinks
};
```

**Key Fields Explained**:
- **Preservation flags** (0x38-0x40): Control which file attributes to preserve
- **Override values** (0x44-0x4C): Force specific UID/GID/permissions
- **Operation modes** (0x80-0x98): Delete, update, backup behaviors
- **Pattern lists** (0xA8-0xE4): Include/exclude/filter with dynamic sizing

**MCP Declaration**:
```
struct config_options { 
    void *cmd_string; 
    int32_t verbose_level; 
    int32_t padding1; 
    void *source_path; 
    void *filter_data; 
    void *list_mgr; 
    void *string_list; 
    void *remote_command; 
    int32_t preserve_user; 
    int32_t preserve_group; 
    int32_t preserve_perms; 
    int32_t uid_override; 
    int32_t gid_override; 
    int32_t chmod_mode; 
    int32_t compress_level; 
    int32_t padding2; 
    void *compress_method; 
    void *remote_shell; 
    void *ssh_options; 
    void *rsh_command; 
    void *link_dest; 
    int32_t delete_mode; 
    int32_t update_only; 
    int32_t backup_mode; 
    int32_t backup_numbered; 
    int32_t backup_suffix; 
    int32_t preserve_times; 
    int32_t preserve_owner; 
    int32_t padding3; 
    void *exclude_from_file; 
    void *include_list; 
    int32_t include_count; 
    int32_t include_capacity; 
    void *exclude_list; 
    int32_t exclude_count; 
    int32_t exclude_capacity; 
    void *filter_list; 
    int32_t filter_count; 
    int32_t filter_capacity; 
    void *pattern_list; 
    int32_t pattern_count; 
    int32_t pattern_capacity; 
    void *remote_list; 
    void *string_list2; 
    void *symlink_dest; 
    int32_t copy_links; 
    int32_t safe_links; 
};
```

---

### 5. struct file_list_entry

**Size**: 40 bytes (0x28)  
**Purpose**: Entry in file list for directory scanning/synchronization

```c
struct file_list_entry {
    char *path;           // +0x00: Full file path
    void *data;           // +0x08: Additional data (stat info, etc)
    int32_t flags;        // +0x10: Entry flags (include/exclude)
    int32_t type;         // +0x14: File type (regular/dir/link)
    int32_t status;       // +0x18: Processing status
    int32_t padding;      // +0x1C: Alignment
    uint64_t reserved1;   // +0x20: Reserved/future use
    uint64_t reserved2;   // +0x28: Reserved/future use
};
```

**Type Field Values**:
- `0`: Regular file
- `1`: Directory
- `2`: Symbolic link
- `3`: Special file (device, socket, etc)

**MCP Declaration**:
```
struct file_list_entry { 
    char *path; 
    void *data; 
    int32_t flags; 
    int32_t type; 
    int32_t status; 
    int32_t padding; 
    uint64_t reserved1; 
    uint64_t reserved2; 
};
```

---

### 6. struct copy_context

**Size**: 24 bytes (0x18)  
**Purpose**: Context passed to file copy operations

```c
struct copy_context {
    char *dest_path;         // +0x00: Destination base path
    void *config_options;    // +0x08: Pointer to config_options
    uint32_t flags;          // +0x10: Copy operation flags
    int32_t verbose;         // +0x14: Verbose output flag
};
```

**Usage**:
- Passed to `copy_file_data()`, `process_single_file()`
- Maintains state during recursive operations
- Provides access to global configuration

**MCP Declaration**:
```
struct copy_context { 
    char *dest_path; 
    void *config_options; 
    uint32_t flags; 
    int32_t verbose; 
};
```

---

### 7. struct config_list_node

**Size**: 40 bytes (0x28)  
**Purpose**: Linked list node for configuration file entries

```c
struct config_list_node {
    uint8_t type;         // +0x00: Entry type (0x01, 0x03, 0x10, 0x33, etc)
    uint8_t has_data;     // +0x01: Has associated data flag
    uint8_t padding1;     // +0x02: Padding
    uint8_t padding2;     // +0x03: Padding
    int32_t padding3;     // +0x04: Padding
    void *name;           // +0x08: Entry name/key
    void *data;           // +0x10: Entry data payload
    uint64_t data_size;   // +0x18: Size of data
    void *next;           // +0x20: Next node in list
};
```

**Type Values**:
- `0x01`: License data (stored at `license_struct_2`)
- `0x03`: Configuration data (stored at `license_struct_1`)
- `0x10`: Generic option (processed by `process_config_type_0x10`)
- `0x33`: 16-byte key data (must be exactly 16 bytes)
- `0x38`: Integer value (stored at `config_param`)
- `0x3D`: Conditional option (processed by `process_config_type_0x3D`)
- `0x3F`: Conditional option (processed by `process_config_type_0x3F`)

**MCP Declaration**:
```
struct config_list_node { 
    uint8_t type; 
    uint8_t has_data; 
    uint8_t padding1; 
    uint8_t padding2; 
    int32_t padding3; 
    void *name; 
    void *data; 
    uint64_t data_size; 
    void *next; 
};
```

---

## String Obfuscation Structures

### 8. struct string_cache_entry

**Size**: 24 bytes (0x18)  
**Purpose**: Cache entry for decrypted strings (hash table with chaining)

```c
struct string_cache_entry {
    unsigned char *encrypted_ptr;        // +0x00: Original encrypted string pointer
    char *decrypted_ptr;                 // +0x08: Decrypted string pointer
    struct string_cache_entry *next;     // +0x10: Next in collision chain
};
```

**Hash Table**:
- Global: `string_cache_table` (0x7E6470)
- Type: `struct string_cache_entry *[4096]`
- Hash: `(address >> 3) & 0xFFF`

**Collision Handling**:
- Uses linked list chaining
- New entries inserted at head of bucket

**MCP Declaration**:
```
struct string_cache_entry { 
    unsigned char *encrypted_ptr; 
    char *decrypted_ptr; 
    struct string_cache_entry *next; 
};
```

**Global Variable Typed**:
```c
// At 0x7E6470
struct string_cache_entry *string_cache_table[4096];
```

---

## Filter and Pattern Structures

### 9. struct filter_entry

**Size**: 16 bytes (0x10)  
**Purpose**: Single filter pattern entry

```c
struct filter_entry {
    int32_t type;         // +0x00: Filter type (include/exclude)
    int32_t flags;        // +0x04: Pattern flags (case-sensitive, etc)
    void *pattern;        // +0x08: Pattern string (glob or regex)
};
```

**Type Values**:
- `0`: Exclude pattern
- `1`: Include pattern
- `2`: Merge pattern
- `3`: Hide pattern

**MCP Declaration**:
```
struct filter_entry { 
    int32_t type; 
    int32_t flags; 
    void *pattern; 
};
```

---

### 10. struct filter_rule

**Size**: 24 bytes (0x18)  
**Purpose**: Filter rule in linked list

```c
struct filter_rule {
    int32_t type;         // +0x00: Rule type
    int32_t action;       // +0x04: Action (allow/deny)
    void *pattern;        // +0x08: Pattern to match
    void *next;           // +0x10: Next rule in list
};
```

**MCP Declaration**:
```
struct filter_rule { 
    int32_t type; 
    int32_t action; 
    void *pattern; 
    void *next; 
};
```

---

## Memory Management Structures

### 11. struct allocator_vtable

**Size**: 24 bytes (0x18)  
**Purpose**: Virtual function table for custom memory allocator

```c
struct allocator_vtable {
    void *(*alloc)(size_t size);              // +0x00: Allocation function
    void *(*realloc)(void *ptr, size_t size); // +0x08: Reallocation function
    void (*free)(void *ptr);                  // +0x10: Free function
};
```

**Global**: `memory_allocator_table` (0x7E6438)

**Usage**:
```c
// Allocate memory
void *ptr = (*memory_allocator_table->alloc)(size);

// Reallocate
ptr = (*memory_allocator_table->realloc)(ptr, new_size);

// Free
(*memory_allocator_table->free)(ptr);
```

**MCP Declaration**:
```
struct allocator_vtable { 
    void *(*alloc)(size_t size); 
    void *(*realloc)(void *ptr, size_t size); 
    void (*free)(void *ptr); 
};
```

---

## Command-Line Parsing Structures

### 12. struct cmdline_options

**Size**: ~824 bytes (0x338)  
**Purpose**: Parsed command-line options (returned by `parse_cmdline`)

```c
struct cmdline_options {
    int32_t argc;              // +0x00: Argument count
    int32_t argv_capacity;     // +0x04: Argv array capacity
    int32_t padding[2];        // +0x08: Padding
    char **argv;               // +0x10: Argument array
    void *filter_file;         // +0x18: Filter file path
    void *batch_file;          // +0x20: Batch file path
    void *config_file;         // +0x28: Config file path
    int32_t flags[200];        // +0x30: Boolean flags (200+ options)
};
```

**Flags Array** (partial list):
- `flags[0]`: Verbose mode
- `flags[2]`: Source file count
- `flags[6]`: Recursive mode
- `flags[7]`: Preserve permissions
- `flags[9]`: Archive mode
- `flags[10]`: Compress
- `flags[56]`: Config field 1
- `flags[57]`: Recursive flag
- `flags[74]`: Directory mode
- And many more...

**MCP Declaration**:
```
struct cmdline_options { 
    int32_t argc; 
    int32_t argv_capacity; 
    int32_t padding[2]; 
    char **argv; 
    void *filter_file; 
    void *batch_file; 
    void *config_file; 
    int32_t flags[200]; 
};
```

---

## Global Variables with Applied Types

### 1. string_cache_table (0x7E6470)

**Type**: `struct string_cache_entry *[4096]`  
**Purpose**: Hash table for cached decrypted strings

```c
struct string_cache_entry *string_cache_table[4096];
```

**Usage**:
```c
// Hash the encrypted pointer
int hash = ((uintptr_t)encrypted_string >> 3) & 0xFFF;

// Search collision chain
struct string_cache_entry *entry = string_cache_table[hash];
while (entry) {
    if (entry->encrypted_ptr == encrypted_string)
        return entry->decrypted_ptr;
    entry = entry->next;
}
```

---

### 2. string_xor_key (0x5A0080)

**Type**: `unsigned char string_xor_key[16]`  
**Purpose**: 16-byte XOR key for string decryption

```c
unsigned char string_xor_key[16] = {
    0x25, 0x68, 0xD3, 0xC2, 0x28, 0xF2, 0x59, 0x2E,
    0x94, 0xEE, 0xF2, 0x91, 0xAC, 0x13, 0x96, 0x95
};
```

**Decryption Algorithm**:
```c
int counter = length;  // Start with string length
for (int i = 1; i <= length; i++) {
    decrypted[i] = encrypted[i] ^ string_xor_key[counter & 0xF];
    counter++;
}
```

---

### 3. license_struct_1 (0x7E1EC8)

**Type**: `struct license_data *`  
**Purpose**: Primary license data structure

```c
struct license_data *license_struct_1 = malloc(sizeof(struct license_data));
```

**Validation**:
```c
if (*(_DWORD *)(license_struct_1 + 16) < current_time)
    longjmp(main_setjmp_buf, 97);
```

---

### 4. license_struct_2 (0x7E1EC0)

**Type**: `struct license_data *`  
**Purpose**: Secondary license data structure

```c
struct license_data *license_struct_2 = malloc(sizeof(struct license_data));
```

**Validation**:
```c
if ((*(_DWORD *)(license_struct_2 + 16) ^ 0x64E693C3) - 83544 > timer)
    longjmp(main_setjmp_buf, 96);
```

---

## Complete MCP Commands Used

All types were declared using the MCP `declare_type` function:

```python
# License data structure
declare_type(decls=[
    "struct license_data { uint64_t field0; uint64_t field1; uint64_t field2; uint64_t timestamp_xor; uint64_t field4; };"
])

# Dynamic list structures
declare_type(decls=[
    "struct dynamic_list { int32_t count; int32_t capacity; int32_t grow_size; int32_t padding; void *data; };",
    "struct string_list { int32_t count; int32_t capacity; int32_t grow_size; int32_t padding; char **strings; };"
])

# File and filter structures
declare_type(decls=[
    "struct filter_entry { int32_t type; int32_t flags; void *pattern; };",
    "struct file_list_entry { char *path; void *data; int32_t flags; int32_t type; int32_t status; int32_t padding; uint64_t reserved1; uint64_t reserved2; };",
    "struct copy_context { char *dest_path; void *config_options; uint32_t flags; int32_t verbose; };",
    "struct config_list_node { uint8_t type; uint8_t has_data; uint8_t padding1; uint8_t padding2; int32_t padding3; void *name; void *data; uint64_t data_size; void *next; };"
])

# Configuration structure
declare_type(decls=[
    "struct config_options { void *cmd_string; int32_t verbose_level; int32_t padding1; void *source_path; void *filter_data; void *list_mgr; void *string_list; void *remote_command; int32_t preserve_user; int32_t preserve_group; int32_t preserve_perms; int32_t uid_override; int32_t gid_override; int32_t chmod_mode; int32_t compress_level; int32_t padding2; void *compress_method; void *remote_shell; void *ssh_options; void *rsh_command; void *link_dest; int32_t delete_mode; int32_t update_only; int32_t backup_mode; int32_t backup_numbered; int32_t backup_suffix; int32_t preserve_times; int32_t preserve_owner; int32_t padding3; void *exclude_from_file; void *include_list; int32_t include_count; int32_t include_capacity; void *exclude_list; int32_t exclude_count; int32_t exclude_capacity; void *filter_list; int32_t filter_count; int32_t filter_capacity; void *pattern_list; int32_t pattern_count; int32_t pattern_capacity; void *remote_list; void *string_list2; void *symlink_dest; int32_t copy_links; int32_t safe_links; };"
])

# String obfuscation structures
declare_type(decls=[
    "struct string_cache_entry { unsigned char *encrypted_ptr; char *decrypted_ptr; struct string_cache_entry *next; };",
    "struct allocator_vtable { void *(*alloc)(size_t size); void *(*realloc)(void *ptr, size_t size); void (*free)(void *ptr); };",
    "struct filter_rule { int32_t type; int32_t action; void *pattern; void *next; };",
    "struct cmdline_options { int32_t argc; int32_t argv_capacity; int32_t padding[2]; char **argv; void *filter_file; void *batch_file; void *config_file; int32_t flags[200]; };"
])
```

Global variable typing with `set_type`:

```python
# String cache table (4096-entry hash table)
set_type(addr="0x7E6470", ty="struct string_cache_entry *[4096]")

# XOR key for string decryption
set_type(addr="0x5A0080", ty="unsigned char string_xor_key[16]")

# License structures
set_type(addr="0x7E1EC8", ty="struct license_data *")
set_type(addr="0x7E1EC0", ty="struct license_data *")
```

---

## Benefits and Impact

### Code Readability Improvement

**Before Type Definitions**:
```c
_QWORD *v3 = malloc(0x28u);
*v3 = 0;
v3[1] = 0;
v3[2] = 0;
if ((*(_DWORD *)(v3 + 16) ^ 0x64E693C3) - 83544 > timer)
    longjmp(buf, 96);
```

**After Type Definitions**:
```c
struct license_data *license = malloc(sizeof(struct license_data));
license->field0 = 0;
license->field1 = 0;
license->field2 = 0;
if ((license->timestamp_xor ^ 0x64E693C3) - 83544 > timer)
    longjmp(buf, 96);
```

### Structure Understanding

With proper types:
- Field offsets are automatic
- Relationships between structures are clear
- Memory layout is explicit
- Cross-references are typed

### Analysis Benefits

1. **Faster comprehension**: Self-documenting code
2. **Fewer errors**: Type checking catches mistakes
3. **Better navigation**: Jump to structure definitions
4. **Cleaner decompilation**: Types propagate through code

---

## Usage in IDA

### Viewing Types

```
View → Open subviews → Local Types
```

All declared structures will appear in the local types window.

### Applying Types to Variables

Right-click on a variable → "Convert to struct *" → Select structure type

### Cross-References

Right-click on struct name → "Jump to xref" to find all uses

---

## Next Steps

### Priority 1: Apply Types to Function Signatures

Update function signatures to use structure types:

```c
// Before:
__int64 __fastcall file_list_add_entry(void *list, void *path);

// After:
int file_list_add_entry(struct dynamic_list *list, const char *path);
```

### Priority 2: Apply Types to Local Variables

Apply structure types to local variables in key functions using `set_type`:

```python
set_type(addr="0x418300", kind="local", variable="src_file_list", ty="struct dynamic_list *")
set_type(addr="0x418300", kind="local", variable="dest_file_list", ty="struct dynamic_list *")
```

### Priority 3: Refine Structure Definitions

As analysis progresses, refine structures with:
- More descriptive field names
- Union types for overlapping fields
- Nested structures where appropriate
- Better documentation comments

---

## Summary

Successfully declared **12 comprehensive structures** using MCP:

1. ✅ `struct license_data` - License/registration (40 bytes)
2. ✅ `struct dynamic_list` - Generic dynamic arrays (24 bytes)
3. ✅ `struct string_list` - String array management (24 bytes)
4. ✅ `struct config_options` - Program configuration (~336 bytes)
5. ✅ `struct file_list_entry` - File list entries (40 bytes)
6. ✅ `struct copy_context` - Copy operation context (24 bytes)
7. ✅ `struct config_list_node` - Config file entries (40 bytes)
8. ✅ `struct string_cache_entry` - String cache entries (24 bytes)
9. ✅ `struct filter_entry` - Filter patterns (16 bytes)
10. ✅ `struct filter_rule` - Filter rules (24 bytes)
11. ✅ `struct allocator_vtable` - Memory allocator (24 bytes)
12. ✅ `struct cmdline_options` - Parsed options (~824 bytes)

**Global variables typed**: 4  
**Method**: MCP `declare_type` and `set_type` functions  
**Status**: Complete and ready for use ✅

These type definitions form the foundation for understanding the binary's architecture and will significantly improve reverse engineering productivity.

---

**Document Version**: 1.0  
**Date**: 2024  
**Status**: Complete ✅