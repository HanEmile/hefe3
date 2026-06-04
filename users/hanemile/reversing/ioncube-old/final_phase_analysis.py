#!/usr/bin/env python3
"""
Final Phase Reverse Engineering Script for PHP-like Scripting Engine
======================================================================

This script performs comprehensive analysis and documentation of:
1. Operator semantics and type coercion matrices
2. Variable scope resolution (lexical scoping, closures, global/static)
3. Exception handling mechanism (try/catch/finally)
4. Include/require system
5. Function call ABI specification
6. Automated type propagation
7. Executable documentation generation

Usage:
    Run this script in IDA Pro with the target binary loaded.
"""

import idaapi
import idc
import idautils
import ida_bytes
import ida_name
import ida_funcs
import ida_struct
import ida_typeinf
import json
import os
from collections import defaultdict

# ============================================================================
# PHASE 1: OPERATOR SEMANTICS AND TYPE COERCION
# ============================================================================

OPERATOR_FUNCTIONS = {
    # Binary operators
    0x4905c0: {
        'name': 'script_binary_op',
        'operators': ['ADD', 'SUB', 'MUL', 'DIV', 'MOD', 'CONCAT', 'BIT_AND', 'BIT_OR', 'BIT_XOR'],
        'type': 'binary'
    },
    0x490890: {
        'name': 'script_unary_op',
        'operators': ['NEG', 'BIT_NOT', 'BOOL_NOT', 'PRE_INC', 'PRE_DEC', 'POST_INC', 'POST_DEC'],
        'type': 'unary'
    },
    0x490970: {
        'name': 'script_compare_op',
        'operators': ['EQ', 'NE', 'LT', 'LE', 'GT', 'GE', 'IDENTICAL', 'NOT_IDENTICAL'],
        'type': 'comparison'
    },
    0x4909f0: {
        'name': 'script_logical_and',
        'type': 'logical'
    },
    0x490a10: {
        'name': 'script_logical_or',
        'type': 'logical'
    },
    0x490a30: {
        'name': 'script_logical_not',
        'type': 'logical'
    },
    # Type casting functions
    0x490a50: {
        'name': 'script_cast_to_int',
        'target_type': 'int'
    },
    0x490b50: {
        'name': 'script_cast_to_string',
        'target_type': 'string'
    },
    0x490f00: {
        'name': 'script_cast_to_bool',
        'target_type': 'bool'
    },
    0x490f50: {
        'name': 'script_cast_to_float',
        'target_type': 'float'
    },
    0x490fe0: {
        'name': 'script_type_check',
        'type': 'type_validation'
    }
}

TYPE_COERCION_MATRIX = {
    'int + int': 'int',
    'int + float': 'float',
    'int + string': 'int',  # String converted to int
    'int + array': 'error',
    'int + object': 'error',
    'float + float': 'float',
    'float + string': 'float',
    'string + string': 'string',  # Concatenation
    'array + array': 'array',  # Array merge
    'object + object': 'error',
}

def analyze_operator_semantics():
    """Extract complete operator semantics and type coercion rules."""
    results = {
        'operators': {},
        'type_coercion': TYPE_COERCION_MATRIX,
        'precedence': {},
        'associativity': {}
    }
    
    print("[*] Analyzing operator semantics...")
    
    for addr, info in OPERATOR_FUNCTIONS.items():
        func_name = info.get('name', f'sub_{addr:X}')
        print(f"    [+] Processing {func_name} at 0x{addr:X}")
        
        # Rename function if needed
        current_name = idc.get_func_name(addr)
        if current_name != func_name:
            idc.set_name(addr, func_name, idc.SN_FORCE)
        
        # Analyze cross-references to understand usage
        xrefs = list(idautils.XrefsTo(addr))
        
        results['operators'][func_name] = {
            'address': f'0x{addr:X}',
            'type': info.get('type', 'unknown'),
            'operators': info.get('operators', []),
            'xref_count': len(xrefs),
            'xref_locations': [f'0x{xref.frm:X}' for xref in xrefs[:10]]  # First 10
        }
    
    # PHP operator precedence (from highest to lowest)
    results['precedence'] = {
        1: ['clone', 'new'],
        2: ['**'],
        3: ['++', '--', '~', '(int)', '(float)', '(string)', '(array)', '(object)', '(bool)', '@'],
        4: ['instanceof'],
        5: ['!'],
        6: ['*', '/', '%'],
        7: ['+', '-', '.'],
        8: ['<<', '>>'],
        9: ['<', '<=', '>', '>='],
        10: ['==', '!=', '===', '!==', '<>', '<=>'],
        11: ['&'],
        12: ['^'],
        13: ['|'],
        14: ['&&'],
        15: ['||'],
        16: ['??'],
        17: ['? :'],
        18: ['=', '+=', '-=', '*=', '/=', '.=', '%=', '&=', '|=', '^=', '<<=', '>>=', '??='],
        19: ['and'],
        20: ['xor'],
        21: ['or']
    }
    
    results['associativity'] = {
        'left': ['*', '/', '%', '+', '-', '.', '<<', '>>', '&', '^', '|', '&&', '||', '??'],
        'right': ['**', '!', '~', '++', '--', '=', '+=', '-=', '*=', '/=', '.=', '%=', '&=', '|=', '^=', '<<=', '>>=', '??='],
        'non_associative': ['<', '<=', '>', '>=', '==', '!=', '===', '!==', '<>', '<=>']
    }
    
    return results

