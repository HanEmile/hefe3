# MCP Resources

Resources represent browsable state (read-only data) following MCP's philosophy.

Core IDB State:

ida://idb/metadata - IDB file info (path, arch, base, size, hashes)
ida://idb/segments - Memory segments with permissions
ida://idb/entrypoints - Entry points (main, TLS callbacks, etc.)
UI State:

ida://cursor - Current cursor position and function
ida://selection - Current selection range
Type Information:

ida://types - All local types
ida://structs - All structures/unions
ida://struct/{name} - Structure definition with fields
Lookups:

ida://import/{name} - Import details by name
ida://export/{name} - Export details by name
ida://xrefs/from/{addr} - Cross-references from address
Core Functions

lookup_funcs(queries): Get function(s) by address or name (auto-detects, accepts list or comma-separated string).
int_convert(inputs): Convert numbers to different formats (decimal, hex, bytes, ASCII, binary).
list_funcs(queries): List functions (paginated, filtered).
list_globals(queries): List global variables (paginated, filtered).
imports(offset, count): List all imported symbols with module names (paginated).
decompile(addr): Decompile function at the given address.
disasm(addr): Disassemble function with full details (arguments, stack frame, etc).
xrefs_to(addrs): Get all cross-references to address(es).
xrefs_to_field(queries): Get cross-references to specific struct field(s).
callees(addrs): Get functions called by function(s) at address(es).
Modification Operations

set_comments(items): Set comments at address(es) in both disassembly and decompiler views.
patch_asm(items): Patch assembly instructions at address(es).
declare_type(decls): Declare C type(s) in the local type library.
Memory Reading Operations

get_bytes(addrs): Read raw bytes at address(es).
get_int(queries): Read integer values using ty (i8/u64/i16le/i16be/etc).
get_string(addrs): Read null-terminated string(s).
get_global_value(queries): Read global variable value(s) by address or name (auto-detects, compile-time values).
Stack Frame Operations

stack_frame(addrs): Get stack frame variables for function(s).
declare_stack(items): Create stack variable(s) at specified offset(s).
delete_stack(items): Delete stack variable(s) by name.
Structure Operations

read_struct(queries): Read structure field values at specific address(es).
search_structs(filter): Search structures by name pattern.

Batch Operations

rename(batch): Unified batch rename operation for functions, globals, locals, and stack variables (accepts dict with optional func, data, local, stack keys).
patch(patches): Patch multiple byte sequences at once.
put_int(items): Write integer values using ty (i8/u64/i16le/i16be/etc).
