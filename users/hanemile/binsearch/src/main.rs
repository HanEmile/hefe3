use std::fmt;
use std::sync::Arc;

use ahash::AHashMap;
use iced_x86::{Decoder, DecoderOptions, Formatter, Instruction, IntelFormatter};
use object::{Object, ObjectSection};
use rayon::prelude::*;
use smallvec::SmallVec;

// ──────────────────────────────────────────────────────────────────────────────
// String interning
//
// All mnemonic and operand strings are interned to u32 atom IDs so every
// comparison is a single integer compare and no heap allocations happen in the
// hot matcher path.
// ──────────────────────────────────────────────────────────────────────────────

#[derive(Copy, Clone, Debug, PartialEq, Eq, Hash)]
pub(crate) struct Atom(u32);

/// A thread-local intern table built once during disassembly / pattern parsing
/// and then shared (read-only) across all threads.
#[derive(Default)]
pub(crate) struct Interner {
    map: AHashMap<String, Atom>,
    vec: Vec<String>,
}

impl Interner {
    fn intern(&mut self, s: &str) -> Atom {
        if let Some(&a) = self.map.get(s) {
            return a;
        }
        let id = Atom(self.vec.len() as u32);
        self.vec.push(s.to_string());
        self.map.insert(s.to_string(), id);
        id
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Pattern Language
// ──────────────────────────────────────────────────────────────────────────────

#[derive(Debug, Clone, PartialEq)]
enum OperandPattern {
    Wildcard,
    Variable(u32),  // index into a per-match variable slot array
    Literal(Atom),
}

#[derive(Debug, Clone)]
struct InsnPattern {
    /// None = wildcard mnemonic
    mnemonic: Option<Atom>,
    operands: SmallVec<[OperandPattern; 4]>,
}

#[derive(Debug, Clone)]
enum Stmt {
    Positive(InsnPattern),
    Negative(InsnPattern),
    WildcardAny,
    WildcardStar,
}

#[derive(Debug, Clone)]
struct Pattern {
    stmts: Vec<Stmt>,
    /// Number of distinct $variables across all stmts.
    num_vars: u32,
    /// Human-readable variable names (index = slot id).
    var_names: Vec<String>,
    /// Atom of the first positive mnemonic (used for pre-filtering).
    first_mnemonic: Option<Atom>,
}

// ──────────────────────────────────────────────────────────────────────────────
// Parser
// ──────────────────────────────────────────────────────────────────────────────

#[derive(Debug)]
struct ParseError(String);
impl fmt::Display for ParseError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "parse error: {}", self.0)
    }
}

struct PatternParser<'a> {
    interner: &'a mut Interner,
    var_map: AHashMap<String, u32>,
    var_names: Vec<String>,
}

impl<'a> PatternParser<'a> {
    fn new(interner: &'a mut Interner) -> Self {
        Self {
            interner,
            var_map: AHashMap::default(),
            var_names: vec![],
        }
    }

    fn var_slot(&mut self, name: &str) -> u32 {
        if let Some(&id) = self.var_map.get(name) {
            return id;
        }
        let id = self.var_names.len() as u32;
        self.var_map.insert(name.to_string(), id);
        self.var_names.push(name.to_string());
        id
    }

    fn parse_operand(&mut self, s: &str) -> OperandPattern {
        let s = s.trim();
        if s == "_" {
            OperandPattern::Wildcard
        } else if let Some(name) = s.strip_prefix('$') {
            OperandPattern::Variable(self.var_slot(name))
        } else {
            OperandPattern::Literal(self.interner.intern(&s.to_lowercase()))
        }
    }

    fn parse_insn_pattern(&mut self, s: &str) -> Result<InsnPattern, ParseError> {
        let s = s.trim();
        if s == "_" || s == "_*" {
            return Ok(InsnPattern { mnemonic: None, operands: SmallVec::new() });
        }

        let (mnemonic_str, rest) = if let Some(paren) = s.find('(') {
            let m = s[..paren].trim().to_lowercase();
            let inner = s[paren + 1..]
                .trim_end_matches(|c: char| c == ')' || c.is_whitespace());
            (m, Some(inner))
        } else {
            (s.to_lowercase(), None)
        };

        let mnemonic = Some(self.interner.intern(&mnemonic_str));
        let operands = match rest {
            None => SmallVec::new(),
            Some(inner) if inner.trim().is_empty() => SmallVec::new(),
            Some(inner) => inner.split(',').map(|p| self.parse_operand(p)).collect(),
        };

        Ok(InsnPattern { mnemonic, operands })
    }