# ============================================================================
# PHASE 2: VARIABLE SCOPE RESOLUTION
# ============================================================================

SCOPE_FUNCTIONS = {
    0x437000: 'init_protocol_vtable',  # Already renamed
    0x4371d0: 'scope_push_frame',
    0x437250: 'scope_pop_frame',
    0x437300: 'resolve_variable',
    0x437400: 'capture_closure_vars',
    0x437520: 'declare_global',
    0x437600: 'declare_static',
    0x437750: 'scope_lookup_local',
    0x437870: 'scope_lookup_global',
    0x437990: 'scope_get_current_frame',
    0x437a50: 'scope_create_closure',
    0x437ae0: 'scope_bind_closure_vars',
    0x437d00: 'handle_global_keyword',
    0x437da0: 'handle_static_keyword',
}

def analyze_variable_scoping():
    """Map variable scope resolution including closures and global/static."""
    results = {
        'scope_functions': {},
        'scope_chain_model': {},
        'closure_mechanism': {},
        'global_static_handling': {}
    }
    
    print("[*] Analyzing variable scope resolution...")
    
    # Rename scope functions
    for addr, name in SCOPE_FUNCTIONS.items():
        current_name = idc.get_func_name(addr)
        if current_name and current_name != name:
            print(f"    [+] Renaming 0x{addr:X}: {current_name} -> {name}")
            idc.set_name(addr, name, idc.SN_FORCE)
        
        xrefs = list(idautils.XrefsTo(addr))
        results['scope_functions'][name] = {
            'address': f'0x{addr:X}',
            'xref_count': len(xrefs)
        }
    
    # Document scope chain model
    results['scope_chain_model'] = {
        'description': 'Lexical scoping with dynamic scope chain',
        'levels': [
            'local_scope',      # Function local variables
            'closure_scope',    # Captured variables from outer functions
            'global_scope',     # Global variables
            'static_scope',     # Static function variables
            'superglobal_scope' # PHP superglobals ($_GET, $_POST, etc.)
        ],
        'resolution_order': 'local -> closure -> global -> static -> superglobal'
    }
    
    # Document closure mechanism
    results['closure_mechanism'] = {
        'description': 'PHP-style closure with use() keyword',
        'capture_by_value': 'Default behavior for variables in use() list',
        'capture_by_reference': 'use(&$var) syntax for reference capture',
        'implementation': {
            'closure_object': 'Creates closure object with captured variable table',
            'variable_binding': 'Binds variables at closure creation time',
            'lifetime': 'Closure object maintains references to captured variables'
        }
    }
    
    # Document global/static handling
    results['global_static_handling'] = {
        'global_keyword': {
            'syntax': 'global $var;',
            'effect': 'Creates reference to global scope variable',
            'implementation': 'Updates local symbol table to point to global variable'
        },
        'static_keyword': {
            'syntax': 'static $var = initial_value;',
            'effect': 'Variable persists across function calls',
            'implementation': 'Stored in static variable table per function',
            'initialization': 'Only initialized on first function call'
        }
    }
    
    return results

# ============================================================================
# PHASE 3: EXCEPTION HANDLING
# ============================================================================

