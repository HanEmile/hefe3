# a sample reproducible battle declaration
battle "sample"
arena 4096
rounds 200
gap 64

bot "looper" arch x86 bits 32 {
    nop
    jmp 0
}

# references one of the real sample bots shipped in ../bots
bot "pancake" arch x86 bits 32 source "../../bots/pancake.x86-32.asm"
