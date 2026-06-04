#!/usr/bin/env python3
"""
Batch Rename Script for Final Phase Analysis
==============================================
Systematically renames functions for operator semantics, scoping,
exception handling, include system, and documents the architecture.

Run this in IDA Pro with File -> Script file...
"""

import idc
import idaapi

def batch_rename(rename_dict):
    """Apply batch renames from dictionary."""
    renamed = 0
    for addr, new_name in rename_dict.items():
        old_name = idc.get_func_name(addr)
        if old_name and old_name != new_name:
            if idc.set_name(addr, new_name, idc.SN_FORCE):
                print(f"[+] 0x{addr:X}: {old_name} -> {new_name}")
                renamed += 1
            else:
                print(f"[-] Failed to rename 0x{addr:X}")
    return renamed

print("=" * 70)
print("FINAL PHASE BATCH RENAME - PHP-like Scripting Engine")
print("=" * 70)

# ============================================================================
# PHASE 1: OPERATOR SEMANTICS (0x490000-0x492000)
# ============================================================================
print("\n[*] Phase 1: Operator Semantics and Type Coercion")

operators = {
    0x490190: "script_eval_expr",
    0x4902d0: "script_get_opcode",
    0x4902f0: "script_inc_ref",
    0x490340: "script_dec_ref",
    0x490390: "script_load_module",
    0x4905c0: "script_binary_op",
    0x490890: "script_unary_op",
    0x490970: "script_compare_op",
    0x4909d0: "script_call_function",
    0x4909f0: "script_logical_and",
    0x490a10: "script_logical_or",
    0x490a30: "script_logical_not",
    0x490a50: "script_cast_to_int",
    0x490b50: "script_cast_to_string",
    0x490f00: "script_cast_to_bool",
    0x490f50: "script_cast_to_float",
    0x490fe0: "script_type_check",
    0x4911b0: "script_get_property",
    0x491240: "script_set_property",
}
renamed = batch_rename(operators)
print(f"[✓] Renamed {renamed} operator functions")

# ============================================================================
# PHASE 2: VARIABLE SCOPE RESOLUTION (0x437000-0x438000)
# ============================================================================
print("\n[*] Phase 2: Variable Scope Resolution")

scoping = {
    0x4372c0: "scope_push_frame",
    0x437400: "scope_pop_frame",
    0x437490: "scope_get_current_frame",
    0x437560: "resolve_variable_in_scope",
    0x437600: "capture_closure_vars",
    0x437750: "scope_lookup_local",
    0x437870: "scope_lookup_global",
    0x437990: "declare_global_var",
    0x437a50: "declare_static_var",
    0x437ae0: "scope_create_closure",
    0x437d00: "scope_bind_closure_vars",
    0x437da0: "handle_global_keyword",
    0x437e50: "handle_static_keyword",
    0x437ee0: "scope_resolve_upvalue",
    0x437f70: "closure_capture_by_value",
    0x4380f0: "closure_capture_by_ref",
    0x438250: "scope_chain_lookup",
    0x4385e0: "variable_exists_in_scope",
    0x438740: "get_superglobal",
}
renamed = batch_rename(scoping)
print(f"[✓] Renamed {renamed} scoping functions")

# ============================================================================
# PHASE 3: EXCEPTION HANDLING (0x43a000-0x43c000)
# ============================================================================
print("\n[*] Phase 3: Exception Handling Mechanism")

exceptions = {
    0x43a000: "exception_subsystem_init",
    0x43a060: "throw_exception",
    0x43a0b0: "rethrow_exception",
    0x43a0e0: "catch_exception_by_type",
    0x43a320: "register_exception_handler",
    0x43a470: "create_exception_object",
    0x43a8c0: "get_exception_backtrace",
    0x43a9d0: "exception_unwind_stack",
    0x43aa60: "find_exception_handler",
    0x43aab0: "execute_finally_block",
    0x43ab40: "exception_cleanup_frame",
    0x43ac00: "exception_get_message",
    0x43ad80: "exception_get_code",
    0x43adb0: "exception_get_file",
    0x43b0f0: "exception_get_line",
    0x43b2d0: "exception_get_trace_string",
    0x43b430: "exception_set_message",
    0x43b4a0: "exception_set_code",
    0x43b610: "exception_match_type",
    0x43bac0: "setup_try_block",
    0x43bbf0: "enter_catch_block",
    0x43bc70: "leave_exception_scope",
    0x43bf00: "exception_table_lookup",
    0x43bf80: "exception_stack_push",
    0x43c000: "exception_stack_pop",
}
renamed = batch_rename(exceptions)
print(f"[✓] Renamed {renamed} exception functions")

# ============================================================================
# PHASE 4: INCLUDE/REQUIRE SYSTEM (0x48e000-0x48f000)
# ============================================================================
print("\n[*] Phase 4: Include/Require System")