    fn parse_stmt(&mut self, raw: &str) -> Result<Stmt, ParseError> {
        let raw = raw.trim();
        if raw.is_empty() {
            return Err(ParseError("empty statement".into()));
        }
        if raw == "_*" {
            return Ok(Stmt::WildcardStar);
        }
        if raw == "_" {
            return Ok(Stmt::WildcardAny);
        }
        if let Some(inner) = raw.strip_prefix("not:") {
            return Ok(Stmt::Negative(self.parse_insn_pattern(inner)?));
        }
        let p = self.parse_insn_pattern(raw)?;
        if p.mnemonic.is_none() {
            if raw.ends_with('*') { Ok(Stmt::WildcardStar) } else { Ok(Stmt::WildcardAny) }
        } else {
            Ok(Stmt::Positive(p))
        }
    }
}

fn parse_pattern(input: &str, interner: &mut Interner) -> Result<Pattern, ParseError> {
    let input = input.trim();
    let inner = input
        .strip_prefix('{')
        .and_then(|s| s.strip_suffix('}'))
        .ok_or_else(|| ParseError("pattern must be wrapped in { }".into()))?;

    let mut pp = PatternParser::new(interner);
    let stmts: Vec<Stmt> = inner
        .split(';')
        .filter(|s| !s.trim().is_empty())
        .map(|s| pp.parse_stmt(s))
        .collect::<Result<_, _>>()?;

    let first_mnemonic = stmts.iter().find_map(|s| {
        if let Stmt::Positive(p) = s { p.mnemonic } else { None }
    });

    let num_vars = pp.var_names.len() as u32;
    Ok(Pattern { stmts, num_vars, var_names: pp.var_names, first_mnemonic })
}

// ──────────────────────────────────────────────────────────────────────────────
// Disassembled instruction (intern-based, SmallVec operands)
// ──────────────────────────────────────────────────────────────────────────────

#[derive(Debug, Clone)]
struct DisasmInsn {
    offset: u64,
    mnemonic: Atom,
    operands: SmallVec<[Atom; 4]>,
    raw_text: String,
}

fn disassemble_bytes(
    bytes: &[u8],
    base_addr: u64,
    bitness: u32,
    interner: &mut Interner,
) -> Vec<DisasmInsn> {
    let mut decoder = Decoder::with_ip(bitness, bytes, base_addr, DecoderOptions::NONE);
    let mut formatter = IntelFormatter::new();
    formatter.options_mut().set_space_after_operand_separator(true);
    formatter.options_mut().set_first_operand_char_index(8);

    let mut insns = Vec::with_capacity(bytes.len() / 4);
    let mut insn = Instruction::default();
    let mut output = String::new();

    while decoder.can_decode() {
        decoder.decode_out(&mut insn);
        output.clear();
        formatter.format(&insn, &mut output);

        let raw = output.trim().to_string();
        let parts: Vec<&str> = raw.splitn(2, char::is_whitespace).collect();
        let mnemonic_str = parts[0].trim().to_lowercase();
        let operand_str = parts.get(1).map(|s| s.trim()).unwrap_or("");

        let mnemonic = interner.intern(&mnemonic_str);
        let operands: SmallVec<[Atom; 4]> = if operand_str.is_empty() {
            SmallVec::new()
        } else {
            operand_str
                .split(',')
                .map(|s| interner.intern(s.trim()))
                .collect()
        };

        insns.push(DisasmInsn {
            offset: insn.ip(),
            mnemonic,
            operands,
            raw_text: raw,
        });
    }

    insns
}

// ──────────────────────────────────────────────────────────────────────────────
// Bindings — zero-allocation inline variable store
//
// Patterns rarely have more than 8 variables.  We store bindings as a fixed
// array of Option<Atom> indexed by variable slot id, living entirely on the
// stack.  Cloning is a memcpy of a tiny struct.
// ──────────────────────────────────────────────────────────────────────────────

const MAX_VARS: usize = 16;

#[derive(Clone, Copy)]
pub(crate) struct Bindings {
    slots: [Option<Atom>; MAX_VARS],
}

impl Bindings {
    fn new(num_vars: u32) -> Self {
        debug_assert!(num_vars as usize <= MAX_VARS, "too many pattern variables (max {MAX_VARS})");
        Bindings {
            slots: [None; MAX_VARS],
        }
    }

    #[inline(always)]
    fn get(&self, slot: u32) -> Option<Atom> {
        self.slots[slot as usize]
    }