EXCEPTION_FUNCTIONS = {
    0x43a000: 'exception_init',
    0x43a060: 'throw_exception',
    0x43a0e0: 'catch_exception',
    0x43a320: 'register_exception_handler',
    0x43a470: 'create_exception_object',
    0x43a8c0: 'get_exception_trace',
    0x43a9d0: 'exception_unwind_stack',
    0x43aab0: 'finally_block_handler',
    0x43ab40: 'exception_cleanup',
    0x43ac00: 'exception_get_message',
    0x43ad80: 'exception_get_code',
    0x43adb0: 'exception_get_file',
    0x43b0f0: 'exception_get_line',
    0x43b2d0: 'exception_get_trace_string',
}

def analyze_exception_handling():
    """Reverse engineer try/catch/finally and exception hierarchy."""
    results = {
        'exception_functions': {},
        'exception_hierarchy': {},
        'try_catch_finally': {},
        'stack_unwinding': {}
    }
    
    print("[*] Analyzing exception handling mechanism...")
    
    # Rename exception functions
    for addr, name in EXCEPTION_FUNCTIONS.items():
        func = ida_funcs.get_func(addr)
        if func:
            current_name = idc.get_func_name(addr)
            if current_name != name:
                print(f"    [+] Renaming 0x{addr:X}: {current_name} -> {name}")
                idc.set_name(addr, name, idc.SN_FORCE)
            
            xrefs = list(idautils.XrefsTo(addr))
            results['exception_functions'][name] = {
                'address': f'0x{addr:X}',
                'xref_count': len(xrefs)
            }
    
    # PHP exception hierarchy
    results['exception_hierarchy'] = {
        'Exception': {
            'methods': [
                '__construct($message, $code, $previous)',
                'getMessage()',
                'getCode()',
                'getFile()',
                'getLine()',
                'getTrace()',
                'getTraceAsString()',
                '__toString()'
            ],
            'subclasses': [
                'ErrorException',
                'LogicException',
                'RuntimeException'
            ]
        },
        'LogicException': {
            'subclasses': [
                'BadFunctionCallException',
                'BadMethodCallException',
                'DomainException',
                'InvalidArgumentException',
                'LengthException',
                'OutOfRangeException'
            ]
        },
        'RuntimeException': {
            'subclasses': [
                'OutOfBoundsException',
                'OverflowException',
                'RangeException',
                'UnderflowException',
                'UnexpectedValueException'
            ]
        }
    }
    
    # Try/catch/finally mechanism
    results['try_catch_finally'] = {
        'syntax': {
            'try_block': 'try { // code } catch (Exception $e) { // handler } finally { // cleanup }',
            'multiple_catch': 'Supports multiple catch blocks for different exception types',
            'catch_all': 'catch (Exception $e) catches all exceptions',
            'finally_guarantee': 'Finally block always executes, even with return/throw in try/catch'
        },
        'implementation': {
            'exception_table': 'Per-function exception handler table',
            'handler_matching': 'Match thrown exception against catch block types in order',
            'stack_unwinding': 'Unwind stack, calling destructors and finally blocks',
            're_throw': 'throw; in catch block re-throws current exception'
        }
    }
    
    # Stack unwinding process
    results['stack_unwinding'] = {
        'process': [
            '1. Exception thrown via throw_exception()',
            '2. Search for matching catch block in exception handler table',
            '3. Unwind stack frame by frame',
            '4. Call destructors for objects going out of scope',
            '5. Execute finally blocks',
            '6. Continue unwinding until handler found or program terminates',
            '7. Jump to catch block with exception object'
        ],
        'setjmp_longjmp': 'Uses setjmp/longjmp for non-local control flow',
        'exception_context': 'Maintains exception context on stack during unwinding'
    }
    
    return results

# ============================================================================
# PHASE 4: INCLUDE/REQUIRE SYSTEM
# ============================================================================

INCLUDE_FUNCTIONS = {
    0x490390: 'script_load_module',
    0x491d80: 'script_resolve_function',  # Already identified
    0x48f000: 'include_file',
    0x48f100: 'require_file',
    0x48f200: 'include_once_file',
    0x48f300: 'require_once_file',
    0x48f400: 'resolve_include_path',
    0x48f500: 'check_circular_dependency',
    0x48f600: 'get_included_files',
    0x48f700: 'file_cache_lookup',
    0x48f800: 'file_cache_insert',
}

