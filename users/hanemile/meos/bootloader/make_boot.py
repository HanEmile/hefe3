import struct
import re

class MicroAssembler:
    def __init__(self):
        self.code = bytearray()
        self.labels = {}
        self.jumps = []
        self.r16_names = r'ax|cx|dx|bx|sp|bp|si|di'
        self.r8_names  = r'al|cl|dl|bl|ah|ch|dh|bh'
        self.r16 = {'ax':0, 'cx':1, 'dx':2, 'bx':3, 'sp':4, 'bp':5, 'si':6, 'di':7}
        self.r8  = {'al':0, 'cl':1, 'dl':2, 'bl':3, 'ah':4, 'ch':5, 'dh':6, 'bh':7}
        self.sreg = {'es':0, 'cs':1, 'ss':2, 'ds':3}
        self.static = {
            "cli": b'\xFA', "sti": b'\xFB', "cld": b'\xFC', "ret": b'\xC3',
            "pusha": b'\x60', "popa": b'\x61', "stosb": b'\xAA', "lodsb": b'\xAC',
            "rep stosb": b'\xF3\xAA', "rep lodsb": b'\xF3\xAC', "reboot": b'\xEA\x00\x00\xFF\xFF'
        }

    def assemble(self, text):
        for line in text.split('\n'):
            line = line.split(';')[0].strip()
            if not line: continue
            if ':' in line:
                label_part, code_part = line.split(':', 1)
                self.labels[label_part.strip()] = len(self.code)
                line = code_part.strip()
                if not line: continue

            if line in self.static: self.code.extend(self.static[line]); continue

            # Flow Control
            m = re.fullmatch(r'(jmp|jz|jnz|jbe|jc|loop|call)\s+([.\w]+)', line)
            if m:
                op, label = m.groups()
                b = {'jmp':b'\xE9', 'jz':b'\x74', 'jnz':b'\x75', 'jbe':b'\x76', 'jc':b'\x72', 'loop':b'\xE2', 'call':b'\xE8'}[op]
                s = 2 if op in ['jmp', 'call'] else 1
                self.jumps.append((len(self.code), label, b, s))
                self.code.extend(b'\x00' * (s + 1)); continue

            m = re.fullmatch(rf'(push|pop) ({self.r16_names})', line)
            if m:
                op, reg = m.groups(); base = 0x50 if op == 'push' else 0x58
                self.code.append(base + self.r16[reg]); continue

            # Segment Registers: mov ds, ax / mov ss, ax etc
            m = re.fullmatch(rf'mov (ds|es|ss), ({self.r16_names})', line)
            if m:
                sreg_name, r_name = m.groups()
                self.code.extend(bytes([0x8E, 0xC0 | (self.sreg[sreg_name] << 3) | self.r16[r_name]]))
                continue

            # Reg, Imm
            m = re.fullmatch(rf'(mov|cmp|add|sub|xor|and|or) ({self.r16_names}|{self.r8_names}), (0x[0-9A-Fa-f]+)', line)
            if m:
                op, reg, val_str = m.groups(); val = int(val_str, 16); is_16 = reg in self.r16
                if op == 'mov':
                    if is_16: self.code.extend(bytes([0xB8 + self.r16[reg]]) + struct.pack('<H', val))
                    else: self.code.extend(bytes([0xB0 + self.r8[reg], val]))
                else:
                    ext = {'add':0, 'or':1, 'and':4, 'sub':5, 'xor':6, 'cmp':7}[op]
                    if reg == 'ax': self.code.extend(bytes([0x05 | (ext << 3)]) + struct.pack('<H', val))
                    elif reg == 'al': self.code.extend(bytes([0x04 | (ext << 3), val & 0xFF]))
                    else:
                        opcode = 0x81 if is_16 else 0x80
                        self.code.extend(bytes([opcode, 0xC0 | (ext << 3) | (self.r16[reg] if is_16 else self.r8[reg])]))
                        if is_16: self.code.extend(struct.pack('<H', val))
                        else: self.code.append(val & 0xFF)
                continue

            # Absolute Store/Load: mov [addr], al / mov al, [addr]
            m_st = re.fullmatch(rf'mov \[(0x[0-9A-Fa-f]+)\], ({self.r16_names}|{self.r8_names})', line)
            m_ld = re.fullmatch(rf'mov ({self.r16_names}|{self.r8_names}), \[(0x[0-9A-Fa-f]+)\]', line)
            if m_st or m_ld:
                if m_st: addr, reg = int(m_st.group(1), 16), m_st.group(2); op_ax, op_al = b'\xA3', b'\xA2'
                else: reg, addr = m_ld.group(1), int(m_ld.group(2), 16); op_ax, op_al = b'\xA1', b'\xA0'
                if reg == 'ax': self.code.extend(op_ax + struct.pack('<H', addr))
                elif reg == 'al': self.code.extend(op_al + struct.pack('<H', addr))
                continue

            # Reg-to-Reg
            m = re.fullmatch(rf'(mov|add|sub|xor|cmp|and|or) ({self.r16_names}|{self.r8_names}), ({self.r16_names}|{self.r8_names})', line)
            if m:
                op, dst, src = m.groups(); is_16 = dst in self.r16
                base_op = {'mov':0x88, 'add':0x00, 'sub':0x28, 'xor':0x30, 'cmp':0x38, 'and':0x20, 'or':0x08}[op]
                if is_16: base_op |= 0x01
                self.code.extend(bytes([base_op, 0xC0 | ((self.r16[src] if is_16 else self.r8[src]) << 3) | (self.r16[dst] if is_16 else self.r8[dst])]))
                continue

            if line.startswith('int '): self.code.extend(b'\xCD' + struct.pack('B', int(line.split()[1], 16))); continue
            if line.startswith('pad_to '): self.code.extend(b'\x00' * max(0, int(line.split()[1], 16) - (0x7C00 + len(self.code)))); continue
            if line == 'boot_signature': self.code.extend(b'\x00' * (510 - len(self.code)) + b'\x55\xAA'); continue
            raise ValueError(f"Assembler error on: '{line}'")

        for offset, label, op, size in self.jumps:
            target = self.labels[label]; rel = target - (offset + size + 1)
            self.code[offset] = op[0]
            if size == 1: self.code[offset+1] = struct.pack('b', rel)[0]
            else: self.code[offset+1:offset+3] = struct.pack('<h', rel)
        return self.code