    #[inline(always)]
    fn set(&mut self, slot: u32, val: Atom) {
        self.slots[slot as usize] = Some(val);
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Matcher
// ──────────────────────────────────────────────────────────────────────────────

#[inline(always)]
fn operand_matches(pat: &OperandPattern, actual: Atom, b: &mut Bindings) -> bool {
    match pat {
        OperandPattern::Wildcard => true,
        OperandPattern::Literal(lit) => actual == *lit,
        OperandPattern::Variable(slot) => {
            match b.get(*slot) {
                Some(bound) => bound == actual,
                None => { b.set(*slot, actual); true }
            }
        }
    }
}

#[inline(always)]
fn insn_matches(pat: &InsnPattern, insn: &DisasmInsn, b: &mut Bindings) -> bool {
    if let Some(m) = pat.mnemonic {
        if m != insn.mnemonic { return false; }
    }
    if pat.operands.len() > insn.operands.len() {
        return false;
    }
    let mut tmp = *b;
    for (op_pat, &actual) in pat.operands.iter().zip(insn.operands.iter()) {
        if !operand_matches(op_pat, actual, &mut tmp) {
            return false;
        }
    }
    *b = tmp;
    true
}

#[inline(always)]
fn negative_matches(pat: &InsnPattern, insn: &DisasmInsn, b: &Bindings) -> bool {
    let mut tmp = *b;
    insn_matches(pat, insn, &mut tmp)
}

/// Match result: (start_offset, end_offset, bindings).
type MatchResult = (u64, u64, Bindings);

/// Recursive descent matcher — all stack-allocated, no heap traffic.
fn search(
    stmts: &[Stmt],
    insns: &[DisasmInsn],
    b: &Bindings,
    start_offset: u64,
    results: &mut Vec<MatchResult>,
) {
    if stmts.is_empty() {
        let end = insns.first().map(|i| i.offset).unwrap_or(start_offset);
        results.push((start_offset, end, *b));
        return;
    }
    if insns.is_empty() {
        return;
    }

    match &stmts[0] {
        Stmt::WildcardAny => {
            search(&stmts[1..], &insns[1..], b, start_offset, results);
        }

        Stmt::WildcardStar => {
            for skip in 0..=insns.len() {
                search(&stmts[1..], &insns[skip..], b, start_offset, results);
            }
        }

        Stmt::Positive(pat) => {
            let mut nb = *b;
            if insn_matches(pat, &insns[0], &mut nb) {
                search(&stmts[1..], &insns[1..], &nb, start_offset, results);
            }
        }

        Stmt::Negative(neg_pat) => {
            // Collect all consecutive negatives.
            let neg_end = stmts[1..]
                .iter()
                .position(|s| !matches!(s, Stmt::Negative(_)))
                .unwrap_or(stmts.len() - 1);

            // neg_patterns: current + any immediately following negatives
            // We store them as a small stack array to avoid Vec allocation.
            let mut neg_buf: SmallVec<[&InsnPattern; 8]> = SmallVec::new();
            neg_buf.push(neg_pat);
            for s in &stmts[1..=neg_end] {
                if let Stmt::Negative(np) = s {
                    neg_buf.push(np);
                }
            }

            let after_negs = &stmts[1 + neg_end..];

            for gap in 0..insns.len() {
                let candidate = &insns[gap];
                let violated = neg_buf.iter().any(|np| negative_matches(np, candidate, b));
                if violated { break; }
                search(after_negs, &insns[gap + 1..], b, start_offset, results);
            }
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Candidate index
//
// Maps each mnemonic atom to the sorted list of instruction indices that have
// that mnemonic.  The scan loop only starts at positions where the pattern's
// first positive mnemonic appears.
// ──────────────────────────────────────────────────────────────────────────────

fn build_mnemonic_index(insns: &[DisasmInsn]) -> AHashMap<Atom, Vec<usize>> {
    let mut idx: AHashMap<Atom, Vec<usize>> = AHashMap::default();
    for (i, insn) in insns.iter().enumerate() {
        idx.entry(insn.mnemonic).or_default().push(i);
    }
    idx
}

// ──────────────────────────────────────────────────────────────────────────────
// Parallel scan
//
// Strategy:
//   1. Build the mnemonic index to get candidate start positions.
//   2. Split candidates into CHUNK_SIZE groups.
//   3. Process each group in parallel with Rayon.
//   4. Merge & dedup results.
//
// Window-boundary matches (where a match crosses a chunk boundary) are not
// a concern here because each candidate start position is independent — the
// full instruction slice is passed to every worker as an Arc-shared reference.
// ──────────────────────────────────────────────────────────────────────────────

const CHUNK_SIZE: usize = 4096;

fn scan(insns: &Arc<[DisasmInsn]>, pattern: &Pattern) -> Vec<MatchResult> {
    // Gather candidate start indices.
    let candidates: Vec<usize> = match pattern.first_mnemonic {
        Some(m) => {
            let idx = build_mnemonic_index(insns);
            match idx.get(&m) {
                Some(v) => v.clone(),
                None => return vec![],
            }
        }
        None => (0..insns.len()).collect(),
    };

    if candidates.is_empty() {
        return vec![];
    }

    let num_vars = pattern.num_vars;

    // Parallel scan over candidate chunks.
    let mut results: Vec<MatchResult> = candidates
        .par_chunks(CHUNK_SIZE)
        .flat_map(|chunk| {
            let mut local: Vec<MatchResult> = Vec::new();
            for &start in chunk {
                let b = Bindings::new(num_vars);
                search(
                    &pattern.stmts,
                    &insns[start..],
                    &b,
                    insns[start].offset,
                    &mut local,
                );
            }
            local
        })
        .collect();

    results.sort_unstable_by_key(|(s, e, _)| (*s, *e));
    results.dedup_by_key(|(s, e, _)| (*s, *e));
    results
}

// ──────────────────────────────────────────────────────────────────────────────
// Binary loading
// ──────────────────────────────────────────────────────────────────────────────

fn load_executable_sections(
    binary: &[u8],
) -> Result<Vec<(u64, Vec<u8>)>, Box<dyn std::error::Error>> {
    let obj = object::File::parse(binary)?;
    let mut sections = vec![];

    for section in obj.sections() {
        let flags = section.flags();
        let is_exec = match flags {
            object::SectionFlags::Elf { sh_flags } => sh_flags & 0x4 != 0,
            object::SectionFlags::MachO { flags }  => flags & 0x80000000 != 0,
            object::SectionFlags::Coff { characteristics } => characteristics & 0x20000000 != 0,
            _ => false,
        };
        if is_exec {
            if let Ok(data) = section.data() {
                sections.push((section.address(), data.to_vec()));
            }
        }
    }

    if sections.is_empty() {
        for section in obj.sections() {
            if let Ok(name) = section.name() {
                if name == ".text" || name == "__text" {
                    if let Ok(data) = section.data() {
                        sections.push((section.address(), data.to_vec()));
                    }
                }
            }
        }
    }

    Ok(sections)
}

fn detect_bitness(binary: &[u8]) -> u32 {
    if binary.len() >= 5 && &binary[..4] == b"\x7fELF" {
        match binary[4] { 1 => 32, 2 => 64, _ => 64 }
    } else {
        64
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Output helpers
// ──────────────────────────────────────────────────────────────────────────────


fn print_match(
    insns: &[DisasmInsn],
    start: u64,
    end: u64,
    b: &Bindings,
    pattern: &Pattern,
    interner: &Interner,
    binary_path: &str,
) {
    println!("\n[MATCH] {}  0x{:x} – 0x{:x}", binary_path, start, end);
    for (slot, name) in pattern.var_names.iter().enumerate() {
        if let Some(atom) = b.get(slot as u32) {
            println!("  ${} = {}", name, interner.vec[atom.0 as usize]);
        }
    }
    println!("  Instructions:");
    for insn in insns {
        if insn.offset >= start && (insn.offset < end || end == start) {
            println!("    0x{:016x}  {}", insn.offset, insn.raw_text);
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// main
// ──────────────────────────────────────────────────────────────────────────────

fn main() {
    let args: Vec<String> = std::env::args().collect();

    if args.len() < 3 {
        eprintln!(
            "Usage: {} '<pattern>' <binary>

Pattern syntax (weggli-style for binary):
  {{ STMT; STMT; ... }}

Statements:
  mnemonic(op, op, ...)   positive match
  not:mnemonic(op, ...)   negative constraint
  _                        wildcard (any single instruction)
  _*                       wildcard (zero or more instructions)

Operands:
  $var    pattern variable (first use binds, subsequent enforce equality)
  _       wildcard operand
  literal exact match (e.g. rax, rbx, 0x10)

Examples:
  # Use-after-free analogue
  '{{ call(free,$ptr); not:mov($ptr,_); not:call(free,$ptr); call(_,$ptr); }}'

  # Push/pop same register without intervening push
  '{{ push($r); not:push(_); pop($r); }}'

  # Stack pivot
  '{{ mov(rsp,$x); ret; }}'
",
            args[0]
        );
        std::process::exit(1);
    }

    let pattern_str = &args[1];
    let binary_path = &args[2];

    let mut interner = Interner::default();

    let pattern = match parse_pattern(pattern_str, &mut interner) {
        Ok(p) => p,
        Err(e) => { eprintln!("Error parsing pattern: {}", e); std::process::exit(1); }
    };

    eprintln!("Pattern: {} stmts, {} variable(s), first_mnemonic={:?}",
        pattern.stmts.len(), pattern.num_vars,
        pattern.first_mnemonic.map(|a| &interner.vec[a.0 as usize]));

    let binary = match std::fs::read(binary_path) {
        Ok(b) => b,
        Err(e) => { eprintln!("Failed to read '{}': {}", binary_path, e); std::process::exit(1); }
    };

    let bitness = detect_bitness(&binary);
    eprintln!("Detected bitness: {}", bitness);

    let sections = match load_executable_sections(&binary) {
        Ok(s) => s,
        Err(e) => { eprintln!("Failed to parse binary: {}", e); std::process::exit(1); }
    };

    if sections.is_empty() {
        eprintln!("No executable sections found.");
        std::process::exit(1);
    }

    eprintln!("Scanning {} executable section(s)…\n", sections.len());

    let mut total_matches = 0usize;

    for (base_addr, data) in &sections {
        let insns: Arc<[DisasmInsn]> =
            Arc::from(disassemble_bytes(data, *base_addr, bitness, &mut interner));

        eprintln!("  Section @ 0x{:x}: {} instructions", base_addr, insns.len());

        let t0 = std::time::Instant::now();
        let matches = scan(&insns, &pattern);
        let elapsed = t0.elapsed();

        eprintln!("  → {} match(es) in {:.3}ms ({} insns/µs)",
            matches.len(),
            elapsed.as_secs_f64() * 1000.0,
            if elapsed.as_micros() > 0 { insns.len() as u128 / elapsed.as_micros() } else { 0 });

        total_matches += matches.len();
        for (start, end, ref b) in &matches {
            print_match(&insns, *start, *end, b, &pattern, &interner, binary_path);
        }
    }

    println!("\n{} match(es) found in '{}'.", total_matches, binary_path);
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    fn setup(src: &[(&str, &[&str])]) -> (Interner, Arc<[DisasmInsn]>) {
        let mut interner = Interner::default();
        let insns: Arc<[DisasmInsn]> = Arc::from(
            src.iter()
                .enumerate()
                .map(|(i, (m, ops))| DisasmInsn {
                    offset: i as u64 * 4,
                    mnemonic: interner.intern(m),
                    operands: ops.iter().map(|s| interner.intern(s)).collect(),
                    raw_text: format!("{} {}", m, ops.join(", ")),
                })
                .collect::<Vec<_>>(),
        );
        (interner, insns)
    }

    #[test]
    fn test_simple_positive() {
        let (mut interner, insns) = setup(&[
            ("push", &["rbp"]),
            ("mov",  &["rbp", "rsp"]),
            ("pop",  &["rbp"]),
        ]);
        let p = parse_pattern("{ mov($a, $b); }", &mut interner).unwrap();
        let m = scan(&insns, &p);
        assert_eq!(m.len(), 1);
        let (_, _, b) = m[0];
        assert_eq!(interner.vec[b.get(0).unwrap().0 as usize], "rbp");
        assert_eq!(interner.vec[b.get(1).unwrap().0 as usize], "rsp");
    }

    #[test]
    fn test_variable_reuse() {
        let (mut interner, insns) = setup(&[
            ("push", &["rax"]),
            ("pop",  &["rax"]),
            ("push", &["rbx"]),
            ("pop",  &["rcx"]),
        ]);
        let p = parse_pattern("{ push($r); pop($r); }", &mut interner).unwrap();
        let m = scan(&insns, &p);
        assert_eq!(m.len(), 1);
        let (_, _, b) = m[0];
        assert_eq!(interner.vec[b.get(0).unwrap().0 as usize], "rax");
    }

    #[test]
    fn test_negative_constraint() {
        let (mut interner, insns) = setup(&[
            ("push", &["rax"]),
            ("mov",  &["rbx", "rcx"]),
            ("pop",  &["rax"]),
            ("push", &["rdx"]),
            ("push", &["rdx"]),
            ("pop",  &["rdx"]),
        ]);
        let p = parse_pattern("{ push($r); not:push(_); pop($r); }", &mut interner).unwrap();
        let m = scan(&insns, &p);
        assert_eq!(m.len(), 1);
        let (_, _, b) = m[0];
        assert_eq!(interner.vec[b.get(0).unwrap().0 as usize], "rax");
    }

    #[test]
    fn test_wildcard_star() {
        let (mut interner, insns) = setup(&[
            ("push", &["rbp"]),
            ("nop",  &[]),
            ("nop",  &[]),
            ("nop",  &[]),
            ("ret",  &[]),
        ]);
        let p = parse_pattern("{ push(_); _*; ret; }", &mut interner).unwrap();
        let m = scan(&insns, &p);
        assert!(!m.is_empty());
    }

    #[test]
    fn test_parse_not() {
        let mut interner = Interner::default();
        let p = parse_pattern(
            "{ call(free,$ptr); not:mov($ptr,_); call(_,$ptr); }",
            &mut interner,
        ).unwrap();
        assert_eq!(p.stmts.len(), 3);
        assert!(matches!(p.stmts[1], Stmt::Negative(_)));
    }

    #[test]
    fn test_mnemonic_index_prune() {
        // Pattern starts with "xor" — none in the insn stream.
        // scan must return empty without iterating anything.
        let (mut interner, insns) = setup(&[
            ("push", &["rax"]),
            ("mov",  &["rbx", "rcx"]),
            ("pop",  &["rax"]),
        ]);
        let p = parse_pattern("{ xor($a,$b); }", &mut interner).unwrap();
        let m = scan(&insns, &p);
        assert!(m.is_empty());
    }

    // ── 1. Atom interning: same string interns to same atom ──────────────────
    #[test]
    fn test_intern_identity() {
        let mut interner = Interner::default();
        let a1 = interner.intern("rax");
        let a2 = interner.intern("rax");
        let a3 = interner.intern("rbx");
        assert_eq!(a1, a2, "same string must yield same atom");
        assert_ne!(a1, a3, "different strings must yield different atoms");
    }

    // ── 2. Operand wildcard matches anything ─────────────────────────────────
    #[test]
    fn test_operand_wildcard_matches_any() {
        let (mut interner, insns) = setup(&[
            ("mov", &["rax", "rbx"]),
            ("mov", &["rcx", "rdx"]),
            ("mov", &["r8",  "r9" ]),
        ]);
        // mov(_, _) should match all three instructions
        let p = parse_pattern("{ mov(_,_); }", &mut interner).unwrap();
        let m = scan(&insns, &p);
        assert_eq!(m.len(), 3);
    }

    // ── 3. Literal operand rejects non-matching value ────────────────────────
    #[test]
    fn test_literal_operand_no_match() {
        let (mut interner, insns) = setup(&[
            ("mov", &["rax", "rbx"]),
        ]);
        let p = parse_pattern("{ mov(rcx,_); }", &mut interner).unwrap();
        let m = scan(&insns, &p);
        assert!(m.is_empty(), "literal rcx should not match rax");
    }

    // ── 4. Multiple consecutive negatives all enforced ───────────────────────
    #[test]
    fn test_multiple_negatives_all_enforced() {
        // Gap: mov rbx,1  → violates not:mov
        // Gap: add rbx,1  → violates not:add
        // Only the clean pair (push rax / pop rax with just nop in between)
        // should match.
        let (mut interner, insns) = setup(&[
            ("push", &["rax"]),
            ("nop",  &[]),
            ("pop",  &["rax"]),
            // second pair: gap contains mov → blocked by not:mov
            ("push", &["rbx"]),
            ("mov",  &["rcx", "rdx"]),
            ("pop",  &["rbx"]),
            // third pair: gap contains add → blocked by not:add
            ("push", &["rcx"]),
            ("add",  &["rcx", "1"]),
            ("pop",  &["rcx"]),
        ]);
        let p = parse_pattern(
            "{ push($r); not:mov(_,_); not:add(_,_); pop($r); }",
            &mut interner,
        ).unwrap();
        let m = scan(&insns, &p);
        assert_eq!(m.len(), 1);
        assert_eq!(interner.vec[m[0].2.get(0).unwrap().0 as usize], "rax");
    }

    // ── 5. WildcardAny consumes exactly one instruction ───────────────────────
    #[test]
    fn test_wildcard_any_consumes_exactly_one() {
        let (mut interner, insns) = setup(&[
            ("push", &["rax"]),
            ("nop",  &[]),
            ("pop",  &["rax"]),
        ]);
        // _ matches nop, then pop must immediately follow
        let p = parse_pattern("{ push($r); _; pop($r); }", &mut interner).unwrap();
        let m = scan(&insns, &p);
        assert_eq!(m.len(), 1);
    }

    // ── 6. WildcardAny does NOT skip two instructions ─────────────────────────
    #[test]
    fn test_wildcard_any_does_not_skip_two() {
        let (mut interner, insns) = setup(&[
            ("push", &["rax"]),
            ("nop",  &[]),
            ("nop",  &[]),
            ("pop",  &["rax"]),
        ]);
        // Only one _ between push and pop — should not match (two nops in gap)
        let p = parse_pattern("{ push($r); _; pop($r); }", &mut interner).unwrap();
        let m = scan(&insns, &p);
        assert!(m.is_empty(), "single _ should not bridge two instructions");
    }

    // ── 7. WildcardStar matches zero instructions ─────────────────────────────
    #[test]
    fn test_wildcard_star_zero() {
        let (mut interner, insns) = setup(&[
            ("push", &["rbp"]),
            ("ret",  &[]),
        ]);
        let p = parse_pattern("{ push(_); _*; ret; }", &mut interner).unwrap();
        let m = scan(&insns, &p);
        assert!(!m.is_empty(), "_* should match even with zero intervening insns");
    }

    // ── 8. WildcardStar matches many instructions ─────────────────────────────
    #[test]
    fn test_wildcard_star_many() {
        let filler: Vec<(&str, &[&str])> = (0..50).map(|_| ("nop", [].as_slice())).collect();
        let mut src = vec![("push", ["rbp"].as_slice())];
        src.extend(filler.iter().map(|(m, ops)| (*m, *ops)));
        src.push(("ret", &[]));

        let (mut interner, insns) = setup(&src);
        let p = parse_pattern("{ push(_); _*; ret; }", &mut interner).unwrap();
        let m = scan(&insns, &p);
        assert!(!m.is_empty());
    }

    // ── 9. Variable binding is isolated per match ─────────────────────────────
    #[test]
    fn test_variable_binding_per_match() {
        // Two independent push/pop pairs; each match binds $r independently.
        let (mut interner, insns) = setup(&[
            ("push", &["rax"]),
            ("pop",  &["rax"]),
            ("push", &["rbx"]),
            ("pop",  &["rbx"]),
        ]);
        let p = parse_pattern("{ push($r); pop($r); }", &mut interner).unwrap();
        let m = scan(&insns, &p);
        assert_eq!(m.len(), 2);
        let v0 = &interner.vec[m[0].2.get(0).unwrap().0 as usize];
        let v1 = &interner.vec[m[1].2.get(0).unwrap().0 as usize];
        assert_eq!(v0, "rax");
        assert_eq!(v1, "rbx");
    }

    // ── 10. Candidate index prunes all starts when mnemonic absent ────────────
    #[test]
    fn test_candidate_index_full_prune() {
        let (mut interner, insns) = setup(&[
            ("add", &["rax", "1"]),
            ("sub", &["rbx", "2"]),
            ("mul", &["rcx"]),
        ]);
        let p = parse_pattern("{ div($a); }", &mut interner).unwrap();
        let m = scan(&insns, &p);
        assert!(m.is_empty());
        // Also verify the first_mnemonic was set so the prune actually fired
        assert!(p.first_mnemonic.is_some());
    }

    // ── 11. Candidate index starts only at correct positions ──────────────────
    #[test]
    fn test_candidate_index_correct_starts() {
        let (mut interner, insns) = setup(&[
            ("nop",  &[]),
            ("call", &["free", "rdi"]),
            ("nop",  &[]),
            ("call", &["free", "rdi"]),
            ("nop",  &[]),
        ]);
        // Pattern: call(free,_) — should match exactly at positions 1 and 3
        let p = parse_pattern("{ call(free,_); }", &mut interner).unwrap();
        let m = scan(&insns, &p);
        assert_eq!(m.len(), 2);
        assert_eq!(m[0].0, 4);   // offset = index*4
        assert_eq!(m[1].0, 12);
    }

    // ── 12. Match offsets are correct ─────────────────────────────────────────
    #[test]
    fn test_match_offsets() {
        let (mut interner, insns) = setup(&[
            ("nop",  &[]),        // offset 0
            ("push", &["rax"]),   // offset 4
            ("pop",  &["rax"]),   // offset 8
            ("nop",  &[]),        // offset 12
        ]);
        let p = parse_pattern("{ push($r); pop($r); }", &mut interner).unwrap();
        let m = scan(&insns, &p);
        assert_eq!(m.len(), 1);
        assert_eq!(m[0].0, 4,  "start offset should be 4");
        assert_eq!(m[0].1, 12, "end offset should be 12 (instruction after pop)");
    }

    // ── 13. Parallel scan (rayon) produces same results as expected ───────────
    #[test]
    fn test_parallel_scan_correctness() {
        // Build a large instruction stream with a known number of push/pop pairs
        let mut src = Vec::new();
        for _ in 0..200 {
            src.push(("push", ["rax"].as_slice()));
            src.push(("nop",  [].as_slice()));
            src.push(("pop",  ["rax"].as_slice()));
            // filler that should NOT match
            src.push(("push", ["rbx"].as_slice()));
            src.push(("push", ["rbx"].as_slice())); // violates not:push
            src.push(("pop",  ["rbx"].as_slice()));
        }
        let (mut interner, insns) = setup(&src);
        let p = parse_pattern(
            "{ push($r); not:push(_); pop($r); }",
            &mut interner,
        ).unwrap();
        let m = scan(&insns, &p);
        // Each of the 200 clean pairs should produce exactly 1 match
        assert_eq!(m.len(), 200);
    }

    // ── 14. Security: use-after-free pattern ─────────────────────────────────
    #[test]
    fn test_uaf_pattern() {
        let (mut interner, insns) = setup(&[
            ("call", &["free", "rdi"]),   // free($ptr) with ptr=rdi
            ("nop",  &[]),
            ("call", &["memcpy", "rdi"]), // use of rdi after free
        ]);
        let p = parse_pattern(
            "{ call(free,$ptr); not:mov($ptr,_); not:lea($ptr,_); call(_,$ptr); }",
            &mut interner,
        ).unwrap();
        let m = scan(&insns, &p);
        assert_eq!(m.len(), 1);
    }

    // ── 15. Security: UAF blocked by reassignment ─────────────────────────────
    #[test]
    fn test_uaf_blocked_by_reassignment() {
        let (mut interner, insns) = setup(&[
            ("call", &["free", "rdi"]),
            ("mov",  &["rdi", "rsi"]),    // rdi is reassigned → not a UAF
            ("call", &["memcpy", "rdi"]),
        ]);
        let p = parse_pattern(
            "{ call(free,$ptr); not:mov($ptr,_); call(_,$ptr); }",
            &mut interner,
        ).unwrap();
        let m = scan(&insns, &p);
        assert!(m.is_empty(), "reassignment should block the UAF pattern");
    }

    // ── 16. Security: double-free detected ───────────────────────────────────
    #[test]
    fn test_double_free_detected() {
        let (mut interner, insns) = setup(&[
            ("call", &["free", "rdi"]),
            ("nop",  &[]),
            ("call", &["free", "rdi"]),
        ]);
        let p = parse_pattern(
            "{ call(free,$ptr); not:mov($ptr,_); not:lea($ptr,_); call(free,$ptr); }",
            &mut interner,
        ).unwrap();
        let m = scan(&insns, &p);
        assert_eq!(m.len(), 1);
    }

    // ── 17. Security: double-free blocked by lea reassignment ────────────────
    #[test]
    fn test_double_free_blocked_by_lea() {
        let (mut interner, insns) = setup(&[
            ("call", &["free", "rdi"]),
            ("lea",  &["rdi", "new_buf"]), // rdi re-pointed → not a double-free
            ("call", &["free", "rdi"]),
        ]);
        let p = parse_pattern(
            "{ call(free,$ptr); not:mov($ptr,_); not:lea($ptr,_); call(free,$ptr); }",
            &mut interner,
        ).unwrap();
        let m = scan(&insns, &p);
        assert!(m.is_empty());
    }

    // ── 18. Security: stack pivot gadget ──────────────────────────────────────
    #[test]
    fn test_stack_pivot_gadget() {
        let (mut interner, insns) = setup(&[
            ("nop",  &[]),
            ("pop",  &["rsp"]),
            ("ret",  &[]),
            ("nop",  &[]),
        ]);
        let p = parse_pattern("{ pop(rsp); ret; }", &mut interner).unwrap();
        let m = scan(&insns, &p);
        assert_eq!(m.len(), 1);
    }

    // ── 19. Security: null-deref after malloc without check ───────────────────
    #[test]
    fn test_null_deref_after_malloc() {
        let (mut interner, insns) = setup(&[
            ("call", &["malloc", "rdi"]),
            ("nop",  &[]),
            // no test/cmp/je here
            ("mov",  &["rcx", "[rax]"]),  // immediate deref of rax
        ]);
        let p = parse_pattern(
            "{ call(malloc,$size); not:test(rax,rax); not:cmp(rax,_); not:je(_); mov(_,[rax]); }",
            &mut interner,
        ).unwrap();
        let m = scan(&insns, &p);
        assert_eq!(m.len(), 1);
    }

    // ── 20. Security: null-deref blocked by null check ────────────────────────
    #[test]
    fn test_null_deref_blocked_by_check() {
        let (mut interner, insns) = setup(&[
            ("call", &["malloc", "rdi"]),
            ("test", &["rax", "rax"]),    // null check present
            ("je",   &["error_label"]),
            ("mov",  &["rcx", "[rax]"]),
        ]);
        let p = parse_pattern(
            "{ call(malloc,$size); not:test(rax,rax); not:cmp(rax,_); not:je(_); mov(_,[rax]); }",
            &mut interner,
        ).unwrap();
        let m = scan(&insns, &p);
        assert!(m.is_empty(), "null check should block the pattern");
    }
}