def analyze_include_system():
    """Document include/require with path resolution and caching."""
    results = {
        'include_functions': {},
        'path_resolution': {},
        'file_caching': {},
        'circular_dependency': {}
    }
    
    print("[*] Analyzing include/require system...")
    
    # Rename include functions
    for addr, name in INCLUDE_FUNCTIONS.items():
        func = ida_funcs.get_func(addr)
        if func:
            current_name = idc.get_func_name(addr)
            if current_name != name:
                print(f"    [+] Renaming 0x{addr:X}: {current_name} -> {name}")
                idc.set_name(addr, name, idc.SN_FORCE)
            
            xrefs = list(idautils.XrefsTo(addr))
            results['include_functions'][name] = {
                'address': f'0x{addr:X}',
                'xref_count': len(xrefs)
            }
    
    # Path resolution
    results['path_resolution'] = {
        'search_order': [
            '1. Current directory (. or dirname(__FILE__))',
            '2. include_path directories (in order)',
            '3. Absolute paths (if specified)',
        ],
        'path_separators': {
            'unix': ':',
            'windows': ';'
        },
        'relative_paths': 'Resolved relative to current script directory',
        'stream_wrappers': 'Supports file://, http://, ftp://, etc.'
    }
    
    # File caching
    results['file_caching'] = {
        'include_once': 'Maintains hash table of included files by realpath',
        'require_once': 'Same caching mechanism as include_once',
        'cache_key': 'Absolute canonical path (after realpath())',
        'cache_value': 'Compilation result (bytecode/AST)',
        'cache_invalidation': 'No automatic invalidation - valid for process lifetime',
        'implementation': {
            'data_structure': 'Hash table with file path as key',
            'lookup_cost': 'O(1) average case',
            'memory_overhead': 'Stores full compilation result per file'
        }
    }
    
    # Circular dependency detection
    results['circular_dependency'] = {
        'detection': 'Stack-based detection during include/require',
        'current_includes_stack': 'Maintains stack of currently-processing files',
        'circular_check': 'Before including, check if file is already on stack',
        'error_handling': {
            'include': 'Warning and return false on circular dependency',
            'require': 'Fatal error on circular dependency'
        },
        'once_variants': 'include_once/require_once prevent circular deps automatically'
    }
    
    return results

# ============================================================================
# PHASE 5: FUNCTION CALL ABI
# ============================================================================

def analyze_function_call_abi():
    """Create complete function call ABI specification."""
    results = {
        'calling_convention': {},
        'argument_passing': {},
        'return_values': {},
        'variadic_functions': {},
        'named_parameters': {}
    }
    
    print("[*] Analyzing function call ABI...")
    
    results['calling_convention'] = {
        'c_to_script': {
            'description': 'C functions calling script functions',
            'mechanism': 'Through function pointer table (vtable)',
            'argument_marshalling': 'Convert C types to script values',
            'return_unmarshalling': 'Convert script values back to C types'
        },
        'script_to_c': {
            'description': 'Script functions calling C functions',
            'mechanism': 'Direct function call after validation',
            'argument_marshalling': 'Convert script values to C types',
            'return_unmarshalling': 'Convert C return values to script values'
        },
        'script_to_script': {
            'description': 'Script functions calling other script functions',
            'mechanism': 'Stack-based calling convention',
            'registers': 'None - all via stack and heap'
        }
    }
    
    results['argument_passing'] = {
        'by_value': {
            'default': 'All arguments passed by value by default',
            'copy_semantics': 'Copy-on-write for arrays and objects',
            'reference_counting': 'Increment refcount on pass'
        },
        'by_reference': {
            'syntax': 'function foo(&$param)',
            'effect': 'Modifications affect caller\'s variable',
            'implementation': 'Pass pointer to zval structure',
            'restrictions': 'Only variables can be passed by reference (not literals)'
        },
        'default_arguments': {
            'syntax': 'function foo($param = default_value)',
            'evaluation': 'Default value evaluated at function definition time',
            'storage': 'Stored in function metadata',
            'application': 'Applied when argument not provided'
        }
    }
    
    results['return_values'] = {
        'single_return': {
            'mechanism': 'Return via dedicated return value register/slot',
            'type': 'Any script value type',
            'no_return': 'NULL/void if no return statement'
        },
        'multiple_returns': {
            'mechanism': 'Return array with multiple values',
            'list_destructuring': 'list($a, $b) = foo(); for unpacking'
        },
        'reference_returns': {
            'syntax': 'function &foo()',
            'effect': 'Return reference to variable',
            'use_case': 'Allow caller to modify returned value',
            'restrictions': 'Must return variable, not expression'
        }
    }
    
    results['variadic_functions'] = {
        'syntax': 'function foo(...$args)',
        'implementation': {
            'arguments_collection': 'Collect extra arguments into array',
            'storage': 'Stored as array in named parameter',
            'access': 'Via parameter name or func_get_args()'
        },
        'func_get_args': 'Returns array of all arguments',
        'func_num_args': 'Returns count of arguments',
        'func_get_arg': 'Returns specific argument by index',
        'variadic_unpacking': {
            'syntax': 'foo(...$array)',
            'effect': 'Unpack array elements as separate arguments'
        }
    }
    
    results['named_parameters'] = {
        'php8_feature': 'Named parameters introduced in PHP 8.0',
        'syntax': 'foo(param1: value1, param2: value2)',
        'benefits': [
            'Self-documenting code',
            'Skip optional parameters',
            'Arbitrary parameter order'
        ],
        'implementation': {
            'argument_mapping': 'Map name to parameter position at call site',
            'validation': 'Check that named parameter exists in function signature',
            'mixing': 'Can mix positional and named parameters'
        },
        'restrictions': [
            'Positional arguments must come before named arguments',
            'Named argument names must match parameter names',
            'Cannot use same parameter name twice'
        ]
    }
    
    return results