os_code = """
cli
xor ax, ax
mov ds, ax
mov es, ax
mov ss, ax
mov sp, 0x7C00
cld

; INIT SERIAL VIA BIOS (9600, 8N1)
mov ah, 0x00
mov al, 0xE3
xor dx, dx
int 0x14

; LOAD STAGE 2
mov ah, 0x02
mov al, 0x04
mov ch, 0x00
mov cl, 0x02
mov dh, 0x00
mov bx, 0x7E00
int 0x13
jmp stage2
boot_signature

stage2:
    mov al, 0x01
    mov [0x8000], al

start_over:
    mov al, 0x0A
    call serial_putc
    mov al, 0x0D
    call serial_putc
    mov al, 0x3E
    call serial_putc

get_cmd:
    call serial_getc
    call serial_putc
    cmp al, 0x56 ; 'V'
    jz do_vga
    cmp al, 0x54 ; 'T'
    jz do_text
    cmp al, 0x46 ; 'F'
    jz do_fill
    jmp get_cmd

do_vga:
    mov ah, 0x00
    mov al, 0x13
    int 0x10
    jmp start_over

do_text:
    mov ah, 0x00
    mov al, 0x03
    int 0x10
    jmp start_over

do_fill:
    mov al, [0x8000]
    xor al, 0x03
    mov [0x8000], al
    mov ax, 0xA000
    mov es, ax
    xor di, di
    mov cx, 0xFA00
    mov al, [0x8000]
    rep stosb
    jmp start_over

serial_putc:
    push dx
    xor dx, dx
    mov ah, 0x01
    int 0x14
    pop dx
    ret

serial_getc:
    push dx
    xor dx, dx
    mov ah, 0x02
    int 0x14
    pop dx
    ret

pad_to 0x8000
boot_signature
"""

ma = MicroAssembler()
binary = ma.assemble(os_code).ljust(512 * 5, b'\x00')
with open("boot.bin", "wb") as f: f.write(binary)
