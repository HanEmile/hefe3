# String Obfuscation Analysis

## Overview

The binary uses a sophisticated string obfuscation system to hide sensitive strings (error messages, file paths, configuration strings, etc.) from static analysis. All obfuscated strings are decrypted at runtime using a custom XOR-based decryption routine.

## Decryption Function

**Address**: `0x4B3B10`  
**Name**: `decrypt_obfuscated_string` (renamed from `sub_4B3B10`)  
**Purpose**: Decrypt XOR-obfuscated strings with caching

### Function Signature

```c
__int64 __fastcall decrypt_obfuscated_string(unsigned __int8 *encrypted_string)
```

### Algorithm

The function implements a **cached XOR decryption** scheme:

1. **Cache Lookup**: Checks a hash table (`qword_7E6470`) to see if the string was already decrypted
2. **Decryption**: If not cached, performs XOR decryption
3. **Caching**: Stores the decrypted result for future lookups

### Detailed Analysis

```c
__int64 __fastcall decrypt_obfuscated_string(unsigned __int8 *encrypted_string)
{
  void *cache_table;              // Hash table for caching decrypted strings
  __int64 hash_index;             // Hash bucket index
  __int64 cache_entry;            // Current cache entry
  __int64 bucket_offset;          // Offset into hash table
  size_t encrypted_len;           // Length including header
  unsigned __int8 *buffer;        // Allocated buffer for decrypted string
  _BYTE *decrypted_data;          // Pointer to decrypted data (skips length byte)
  __int64 decrypt_end;            // End position for decryption loop
  int xor_counter;                // Counter for XOR key indexing
  char current_byte;              // Current byte being decrypted
  _QWORD *new_cache_entry;        // New cache entry structure
  _QWORD *bucket_head;            // Head of hash bucket linked list

  // Initialize cache table if not already created (4096 buckets * 8 bytes)
  cache_table = qword_7E6470;
  if (!qword_7E6470) {
    cache_table = calloc(0x1000u, 8u);  // 4096 entry hash table
    qword_7E6470 = cache_table;
  }

  // Calculate hash bucket: ((address >> 3) & 0xFFF)
  // This gives 4096 possible buckets
  hash_index = ((int)encrypted_string >> 3) & 0xFFF;
  cache_entry = *((_QWORD *)cache_table + hash_index);
  bucket_offset = 8 * hash_index;

  // Check if already decrypted (cache hit)
  if (cache_entry) {
    while (*(unsigned __int8 **)cache_entry != encrypted_string) {
      cache_entry = *(_QWORD *)(cache_entry + 16);  // Next in linked list
      if (!cache_entry)
        goto DECRYPT_NEW;
    }
    return *(_QWORD *)(cache_entry + 8);  // Return cached decrypted string
  }

DECRYPT_NEW:
  // First byte contains the length of encrypted data
  encrypted_len = *encrypted_string + 2LL;
  
  // Allocate buffer for decrypted string
  buffer = (unsigned __int8 *)malloc(encrypted_len);
  memcpy(buffer, encrypted_string, encrypted_len);
  
  // Decrypted data starts after the length byte
  decrypted_data = buffer + 1;
  
  // XOR decrypt the string
  decrypt_end = (__int64)&buffer[*buffer + 2];
  xor_counter = (unsigned __int8)*buffer;  // Initialize counter with length
  
  do {
    current_byte = xor_counter++;
    *decrypted_data++ ^= XOR_KEY_TABLE[current_byte & 0xF];  // 16-byte rolling XOR key
  } while (decrypted_data != (_BYTE *)decrypt_end);
  
  // Create cache entry (24 bytes: original_ptr, decrypted_ptr, next_ptr)
  new_cache_entry = malloc(0x18u);
  bucket_head = (char *)qword_7E6470 + bucket_offset;
  
  new_cache_entry[0] = encrypted_string;      // Original encrypted pointer
  new_cache_entry[1] = buffer + 1;            // Decrypted string pointer
  new_cache_entry[2] = *bucket_head;          // Next entry in bucket
  *bucket_head = new_cache_entry;             // Insert at head of list
  
  return new_cache_entry[1];  // Return pointer to decrypted string
}
```

## XOR Key

**Address**: `0x5A0080`  
**Size**: 16 bytes  
**Name**: `byte_5A0080` (should be renamed to `string_xor_key`)

### Key Bytes

```
Offset  00 01 02 03 04 05 06 07 08 09 0A 0B 0C 0D 0E 0F
Bytes   25 68 D3 C2 28 F2 59 2E 94 EE F2 91 AC 13 96 95
```

### Key as Array

```c
unsigned char string_xor_key[16] = {
    0x25, 0x68, 0xD3, 0xC2, 0x28, 0xF2, 0x59, 0x2E,
    0x94, 0xEE, 0xF2, 0x91, 0xAC, 0x13, 0x96, 0x95
};
```

## Obfuscated String Format

### Structure

```
Byte 0:      Length (N) - number of encrypted bytes that follow
Bytes 1-N:   Encrypted data (XOR'd with key)
Byte N+1:    Null terminator (0x00)
```

### Example

Given an encrypted string at address X:
- `X[0]` = Length (e.g., 0x0A for 10 bytes)
- `X[1]` to `X[10]` = Encrypted characters
- `X[11]` = 0x00 (null terminator)

### Decryption Process

1. Read length byte L at position 0
2. For each byte at position i (from 1 to L):
   - `decrypted[i] = encrypted[i] ^ string_xor_key[(L + i - 1) & 0xF]`
3. The counter starts at L and increments, using only bottom 4 bits for key indexing

## Cache Structure

### Hash Table

**Address**: `qword_7E6470`  
**Type**: `void*` (actually `cache_entry**[4096]`)  
**Purpose**: Hash table with 4096 buckets for string cache