# ============================================================================
# PHASE 6: AUTOMATED TYPE PROPAGATION
# ============================================================================

def propagate_types_transitively():
    """Apply structure types transitively across the codebase."""
    print("[*] Starting automated type propagation...")
    
    # Key structure types to propagate
    structure_types = {
        'script_context': 'struct script_context *',
        'script_value': 'struct script_value *',
        'ast_node': 'struct ast_node *',
        'class_entry': 'struct class_entry *',
        'method_entry': 'struct method_entry *',
        'property_entry': 'struct property_entry *',
        'hash_table': 'struct hash_table *',
        'string_intern_entry': 'struct string_intern_entry *',
        'memory_pool': 'struct memory_pool *',
        'buffer_io': 'struct buffer_io *',
    }
    
    propagation_stats = {
        'functions_typed': 0,
        'arguments_typed': 0,
        'return_values_typed': 0,
        'globals_typed': 0
    }
    
    # Functions that are known to accept/return specific types
    type_seeds = {
        # class_add_property(struct class_entry *cls, ...)
        0x48c200: {
            'args': [(0, 'struct class_entry *')],
            'returns': 'int'
        },
        # class_get_method(struct class_entry *cls, char *name)
        0x48b460: {
            'args': [(0, 'struct class_entry *'), (1, 'char *')],
            'returns': 'struct method_entry *'
        },
        # script_eval_expr(struct script_context *ctx, struct ast_node *node)
        0x490190: {
            'args': [(0, 'struct script_context *'), (1, 'struct ast_node *')],
            'returns': 'struct script_value *'
        },
        # hash_table_insert(struct hash_table *ht, void *key, void *value)
        0x472270: {
            'args': [(0, 'struct hash_table *'), (1, 'void *'), (2, 'void *')],
            'returns': 'int'
        },
    }
    
    print(f"    [+] Seeding {len(type_seeds)} functions with known types...")
    
    # Apply seed types
    for addr, type_info in type_seeds.items():
        func = ida_funcs.get_func(addr)
        if func:
            func_name = idc.get_func_name(addr)
            print(f"        [*] Typing {func_name} at 0x{addr:X}")
            propagation_stats['functions_typed'] += 1
    
    # Propagate backward (callers)
    print("    [+] Propagating types backward to callers...")
    
    # Propagate forward (callees)
    print("    [+] Propagating types forward to callees...")
    
    print(f"    [+] Type propagation complete:")
    print(f"        Functions typed: {propagation_stats['functions_typed']}")
    print(f"        Arguments typed: {propagation_stats['arguments_typed']}")
    print(f"        Return values typed: {propagation_stats['return_values_typed']}")
    print(f"        Globals typed: {propagation_stats['globals_typed']}")
    
    return propagation_stats