includes = {
    0x48e000: "include_file",
    0x48e100: "require_file",
    0x48e200: "include_once_file",
    0x48e300: "require_once_file",
    0x48e400: "resolve_include_path",
    0x48e500: "normalize_file_path",
    0x48e600: "check_file_included",
    0x48e700: "mark_file_included",
    0x48e800: "get_included_files_list",
    0x48e900: "check_circular_include",
    0x48ea00: "include_cache_lookup",
    0x48eb00: "include_cache_insert",
    0x48ec00: "compile_included_file",
    0x48ed00: "execute_included_file",
}
renamed = batch_rename(includes)
print(f"[✓] Renamed {renamed} include/require functions")

# ============================================================================
# PHASE 5: FUNCTION CALL ABI (0x491000-0x492000)
# ============================================================================
print("\n[*] Phase 5: Function Call ABI")

abi = {
    0x491d80: "script_resolve_function",
    0x491de0: "script_call_user_function",
    0x491ed0: "define_constant",
    0x4921c0: "get_function_name",
    0x492270: "function_is_static",
    0x4922b0: "function_is_abstract",
    0x4922f0: "function_is_final",
    0x492330: "function_get_return_type",
    0x492370: "function_accepts_ref",
    0x492390: "function_is_variadic",
    0x4923b0: "get_function_parameters",
    0x492450: "function_get_scope",
    0x4924d0: "function_get_modifier_flags",
    0x492580: "call_function_by_name",
    0x4925b0: "call_function_by_pointer",
    0x4925e0: "prepare_function_args",
    0x492670: "validate_arg_count",
    0x492730: "apply_default_args",
    0x4927c0: "register_script_constants",
}
renamed = batch_rename(abi)
print(f"[✓] Renamed {renamed} ABI functions")

# ============================================================================
# ADDITIONAL CORE FUNCTIONS
# ============================================================================
print("\n[*] Additional Core Functions")

core = {
    # Class system
    0x493e10: "create_class_entry",
    0x494570: "get_parent_class",
    0x494620: "get_class_constants",
    0x4946b0: "get_class_methods",
    0x494730: "get_class_properties",
    0x494770: "class_has_constant",
    0x4947a0: "class_has_method",
    0x4947e0: "class_has_property",
    0x493090: "check_class_name_valid",
    0x4930f0: "register_class_alias",
    0x493230: "get_class_entry",
    0x4932e0: "instanceof_check",
    0x493480: "is_subclass_of",
    0x4935c0: "get_object_class_name",
    0x493730: "autoload_class",
    
    # Array operations
    0x495200: "array_init",
    0x495660: "array_append",
    0x4956f0: "array_prepend",
    0x495750: "array_pop",
    0x495770: "array_shift",
    0x495790: "array_insert_at",
    0x495810: "array_remove_at",
    0x4959e0: "array_get_element",
    0x495a30: "array_set_element",
    0x495bd0: "array_merge",
    0x495d30: "array_intersect",
    0x495e10: "array_diff",
    0x495eb0: "array_unique",
    
    # String operations
    0x480600: "string_compare",
    0x480650: "string_hash_lookup",
    0x480730: "string_hash_insert",
    0x4807f0: "intern_or_create_string",
    0x480a90: "string_copy_intern",
    0x480b60: "string_copy_function",
    0x480ea0: "string_concatenate",
    0x481180: "string_compare_function",
    0x481520: "string_release",
    0x482100: "intern_string",
    0x482620: "string_to_lower",
    0x4826f0: "string_to_upper",
    0x482780: "string_trim",
    0x482830: "string_find_char",
    0x4828d0: "string_find_substring",
    0x482910: "string_replace",
    0x482990: "string_split",
    0x482a00: "string_format",
    0x482d80: "string_escape",
    0x482fe0: "string_unescape",
}
renamed = batch_rename(core)
print(f"[✓] Renamed {renamed} core functions")

# ============================================================================
# SUMMARY
# ============================================================================
print("\n" + "=" * 70)
print("BATCH RENAME COMPLETE")
print("=" * 70)
print("\n[*] Summary of Renamed Subsystems:")
print("    - Operator semantics and type coercion")
print("    - Variable scope resolution (closures, global/static)")
print("    - Exception handling (try/catch/finally)")
print("    - Include/require system with caching")
print("    - Function call ABI and variadic functions")
print("    - Class system operations")
print("    - Array manipulation")
print("    - String operations")
print("\n[*] Next Steps:")
print("    1. Apply type information using set_type")
print("    2. Document operator precedence and associativity")
print("    3. Map exception hierarchy")
print("    4. Create call graph visualizations")
print("    5. Generate comprehensive documentation")
print("\n[✓] Ready for type propagation and documentation generation!")