### Cache Entry Structure

```c
struct string_cache_entry {
    unsigned char *encrypted_ptr;    // +0x00: Pointer to original encrypted string
    char *decrypted_ptr;             // +0x08: Pointer to decrypted string
    struct string_cache_entry *next; // +0x10: Next entry in linked list (collision chain)
};
```

**Size**: 24 bytes (0x18)

### Hashing Algorithm

```c
hash_index = ((uintptr_t)encrypted_string >> 3) & 0xFFF;
```

- Shifts address right by 3 (divides by 8)
- Masks with 0xFFF (4095) to get bucket index 0-4095

## Usage Pattern

The function is called extensively throughout the binary, particularly in:
- **Main function** (`0x418300`): Called 100+ times for error messages, paths, and configuration strings
- **Error reporting**: All error/warning messages are obfuscated
- **Configuration**: File paths and option strings
- **License validation**: License-related strings

### Example Calls from Main

```c
v6 = (const char *)decrypt_obfuscated_string(&unk_50D0C6);   // Config file name
v64 = decrypt_obfuscated_string(&unk_50DBF8);                // Error message
v88 = (const char *)decrypt_obfuscated_string(&unk_50DC78);  // Another message
```

## Security Analysis

### Strengths

1. **Caching**: Efficient - each string only decrypted once
2. **Simple but effective**: Hides strings from basic static analysis
3. **Runtime only**: Strings don't exist in plaintext in the binary

### Weaknesses

1. **Weak XOR**: XOR encryption is cryptographically weak
2. **Static key**: The 16-byte key is embedded in the binary at a fixed location
3. **Reversible**: Can be easily reversed with the key
4. **Pattern visible**: The function calls are visible in disassembly
5. **Cache in memory**: Decrypted strings stored in plaintext in process memory

## Deobfuscation Strategy

### Manual Approach

To manually decrypt any obfuscated string:

1. Find the encrypted string address in IDA/Ghidra
2. Read the length byte at offset 0
3. XOR each subsequent byte with `string_xor_key[(counter++) & 0xF]`
4. Start counter at length value

### Automated Approach

```python
def decrypt_string(encrypted_data):
    """
    Decrypt an obfuscated string from the binary.
    
    Args:
        encrypted_data: bytes object starting with length byte
    
    Returns:
        Decrypted string
    """
    xor_key = bytes([
        0x25, 0x68, 0xD3, 0xC2, 0x28, 0xF2, 0x59, 0x2E,
        0x94, 0xEE, 0xF2, 0x91, 0xAC, 0x13, 0x96, 0x95
    ])
    
    length = encrypted_data[0]
    counter = length
    decrypted = bytearray()
    
    for i in range(1, length + 1):
        decrypted_byte = encrypted_data[i] ^ xor_key[counter & 0xF]
        decrypted.append(decrypted_byte)
        counter += 1
    
    return decrypted.decode('utf-8', errors='replace')
```

### IDA Python Script

```python
import idc
import idaapi

XOR_KEY = bytes([
    0x25, 0x68, 0xD3, 0xC2, 0x28, 0xF2, 0x59, 0x2E,
    0x94, 0xEE, 0xF2, 0x91, 0xAC, 0x13, 0x96, 0x95
])

def decrypt_string_at_addr(addr):
    """Decrypt string at given address in IDA."""
    length = idc.get_wide_byte(addr)
    counter = length
    decrypted = []
    
    for i in range(1, length + 1):
        encrypted_byte = idc.get_wide_byte(addr + i)
        decrypted_byte = encrypted_byte ^ XOR_KEY[counter & 0xF]
        decrypted.append(decrypted_byte)
        counter += 1
    
    try:
        result = bytes(decrypted).decode('utf-8')
        print(f"[0x{addr:08x}] {result}")
        
        # Set comment in IDA
        idc.set_cmt(addr, result, 0)
        return result
    except:
        return None

# Find all references to decrypt_obfuscated_string
def decrypt_all_strings():
    """Find and decrypt all obfuscated strings."""
    decrypt_func_addr = 0x4B3B10
    
    for xref in idautils.XrefsTo(decrypt_func_addr):
        # Get the instruction before the call
        prev_addr = idc.prev_head(xref.frm)
        
        # Check if it's loading an address (lea, mov, etc.)
        if idc.print_insn_mnem(prev_addr) in ['lea', 'mov']:
            # Get the operand (string address)
            op_value = idc.get_operand_value(prev_addr, 1)
            if op_value:
                decrypt_string_at_addr(op_value)

# Run it
decrypt_all_strings()
```

## Recommendations for Further Analysis

1. **Extract all strings**: Use the IDA script to decrypt and document all obfuscated strings
2. **Update function names**: Rename functions based on their decrypted error messages
3. **Identify functionality**: Use decrypted strings to understand program features
4. **License strings**: Focus on license-related strings for activation analysis
5. **Configuration options**: Decrypt command-line help and option strings

## Related Functions

- `sub_4B0880` (0x4B0880): Appears to manage license/configuration data structure
- `sub_4B0680` (0x4B0680): Path manipulation related to binary location
- `sub_4B0590` (0x4B0590): Another configuration-related function
- `sub_4B05E0` (0x4B05E0): Time/date related function (possibly license check)

## Summary

The binary employs a straightforward but effective string obfuscation technique:
- **Algorithm**: Rolling XOR with 16-byte key
- **Key location**: `0x5A0080`
- **Decryption function**: `0x4B3B10` (`decrypt_obfuscated_string`)
- **Caching**: Hash table with 4096 buckets prevents re-decryption
- **Usage**: 500+ obfuscated strings throughout the binary

This obfuscation protects strings from casual inspection but can be easily defeated with the XOR key and decryption algorithm documented above.