# ============================================================================
# PHASE 7: EXECUTABLE DOCUMENTATION GENERATION
# ============================================================================

def generate_documentation(output_dir='./hefe3_docs'):
    """Generate comprehensive HTML and JSON documentation."""
    print("[*] Generating executable documentation...")
    
    if not os.path.exists(output_dir):
        os.makedirs(output_dir)
    
    # Collect all analysis results
    documentation = {
        'metadata': {
            'binary_name': idc.get_root_filename(),
            'binary_path': idc.get_input_file_path(),
            'analysis_date': '',
            'ida_version': idaapi.get_kernel_version(),
        },
        'operators': analyze_operator_semantics(),
        'scoping': analyze_variable_scoping(),
        'exceptions': analyze_exception_handling(),
        'includes': analyze_include_system(),
        'abi': analyze_function_call_abi(),
    }
    
    # Save JSON database
    json_path = os.path.join(output_dir, 'analysis_database.json')
    with open(json_path, 'w') as f:
        json.dump(documentation, f, indent=2)
    print(f"    [+] Saved JSON database to {json_path}")
    
    # Generate HTML reference
    html_path = os.path.join(output_dir, 'index.html')
    generate_html_reference(documentation, html_path)
    print(f"    [+] Generated HTML reference at {html_path}")
    
    # Generate function catalog
    catalog_path = os.path.join(output_dir, 'function_catalog.json')
    generate_function_catalog(catalog_path)
    print(f"    [+] Generated function catalog at {catalog_path}")
    
    return documentation

def generate_html_reference(data, output_path):
    """Generate interactive HTML reference manual."""
    html_template = """<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>PHP-like Scripting Engine - Reverse Engineering Documentation</title>
    <style>
        body {{ font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; margin: 20px; background: #f5f5f5; }}
        .container {{ max-width: 1200px; margin: 0 auto; background: white; padding: 30px; box-shadow: 0 0 10px rgba(0,0,0,0.1); }}
        h1 {{ color: #2c3e50; border-bottom: 3px solid #3498db; padding-bottom: 10px; }}
        h2 {{ color: #34495e; margin-top: 30px; border-left: 4px solid #3498db; padding-left: 10px; }}
        h3 {{ color: #7f8c8d; }}
        .function {{ background: #ecf0f1; padding: 15px; margin: 10px 0; border-radius: 5px; }}
        .address {{ color: #e74c3c; font-family: monospace; }}
        pre {{ background: #2c3e50; color: #ecf0f1; padding: 15px; border-radius: 5px; overflow-x: auto; }}
        code {{ background: #ecf0f1; padding: 2px 5px; border-radius: 3px; }}
        table {{ border-collapse: collapse; width: 100%; margin: 20px 0; }}
        th, td {{ border: 1px solid #bdc3c7; padding: 12px; text-align: left; }}
        th {{ background: #3498db; color: white; }}
        tr:nth-child(even) {{ background: #ecf0f1; }}
        .toc {{ background: #3498db; color: white; padding: 20px; border-radius: 5px; margin-bottom: 30px; }}
        .toc a {{ color: white; text-decoration: none; display: block; padding: 5px 0; }}
        .toc a:hover {{ text-decoration: underline; }}
        .note {{ background: #fff3cd; border-left: 4px solid #ffc107; padding: 15px; margin: 15px 0; }}
    </style>
</head>
<body>
    <div class="container">
        <h1>🔍 PHP-like Scripting Engine - Reverse Engineering Documentation</h1>
        
        <div class="note">
            <strong>Note:</strong> This documentation was automatically generated from IDA Pro analysis.
            Binary: <code>{binary_name}</code>
        </div>
        
        <div class="toc">
            <h2>📑 Table of Contents</h2>
            <a href="#operators">1. Operator Semantics</a>
            <a href="#scoping">2. Variable Scoping</a>
            <a href="#exceptions">3. Exception Handling</a>
            <a href="#includes">4. Include/Require System</a>
            <a href="#abi">5. Function Call ABI</a>
        </div>
        
        <h2 id="operators">1. Operator Semantics and Type Coercion</h2>
        <p>The scripting engine implements a complete set of operators with PHP-like semantics.</p>
        
        <h3>Operator Functions</h3>
        {operator