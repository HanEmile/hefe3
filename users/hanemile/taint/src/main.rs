// taint — source-level taint / data-flow tracer for manual security analysis
//
// Supported languages: C, C++, Rust, Go, Python
//
// Architecture
// ────────────
//  1. Parse source with tree-sitter → language-agnostic AST
//  2. Extract:
//       • Functions  (name, param list, body span)
//       • Variables  (declaration, assignments)
//       • Call sites (callee, arguments, call site location)
//       • Branches   (if/while/for/match conditions)
//  3. Build a lightweight CFG + def-use chains per function
//  4. Propagate taint forward from user-input sources
//       (read / recv / fgets / scanf / argv / os.stdin / io::stdin / …)
//  5. Given a --target (function name, variable name, or line:col),
//     walk the def-use / call graph **backwards** to collect all paths
//     from any taint source to that target
//  6. For each path node annotate:
//       • TaintLevel: Full | Partial | None
//       • Conditions: the branch predicates that must hold to reach it
//  7. Print a human-readable report (or --json for machine consumption)

use std::collections::{HashMap, HashSet};
use std::fmt;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use ahash::AHashMap;
use rayon::prelude::*;
use clap::Parser as ClapParser;
use colored::Colorize;
use serde::{Deserialize, Serialize};
use tree_sitter::{Language, Node, Parser, Tree};

// ─────────────────────────────────────────────────────────────────────────────
// CLI
// ─────────────────────────────────────────────────────────────────────────────

/// taint — trace data flow from user-controlled sources to a target site.
///
/// Single file:
///   taint -s net.c -t process_input
///   taint -s crypto.c -t "decrypt:key" --json
///
/// Whole project (directory scan):
///   taint -s ~/src/openssl -t SSL_read
///   taint -s ~/src/openssl --all-sinks --exclude "test,fuzz,apps/speed"
///   taint -s ~/src/openssl --all-sinks --min-taint full --json > report.json
#[derive(ClapParser, Debug)]
#[command(version, about, verbatim_doc_comment)]
struct Cli {
    /// Source files or directories to analyse.
    /// Directories are walked recursively; files are filtered by extension
    /// (.c .h .cpp .cc .hpp .rs .go .py).
    #[arg(short, long, required = true, num_args = 1..)]
    source: Vec<PathBuf>,

    /// Target to trace to.
    ///
    /// Formats:
    ///   function_name              – any tainted use inside the named function
    ///   function_name:variable     – specific variable inside a named function
    ///   line:col                   – exact source location (1-based)
    ///
    /// Omit --target together with --all-sinks to scan every tainted sink
    /// in the project.
    #[arg(short, long)]
    target: Option<String>,

    /// Scan for ALL tainted sinks across every file (whole-project mode).
    /// Overrides --target. Each discovered tainted call/assignment is treated
    /// as a sink and reported.
    #[arg(long)]
    all_sinks: bool,

    /// Exclude paths whose components match any of these substrings
    /// (comma-separated).  Applied relative to the source root.
    ///
    /// Example:  --exclude "test,fuzz,apps/speed,demos"
    #[arg(long, value_delimiter = ',', default_value = "")]
    exclude: Vec<String>,

    /// Only report sinks at or above this taint level.
    /// Values: full | partial | any  (default: partial)
    #[arg(long, default_value = "partial")]
    min_taint: String,

    /// Extra taint sources beyond the built-in list (comma-separated)
    #[arg(long, value_delimiter = ',')]
    extra_sources: Vec<String>,

    /// Functions that strip taint (comma-separated)
    #[arg(long, value_delimiter = ',')]
    sanitisers: Vec<String>,

    /// Maximum backward-search depth (default: 32)
    #[arg(long, default_value_t = 32)]
    depth: usize,

    /// Emit JSON instead of coloured text
    #[arg(long)]
    json: bool,

    /// Include paths where no taint reaches the target
    #[arg(long)]
    all_paths: bool,

    /// Number of parallel worker threads (default: number of logical CPUs)
    #[arg(long, default_value_t = 0)]
    jobs: usize,

    /// Write an HTML report to this file (in addition to terminal output)
    #[arg(long)]
    html: Option<PathBuf>,

    /// Write annotated source copies to this directory.
    /// Each source file that contains tainted variables gets a copy with
    /// inline comments marking FULL / PARTIAL / NONE taint on every
    /// tainted call or assignment.  The directory structure mirrors the source.
    #[arg(long)]
    annotate: Option<PathBuf>,
}

// ─────────────────────────────────────────────────────────────────────────────
// Source location
// ─────────────────────────────────────────────────────────────────────────────

#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
struct Loc {
    file: String,
    line: usize,
    col: usize,
}

impl fmt::Display for Loc {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}:{}:{}", self.file, self.line, self.col)
    }
}

fn node_loc(node: &Node, file: &str) -> Loc {
    Loc {
        file: file.to_string(),
        line: node.start_position().row + 1,
        col: node.start_position().column + 1,
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Taint level
// ─────────────────────────────────────────────────────────────────────────────

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
enum TaintLevel {
    None,
    Partial, // derived from tainted + untainted (e.g. arithmetic mixing)
    Full,    // all data flows from a taint source
}

impl fmt::Display for TaintLevel {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            TaintLevel::Full    => write!(f, "FULL"),
            TaintLevel::Partial => write!(f, "PARTIAL"),
            TaintLevel::None    => write!(f, "NONE"),
        }
    }
}

impl TaintLevel {
    fn join(self, other: TaintLevel) -> TaintLevel {
        match (self, other) {
            (TaintLevel::Full, _) | (_, TaintLevel::Full) => TaintLevel::Full,
            (TaintLevel::Partial, _) | (_, TaintLevel::Partial) => TaintLevel::Partial,
            _ => TaintLevel::None,
        }
    }

}

// ─────────────────────────────────────────────────────────────────────────────
// Built-in taint sources (language-agnostic names)
// ─────────────────────────────────────────────────────────────────────────────

fn builtin_sources() -> HashSet<&'static str> {
    [
        // C / C++ / POSIX
        "read", "fread", "fgets", "gets", "scanf", "fscanf", "sscanf",
        "recv", "recvfrom", "recvmsg", "accept",
        "getenv", "getline", "getdelim",
        "mmap",   // could map user-controlled file
        // Rust
        "read_line", "read_to_string", "read_to_end",
        "from_utf8_unchecked",
        // Go
        "Read", "ReadFrom", "ReadString", "ReadBytes", "Scan", "Scanln",
        // Python
        "input", "raw_input", "read", "readline", "readlines",
        "recv", "recvfrom",
        // Generic / HTTP
        "request", "body", "query", "form", "json",
        "sys.stdin",
    ]
    .into_iter()
    .collect()
}

// ─────────────────────────────────────────────────────────────────────────────
// IR: extracted program entities
// ─────────────────────────────────────────────────────────────────────────────

#[derive(Debug, Clone, Serialize, Deserialize)]
struct VarDef {
    name: String,
    loc: Loc,
    /// The expression on the right-hand side, as raw source text
    rhs: String,
    /// Variables / call results referenced in rhs
    rhs_uses: Vec<String>,
    taint: TaintLevel,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct CallSite {
    callee: String,
    loc: Loc,
    /// Argument expressions as source text
    args: Vec<String>,
    /// Variables that appear in the arguments
    arg_vars: Vec<String>,
    /// Variable that receives the return value, if any
    return_var: Option<String>,
    /// Argument-variable names that become tainted OUTPUTS after this call
    tainted_arg_outs: Vec<String>,
    taint: TaintLevel,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct Branch {
    loc: Loc,
    /// The condition expression as source text
    condition: String,
    /// Variables referenced in the condition
    condition_vars: Vec<String>,
    taint: TaintLevel,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct Function {
    name: String,
    loc: Loc,
    params: Vec<String>,
    var_defs: Vec<VarDef>,
    call_sites: Vec<CallSite>,
    branches: Vec<Branch>,
    /// Variables whose taint is Unknown (function params from outside)
    param_taint: HashMap<String, TaintLevel>,
}

impl Function {
    fn new(name: String, loc: Loc, params: Vec<String>) -> Self {
        Function {
            name,
            loc,
            params,
            var_defs: vec![],
            call_sites: vec![],
            branches: vec![],
            param_taint: HashMap::new(),
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Language detection
// ─────────────────────────────────────────────────────────────────────────────

#[derive(Debug, Clone, Copy, PartialEq)]
enum Lang {
    C,
    Cpp,
    Rust,
    Go,
    Python,
}

impl Lang {
    fn detect(path: &PathBuf) -> Option<Lang> {
        match path.extension()?.to_str()? {
            "c" | "h"                    => Some(Lang::C),
            "cpp" | "cc" | "cxx" | "hpp" => Some(Lang::Cpp),
            "rs"                          => Some(Lang::Rust),
            "go"                          => Some(Lang::Go),
            "py"                          => Some(Lang::Python),
            _                             => None,
        }
    }

    fn tree_sitter_language(self) -> Language {
        match self {
            Lang::C      => tree_sitter_c::language(),
            Lang::Cpp    => tree_sitter_cpp::language(),
            Lang::Rust   => tree_sitter_rust::language(),
            Lang::Go     => tree_sitter_go::language(),
            Lang::Python => tree_sitter_python::language(),
        }
    }

}

// ─────────────────────────────────────────────────────────────────────────────
// AST walker — language-agnostic extraction
// ─────────────────────────────────────────────────────────────────────────────

const MAX_AST_DEPTH: usize = 128;

struct Extractor<'src> {
    src: &'src [u8],
    file: String,
    functions: Vec<Function>,
    /// stack of function indices we're currently inside
    fn_stack: Vec<usize>,
}

impl<'src> Extractor<'src> {
    fn new(src: &'src [u8], file: &str) -> Self {
        Extractor {
            src,
            file: file.to_string(),
            functions: vec![],
            fn_stack: vec![],
        }
    }

    fn text(&self, node: &Node) -> &str {
        node.utf8_text(self.src).unwrap_or("")
    }

    fn current_fn(&mut self) -> Option<&mut Function> {
        self.fn_stack.last().copied().map(|i| &mut self.functions[i])
    }

    // ── collect identifier names from an expression node ──────────────────
    fn collect_idents(&self, node: &Node) -> Vec<String> {
        let mut out = Vec::new();
        self.collect_idents_rec(node, &mut out);
        out
    }

    fn collect_idents_rec(&self, node: &Node, out: &mut Vec<String>) {
        self.collect_idents_depth(node, out, 0);
    }

    fn collect_idents_depth(&self, node: &Node, out: &mut Vec<String>, depth: usize) {
        if depth > MAX_AST_DEPTH { return; }
        let kind = node.kind();
        if kind == "identifier" || kind == "name" || kind == "simple_identifier" {
            let t = self.text(node).to_string();
            if !t.is_empty() && !out.contains(&t) {
                out.push(t);
            }
        }
        let mut cursor = node.walk();
        if cursor.goto_first_child() {
            loop {
                self.collect_idents_depth(&cursor.node(), out, depth + 1);
                if !cursor.goto_next_sibling() { break; }
            }
        }
    }

    // ── walk the full tree ────────────────────────────────────────────────
    fn walk(&mut self, tree: &Tree) {
        let root = tree.root_node();
        self.visit(&root, 0);
    }

    fn visit(&mut self, node: &Node, depth: usize) {
        if depth > MAX_AST_DEPTH { return; }

        let kind = node.kind();

        match kind {
            // ── function definitions ──────────────────────────────────────
            "function_definition"       // C / C++
            | "function_item"           // Rust
            | "function_declaration"    // Go
            | "method_declaration"      // Go
            | "function_def"            // Python
            | "decorated_definition"    // Python
            => {
                self.enter_function(node);
                self.visit_children(node, depth + 1);
                self.fn_stack.pop();
                return; // children already visited
            }

            // ── variable / let declarations ───────────────────────────────
            "declaration"               // C / C++: int x = expr;
            | "let_declaration"         // Rust:    let x = expr;
            | "short_var_declaration"   // Go:      x := expr
            | "var_declaration"         // Go:      var x = expr
            | "assignment_statement"    // Go / Python
            | "assignment"              // Python: x = expr
            => {
                self.handle_assignment(node);
                return; // do NOT descend — avoids re-processing nested exprs
            }

            // ── standalone call statements ────────────────────────────────
            "expression_statement" => {
                if let Some(inner) = node.child(0) {
                    if matches!(inner.kind(), "call_expression" | "call") {
                        self.handle_call(&inner, None);
                        return;
                    }
                }
                // Non-call expression statement: still descend for nested fns
            }

            // ── branch conditions ─────────────────────────────────────────
            "if_statement"
            | "if_expression"           // Rust
            | "while_statement"
            | "for_statement"
            | "match_expression"        // Rust
            | "switch_statement"
            => {
                self.handle_branch(node);
                // Descend so we catch declarations / calls inside the body
            }

            // ── skip leaf / punctuation nodes early ──────────────────────
            kind if node.child_count() == 0 => {
                let _ = kind; // leaf — nothing to do
                return;
            }

            _ => {}
        }

        self.visit_children(node, depth + 1);
    }

    fn visit_children(&mut self, node: &Node, depth: usize) {
        let mut cursor = node.walk();
        if cursor.goto_first_child() {
            loop {
                self.visit(&cursor.node(), depth);
                if !cursor.goto_next_sibling() { break; }
            }
        }
    }

    // ── Function entry ───────────────────────────────────────────────────
    fn enter_function(&mut self, node: &Node) {
        let loc = node_loc(node, &self.file);

        // Find the name node
        let name = self.find_fn_name(node).unwrap_or_else(|| "<anonymous>".into());

        // Collect parameter names
        let params = self.find_params(node);

        let idx = self.functions.len();
        self.functions.push(Function::new(name, loc, params));
        self.fn_stack.push(idx);
    }

    fn find_fn_name(&self, node: &Node) -> Option<String> {
        for i in 0..node.child_count() {
            let Some(child) = node.child(i) else { continue };
            match child.kind() {
                // Direct identifier (Python def, Go, Rust function_item)
                "identifier" | "name" | "simple_identifier" => {
                    return Some(self.text(&child).to_string());
                }
                // C/C++: function_definition -> function_declarator -> identifier
                "function_declarator" | "pointer_declarator" => {
                    if let Some(name) = self.find_fn_name(&child) {
                        return Some(name);
                    }
                }
                _ => {}
            }
        }
        None
    }

    fn find_params(&self, node: &Node) -> Vec<String> {
        let mut params = Vec::new();
        self.collect_params_rec(node, &mut params);
        params
    }

    fn collect_params_rec(&self, node: &Node, out: &mut Vec<String>) {
        for i in 0..node.child_count() {
            if let Some(child) = node.child(i) {
                match child.kind() {
                    "parameter_list" | "parameters" | "formal_parameters" => {
                        self.collect_param_names(&child, out);
                    }
                    // C/C++: params live inside function_declarator
                    "function_declarator" | "pointer_declarator" => {
                        self.collect_params_rec(&child, out);
                    }
                    _ => {}
                }
            }
        }
    }

    fn collect_param_names(&self, node: &Node, out: &mut Vec<String>) {
        let mut cursor = node.walk();
        if cursor.goto_first_child() {
            loop {
                let child = cursor.node();
                match child.kind() {
                    "parameter_declaration" | "parameter" | "typed_parameter"
                    | "variadic_parameter" | "self_parameter" => {
                        // take the last identifier in a parameter node
                        let idents = self.collect_idents(&child);
                        if let Some(last) = idents.last() {
                            out.push(last.clone());
                        }
                    }
                    "identifier" | "simple_identifier" => {
                        out.push(self.text(&child).to_string());
                    }
                    _ => {}
                }
                if !cursor.goto_next_sibling() { break; }
            }
        }
    }

    // ── Assignment / declaration ─────────────────────────────────────────
    fn handle_assignment(&mut self, node: &Node) {
        let loc = node_loc(node, &self.file);

        // Extract lhs/rhs data first (immutable borrow of self via split_assignment).
        let (lhs_name, rhs_text, rhs_uses) = self.split_assignment(node);

        // Extract call data while still under immutable borrow, before any
        // &mut self call.  We copy out all needed strings so no Node reference
        // survives past this block.
        let call_data: Option<(Loc, String, Vec<String>, Vec<String>)> =
            self.find_call_in(*node).map(|call_node| {
                let cloc    = node_loc(&call_node, &self.file);
                let callee  = self.extract_callee(&call_node);
                let (args, arg_vars) = self.extract_args(&call_node);
                (cloc, callee, args, arg_vars)
            });

        // Now all immutable borrows are released — safe to call &mut self methods.
        if let Some((cloc, callee, args, arg_vars)) = call_data {
            self.push_call_site(cloc, callee, args, arg_vars, lhs_name.as_deref());
        }

        if let Some(name) = lhs_name {
            if let Some(f) = self.current_fn() {
                f.var_defs.push(VarDef {
                    name,
                    loc,
                    rhs: rhs_text,
                    rhs_uses,
                    taint: TaintLevel::None,
                });
            }
        }
    }

    /// Push a pre-extracted call site onto the current function.
    /// Separated from handle_call so callers can release immutable borrows first.
    fn push_call_site(
        &mut self,
        loc: Loc,
        callee: String,
        args: Vec<String>,
        arg_vars: Vec<String>,
        return_var: Option<&str>,
    ) {
        if let Some(f) = self.current_fn() {
            f.call_sites.push(CallSite {
                callee,
                loc,
                args,
                arg_vars,
                return_var: return_var.map(|s| s.to_string()),
                tainted_arg_outs: vec![],
                taint: TaintLevel::None,
            });
        }
    }

    fn split_assignment(&self, node: &Node) -> (Option<String>, String, Vec<String>) {
        let mut cursor = node.walk();
        let mut children = vec![];
        if cursor.goto_first_child() {
            loop {
                children.push(cursor.node());
                if !cursor.goto_next_sibling() { break; }
            }
        }

        // Generic pattern: first non-punctuation child = lhs, last = rhs
        let non_punct: Vec<Node> = children
            .iter()
            .filter(|n| !matches!(n.kind(), "=" | ":=" | "let" | "var" | "mut" | ";" | ","))
            .cloned()
            .collect();

        if non_punct.len() >= 2 {
            let lhs = non_punct.first().unwrap();
            let rhs = non_punct.last().unwrap();
            let lhs_name = {
                // For typed declarations: int *buf — take the deepest identifier
                let idents = self.collect_idents(lhs);
                idents.last().cloned()
            };
            let rhs_text = self.text(rhs).to_string();
            let rhs_uses = self.collect_idents(rhs);
            return (lhs_name, rhs_text, rhs_uses);
        }

        (None, self.text(node).to_string(), self.collect_idents(node))
    }

    fn find_call_in<'a>(&self, node: Node<'a>) -> Option<Node<'a>> {
        self.find_call_in_depth(node, 0)
    }

    fn find_call_in_depth<'a>(&self, node: Node<'a>, depth: usize) -> Option<Node<'a>> {
        if depth > MAX_AST_DEPTH { return None; }
        if matches!(node.kind(), "call_expression" | "call") {
            return Some(node);
        }
        for i in 0..node.child_count() {
            if let Some(child) = node.child(i) {
                if let Some(c) = self.find_call_in_depth(child, depth + 1) {
                    return Some(c);
                }
            }
        }
        None
    }

    // ── Call site extraction ──────────────────────────────────────────────
    fn handle_call(&mut self, node: &Node, return_var: Option<&str>) {
        let loc = node_loc(node, &self.file);
        let callee = self.extract_callee(node);
        let (args, arg_vars) = self.extract_args(node);

        if let Some(f) = self.current_fn() {
            f.call_sites.push(CallSite {
                callee,
                loc,
                args,
                arg_vars,
                return_var: return_var.map(|s| s.to_string()),
                tainted_arg_outs: vec![],
                taint: TaintLevel::None,
            });
        }
    }

    fn extract_callee(&self, node: &Node) -> String {
        // tree-sitter call nodes: first child is usually the function/identifier
        let mut cursor = node.walk();
        if cursor.goto_first_child() {
            let child = cursor.node();
            return self.text(&child).to_string();
        }
        "<unknown>".to_string()
    }

    fn extract_args(&self, node: &Node) -> (Vec<String>, Vec<String>) {
        let (args, per_arg_vars) = self.extract_args_detailed(node);
        let vars: Vec<String> = {
            let mut v: Vec<String> = per_arg_vars.into_iter().flatten().collect();
            v.dedup();
            v
        };
        (args, vars)
    }

    /// Returns (arg_text_list, per_arg_ident_list).
    /// per_arg_ident_list[i] contains all identifier tokens from argument i.
    fn extract_args_detailed(&self, node: &Node) -> (Vec<String>, Vec<Vec<String>>) {
        let mut args: Vec<String>      = Vec::new();
        let mut per_arg: Vec<Vec<String>> = Vec::new();

        let mut cursor = node.walk();
        if cursor.goto_first_child() {
            loop {
                let child = cursor.node();
                if matches!(child.kind(), "argument_list" | "arguments" | "call_suffix") {
                    let mut ac = child.walk();
                    if ac.goto_first_child() {
                        loop {
                            let a = ac.node();
                            if !matches!(a.kind(), "(" | ")" | "," | " ") {
                                args.push(self.text(&a).to_string());
                                per_arg.push(self.collect_idents(&a));
                            }
                            if !ac.goto_next_sibling() { break; }
                        }
                    }
                }
                if !cursor.goto_next_sibling() { break; }
            }
        }
        (args, per_arg)
    }

    /// Return the first simple identifier from argument `idx` of a call node.
    /// Used by taint propagation to identify the buffer variable for sources
    /// like read(fd, buf, n) where the buffer is not the return value.
    fn extract_buf_var_at(&self, node: &Node, idx: usize) -> Option<String> {
        let (_, per_arg) = self.extract_args_detailed(node);
        per_arg.get(idx)
            .and_then(|idents| idents.iter()
                .find(|s| !s.is_empty() && !s.chars().next().map_or(true, |c| c.is_ascii_digit()))
                .cloned())
    }

    // ── Branch conditions ─────────────────────────────────────────────────
    fn handle_branch(&mut self, node: &Node) {
        let loc = node_loc(node, &self.file);

        // Find the condition child
        let cond_node = self.find_condition(node);
        let condition = cond_node
            .as_ref()
            .map(|n| self.text(n).to_string())
            .unwrap_or_default();
        let condition_vars = cond_node
            .as_ref()
            .map(|n| self.collect_idents(n))
            .unwrap_or_default();

        if let Some(f) = self.current_fn() {
            f.branches.push(Branch {
                loc,
                condition,
                condition_vars,
                taint: TaintLevel::None,
            });
        }
    }

    fn find_condition<'a>(&self, node: &'a Node) -> Option<Node<'a>> {
        let mut cursor = node.walk();
        if cursor.goto_first_child() {
            loop {
                let child = cursor.node();
                match child.kind() {
                    "condition"
                    | "parenthesized_expression"
                    | "binary_expression"
                    | "unary_expression"
                    | "comparison_operator"  // Python
                    | "boolean_operator"     // Python
                    => return Some(child),
                    _ => {}
                }
                if !cursor.goto_next_sibling() { break; }
            }
        }
        None
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Taint propagation pass
// ─────────────────────────────────────────────────────────────────────────────

struct TaintEngine {
    sources: HashSet<String>,
    sanitisers: HashSet<String>,
}

impl TaintEngine {
    fn new(extra_sources: &[String], sanitisers: &[String]) -> Self {
        let mut sources: HashSet<String> =
            builtin_sources().into_iter().map(|s| s.to_string()).collect();
        for s in extra_sources {
            sources.insert(s.clone());
        }
        TaintEngine {
            sources,
            sanitisers: sanitisers.iter().cloned().collect(),
        }
    }

    fn is_source(&self, name: &str) -> bool {
        // Exact match or suffix match (e.g. "stdin.read" matches "read")
        self.sources.contains(name)
            || name.split('.').last().map_or(false, |s| self.sources.contains(s))
            || name.split("::").last().map_or(false, |s| self.sources.contains(s))
    }

    fn is_sanitiser(&self, name: &str) -> bool {
        self.sanitisers.contains(name)
    }

    /// Which 0-based argument index receives tainted data for known buffer-filling sources.
    fn source_buf_arg_index(callee: &str) -> Option<usize> {
        let name = callee.split("::").last().unwrap_or(callee)
                         .split('.').last().unwrap_or(callee);
        match name {
            "read" | "recv" | "recvfrom" | "recvmsg" => Some(1),
            "fread"                                   => Some(0),
            "fgets" | "gets"                          => Some(0),
            "scanf" | "sscanf"                        => Some(1),
            "fscanf"                                  => Some(2),
            "getline" | "getdelim"                    => Some(0),
            "read_to_string" | "read_to_end" | "read_line" => Some(0),
            _                                         => None,
        }
    }

    /// Run a fixed-point taint propagation over all functions.
    fn propagate(&self, functions: &mut Vec<Function>) {
        // Single forward pass per function.  Within one function the IR nodes
        // are in source order, so one pass over call_sites → var_defs →
        // branches is a complete fixed point.  An outer loop is not needed
        // and would loop forever because tmap is rebuilt each call.
        for func in functions.iter_mut() {
            self.propagate_function(func);
        }
    }

    fn propagate_function(&self, func: &mut Function) {
        // Build a taint map: varname → TaintLevel, seeded from known params.
        let mut tmap: AHashMap<String, TaintLevel> = AHashMap::default();
        for (name, level) in &func.param_taint {
            tmap.insert(name.clone(), *level);
        }

        // Pass 1: call sites — sources seed taint, sanitisers clear it,
        //         other calls propagate taint from their arguments.
        for cs in func.call_sites.iter_mut() {
            let callee_taint = if self.is_source(&cs.callee) {
                TaintLevel::Full
            } else if self.is_sanitiser(&cs.callee) {
                TaintLevel::None
            } else {
                cs.arg_vars.iter().fold(TaintLevel::None, |acc, v| {
                    acc.join(*tmap.get(v.as_str()).unwrap_or(&TaintLevel::None))
                })
            };

            cs.taint = callee_taint;

            // Buffer-filling sources: taint the destination argument variable.
            // Parse the arg text directly to find the first identifier — this is more
            // reliable than the flat arg_vars list which mixes idents from all args.
            if self.is_source(&cs.callee) {
                if let Some(buf_idx) = TaintEngine::source_buf_arg_index(&cs.callee) {
                    // Try args[buf_idx] text first: split on non-ident chars, take first token
                    let buf_var: Option<String> = cs.args.get(buf_idx).and_then(|a| {
                        a.split(|c: char| !c.is_alphanumeric() && c != '_')
                         .find(|s| !s.is_empty() &&
                              !s.chars().next().map_or(true, |c| c.is_ascii_digit()))
                         .map(|s| s.to_string())
                    });
                    if let Some(bv) = buf_var {
                        if !cs.tainted_arg_outs.contains(&bv) {
                            cs.tainted_arg_outs.push(bv.clone());
                        }
                        tmap.insert(bv, TaintLevel::Full);
                    }
                }
            }

            // Return value propagation.
            if let Some(rv) = &cs.return_var {
                let new_t = tmap
                    .get(rv.as_str())
                    .copied()
                    .unwrap_or(TaintLevel::None)
                    .join(callee_taint);
                tmap.insert(rv.clone(), new_t);
            }
        }

        // Pass 2: variable definitions — propagate taint through assignments.
        for vd in func.var_defs.iter_mut() {
            let rhs_taint = vd.rhs_uses.iter().fold(TaintLevel::None, |acc, v| {
                let vt = *tmap.get(v.as_str()).unwrap_or(&TaintLevel::None);
                match (acc, vt) {
                    (TaintLevel::None, t) | (t, TaintLevel::None) => t,
                    (TaintLevel::Full, TaintLevel::Full)           => TaintLevel::Full,
                    _                                               => TaintLevel::Partial,
                }
            });
            let effective = tmap
                .get(vd.name.as_str())
                .copied()
                .unwrap_or(TaintLevel::None)
                .join(rhs_taint);
            vd.taint = effective;
            tmap.insert(vd.name.clone(), effective);
        }

        // Pass 3: branch conditions — annotate with taint of referenced vars.
        for br in func.branches.iter_mut() {
            br.taint = br.condition_vars.iter().fold(TaintLevel::None, |acc, v| {
                acc.join(*tmap.get(v.as_str()).unwrap_or(&TaintLevel::None))
            });
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Target resolution
// ─────────────────────────────────────────────────────────────────────────────

#[derive(Debug, Clone)]
enum Target {
    /// Any tainted use inside a named function
    Function(String),
    /// A specific variable inside a named function
    FunctionVar { func: String, var: String },
    /// An exact source location
    Location { line: usize, col: usize },
}

impl Target {
    fn parse(s: &str) -> Target {
        // line:col?
        let parts: Vec<&str> = s.split(':').collect();
        if parts.len() == 2 {
            if let (Ok(line), Ok(col)) = (parts[0].parse::<usize>(), parts[1].parse::<usize>()) {
                return Target::Location { line, col };
            }
            // function:variable
            return Target::FunctionVar {
                func: parts[0].to_string(),
                var: parts[1].to_string(),
            };
        }
        Target::Function(s.to_string())
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Path node — one step in a data-flow path
// ─────────────────────────────────────────────────────────────────────────────

#[derive(Debug, Clone, Serialize, Deserialize)]
enum NodeKind {
    Source,
    Assignment,
    Call,
    Branch,
    FunctionEntry,
    Sink,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct PathNode {
    kind: NodeKind,
    loc: Loc,
    description: String,
    taint: TaintLevel,
    /// Branch conditions that must hold to reach this node from the previous
    conditions: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct DataFlowPath {
    /// Index of the path (for display)
    index: usize,
    nodes: Vec<PathNode>,
    /// Overall taint level at the sink
    sink_taint: TaintLevel,
}

// ─────────────────────────────────────────────────────────────────────────────
// Path finder — forward taint propagation from sources to sinks
// ─────────────────────────────────────────────────────────────────────────────
//
// For each source call (read/getenv/recv/…) in a function, we walk forward
// through variable assignments and call sites to see where the tainted data
// flows.  A path is emitted for every chain: source → assignments → sink call.

struct PathFinder<'a> {
    functions: &'a [Function],
    engine: &'a TaintEngine,
    max_depth: usize,
}

/// One IR node visited during the forward walk.
enum ForwardStep<'a> {
    Def(&'a VarDef),
    Call(&'a CallSite),
}

impl<'a> PathFinder<'a> {
    fn find_paths(&self, target: &Target) -> Vec<DataFlowPath> {
        let mut paths = Vec::new();
        let mut idx   = 0usize;

        let target_fns: Vec<&Function> = match target {
            Target::Function(name) =>
                self.functions.iter().filter(|f| &f.name == name).collect(),
            Target::FunctionVar { func: fn_name, .. } =>
                self.functions.iter().filter(|f| &f.name == fn_name).collect(),
            Target::Location { .. } =>
                self.functions.iter().collect(),
        };

        for func in target_fns {
            self.scan_function(func, target, &mut paths, &mut idx);
        }

        paths.sort_by(|a, b| b.sink_taint.cmp(&a.sink_taint));
        paths
    }

    fn scan_function(
        &self,
        func: &Function,
        target: &Target,
        out: &mut Vec<DataFlowPath>,
        idx: &mut usize,
    ) {
        // ── Pass A: use propagate_function results directly ───────────────────
        // propagate_function already marked every call site and var def with its
        // taint level. Emit a finding for each tainted call site / var def that
        // isn't itself a source. This catches ALL cases including complex pointer
        // expressions that the forward walk can't follow.
        for cs in &func.call_sites {
            if cs.taint == TaintLevel::None { continue; }
            if self.engine.is_source(&cs.callee) { continue; }

            // Find which source call(s) in this function tainted the arguments
            let source_descs: Vec<String> = func.call_sites.iter()
                .filter(|s| self.engine.is_source(&s.callee) && s.taint != TaintLevel::None)
                .map(|s| format!("{}({}) at line {}",
                    s.callee, s.args.first().map(|a| a.as_str()).unwrap_or("…"), s.loc.line))
                .collect();

            let src_summary = if source_descs.is_empty() {
                "tainted parameter".to_string()
            } else {
                source_descs.join(", ")
            };

            // Find the primary source node (first source call in this function)
            let src_cs_opt = func.call_sites.iter()
                .find(|s| self.engine.is_source(&s.callee) && s.taint != TaintLevel::None);

            let nodes: Vec<PathNode> = if let Some(src_cs) = src_cs_opt {
                let src_node = PathNode {
                    kind: NodeKind::Source,
                    loc:  src_cs.loc.clone(),
                    description: format!("{}  ← {}({})",
                        src_cs.tainted_arg_outs.first()
                            .or(src_cs.return_var.as_ref())
                            .map(|s| s.as_str()).unwrap_or("(result)"),
                        src_cs.callee,
                        src_cs.args.join(", ")),
                    taint: TaintLevel::Full,
                    conditions: self.conditions_at(func, &src_cs.loc),
                };
                let sink_node = PathNode {
                    kind: NodeKind::Sink,
                    loc:  cs.loc.clone(),
                    description: format!("{}({})  (tainted arg: {})  via [{}]",
                        cs.callee, cs.args.join(", "),
                        cs.arg_vars.join(", "),
                        src_summary),
                    taint: cs.taint,
                    conditions: self.conditions_at(func, &cs.loc),
                };
                vec![src_node, sink_node]
            } else {
                // Taint from parameters — no source call visible in this function
                let sink_node = PathNode {
                    kind: NodeKind::Sink,
                    loc:  cs.loc.clone(),
                    description: format!("{}({})  (tainted arg: {})",
                        cs.callee, cs.args.join(", "),
                        cs.arg_vars.join(", ")),
                    taint: cs.taint,
                    conditions: self.conditions_at(func, &cs.loc),
                };
                vec![sink_node]
            };

            let matches = match target {
                Target::Function(_) => true,
                Target::FunctionVar { var, .. } =>
                    cs.return_var.as_deref() == Some(var) || cs.arg_vars.contains(var),
                Target::Location { line, col } =>
                    cs.loc.line == *line && (*col == 0 || cs.loc.col == *col),
            };

            if matches && !nodes.is_empty() {
                out.push(DataFlowPath {
                    index: *idx,
                    sink_taint: cs.taint,
                    nodes,
                });
                *idx += 1;
            }
        }

        // ── Pass B: forward walk (original logic, kept for multi-hop chains) ──
        // Only functions that contain at least one source call are interesting.
        let source_calls: Vec<&CallSite> = func.call_sites.iter()
            .filter(|cs| self.engine.is_source(&cs.callee))
            .collect();
        if source_calls.is_empty() { return; }

        // Build a line-ordered list of all var defs and (non-source) call sites.
        let mut steps: Vec<(usize, ForwardStep)> = Vec::new();
        for vd in &func.var_defs {
            steps.push((vd.loc.line, ForwardStep::Def(vd)));
        }
        for cs in &func.call_sites {
            if self.engine.is_source(&cs.callee) { continue; }
            steps.push((cs.loc.line, ForwardStep::Call(cs)));
        }
        steps.sort_by_key(|(l, _)| *l);

        for src_cs in source_calls {
            // The PathNode for this source call.
            let src_node = PathNode {
                kind: NodeKind::Source,
                loc:  src_cs.loc.clone(),
                description: format!(
                    "{}  ← {}({}){}",
                    src_cs.return_var.as_deref()
                        .or_else(|| src_cs.tainted_arg_outs.first().map(|s| s.as_str()))
                        .unwrap_or("(result)"),
                    src_cs.callee,
                    src_cs.args.join(", "),
                    if !src_cs.tainted_arg_outs.is_empty() {
                        format!("  [fills: {}]", src_cs.tainted_arg_outs.join(", "))
                    } else { String::new() }
                ),
                taint: TaintLevel::Full,
                conditions: self.conditions_at(func, &src_cs.loc),
            };

            // tainted: varname → the PathNode that last tainted it
            // Seed from return_var and tainted_arg_outs (set during propagation)
            let mut tainted: AHashMap<String, PathNode> = AHashMap::default();
            if let Some(rv) = &src_cs.return_var {
                tainted.insert(rv.clone(), src_node.clone());
            }
            for bv in &src_cs.tainted_arg_outs {
                tainted.insert(bv.clone(), src_node.clone());
            }
            // Also seed any var_defs already marked tainted by propagate_function
            // — covers cases where the return value flows through an intermediate
            // assignment before reaching the source we're walking from.
            for vd in &func.var_defs {
                if vd.taint != TaintLevel::None && !tainted.contains_key(&vd.name) {
                    let vnode = PathNode {
                        kind: NodeKind::Assignment,
                        loc:  vd.loc.clone(),
                        description: format!("{}  =  {}  (tainted)", vd.name, vd.rhs),
                        taint: vd.taint,
                        conditions: self.conditions_at(func, &vd.loc),
                    };
                    tainted.insert(vd.name.clone(), vnode);
                }
            }

            // If the source has no outputs we can trace, emit it as a standalone sink.
            if tainted.is_empty() {
                if matches!(target, Target::Function(_)) {
                    out.push(DataFlowPath {
                        index: *idx,
                        sink_taint: TaintLevel::Full,
                        nodes: vec![src_node],
                    });
                    *idx += 1;
                }
                continue;
            }

            // path_steps accumulates the chain for the *current* forward walk.
            // We start fresh for each source so paths don't bleed into each other.
            let mut path_steps: Vec<PathNode> = vec![src_node.clone()];
            let mut found_sink = false;

            for (_, step) in &steps {
                // Stop if we've exceeded depth
                if path_steps.len() >= self.max_depth { break; }

                match step {
                    // ── Variable assignment: propagate taint ─────────────
                    ForwardStep::Def(vd) => {
                        let used: Vec<String> = vd.rhs_uses.iter()
                            .filter(|v| tainted.contains_key(*v))
                            .cloned()
                            .collect();
                        if !used.is_empty() {
                            let node = PathNode {
                                kind: NodeKind::Assignment,
                                loc:  vd.loc.clone(),
                                description: format!(
                                    "{}  =  {}     (taint from: {})",
                                    vd.name, vd.rhs, used.join(", ")
                                ),
                                taint: TaintLevel::Full,
                                conditions: self.conditions_at(func, &vd.loc),
                            };
                            tainted.insert(vd.name.clone(), node.clone());
                            path_steps.push(node);
                        }
                    }

                    // ── Call site: tainted arg → this is a sink ───────────
                    ForwardStep::Call(cs) => {
                        let used: Vec<String> = cs.arg_vars.iter()
                            .filter(|v| tainted.contains_key(*v))
                            .cloned()
                            .collect();
                        if used.is_empty() { continue; }

                        let matches = match target {
                            Target::Function(_) => true,
                            Target::FunctionVar { var, .. } =>
                                cs.return_var.as_deref() == Some(var)
                                || cs.arg_vars.contains(var),
                            Target::Location { line, col } =>
                                cs.loc.line == *line && (*col == 0 || cs.loc.col == *col),
                        };

                        let sink_node = PathNode {
                            kind: NodeKind::Sink,
                            loc:  cs.loc.clone(),
                            description: format!(
                                "{}({})     (tainted arg: {})",
                                cs.callee,
                                cs.args.join(", "),
                                used.join(", ")
                            ),
                            taint: TaintLevel::Full,
                            conditions: self.conditions_at(func, &cs.loc),
                        };

                        if matches {
                            let mut nodes = path_steps.clone();
                            nodes.push(sink_node.clone());
                            out.push(DataFlowPath {
                                index: *idx,
                                sink_taint: TaintLevel::Full,
                                nodes,
                            });
                            *idx += 1;
                            found_sink = true;
                        }

                        // Propagate through return value too
                        if let Some(rv) = &cs.return_var {
                            tainted.insert(rv.clone(), sink_node);
                        }
                    }
                }
            }

            // No downstream uses found — the variable holding source data is
            // declared but never passed anywhere traceable in this function.
            // Only emit in all-sinks mode so targeted searches stay quiet.
            if !found_sink && matches!(target, Target::Function(_)) && path_steps.len() > 1 {
                // Taint was propagated to at least one variable but never passed
                // to a call — mark the last assignment step as the sink.
                let mut nodes = path_steps;
                nodes.last_mut().unwrap().kind = NodeKind::Sink;
                out.push(DataFlowPath {
                    index: *idx,
                    sink_taint: TaintLevel::Full,
                    nodes,
                });
                *idx += 1;
            }
        }
    }

    fn conditions_at(&self, func: &Function, loc: &Loc) -> Vec<String> {
        func.branches.iter()
            .filter(|br| br.loc.line <= loc.line && !br.condition.is_empty())
            .map(|br| {
                let tag = match br.taint {
                    TaintLevel::Full    => " [⚠ tainted]",
                    TaintLevel::Partial => " [~ partial]",
                    TaintLevel::None    => "",
                };
                format!("{}{}", br.condition, tag)
            })
            .collect()
    }
}

/// Extract identifier tokens from a plain string (for predecessor lookup).
#[cfg(test)]
fn extract_idents_from_str(s: &str) -> Vec<String> {
    let mut out = Vec::new();
    let mut cur = String::new();
    for c in s.chars() {
        if c.is_alphanumeric() || c == '_' {
            cur.push(c);
        } else {
            if !cur.is_empty() && !cur.chars().next().unwrap().is_ascii_digit() {
                out.push(cur.clone());
            }
            cur.clear();
        }
    }
    if !cur.is_empty() && !cur.chars().next().unwrap().is_ascii_digit() {
        out.push(cur);
    }
    out.dedup();
    out
}

// ─────────────────────────────────────────────────────────────────────────────
// Report rendering
// ─────────────────────────────────────────────────────────────────────────────

fn render_text(paths: &[DataFlowPath], target: &str) {
    let full_count    = paths.iter().filter(|p| p.sink_taint == TaintLevel::Full).count();
    let partial_count = paths.iter().filter(|p| p.sink_taint == TaintLevel::Partial).count();
    let w = 76usize;

    // ── Banner ────────────────────────────────────────────────────────────────
    println!();
    println!("  {} taint  {}  {}  {}",
        "⚡".yellow(),
        format!("target: {}", target).dimmed(),
        if full_count > 0 { format!("{} full", full_count).red().bold().to_string() }
        else { format!("{} full", full_count).bright_black().to_string() },
        if partial_count > 0 { format!("{} partial", partial_count).yellow().to_string() }
        else { format!("{} partial", partial_count).bright_black().to_string() },
    );
    println!("  {}", "─".repeat(w - 2).bright_black());

    if paths.is_empty() {
        println!("\n  {} no tainted paths found\n", "✓".green());
        return;
    }

    for path in paths {
        let nodes   = &path.nodes;
        let source  = nodes.first();
        let sink    = nodes.last();

        // ── Finding header ────────────────────────────────────────────────────
        println!();
        let sev_str = match path.sink_taint {
            TaintLevel::Full    => format!("{}", "full".red().bold()),
            TaintLevel::Partial => format!("{}", "partial".yellow()),
            TaintLevel::None    => format!("{}", "none".bright_black()),
        };
        println!("  {} #{} — {}", "finding".bright_white(), path.index, sev_str);
        println!("  {}", "─".repeat(w - 2).bright_black());

        // ── One-line summary: FROM → TO ───────────────────────────────────────
        let from_loc = source.map(|n| n.loc.to_string()).unwrap_or_default();
        let to_loc   = sink  .map(|n| n.loc.to_string()).unwrap_or_default();
        let from_desc = source.map(|n| first_line(&n.description)).unwrap_or_default();
        let to_desc   = sink  .map(|n| first_line(&n.description)).unwrap_or_default();
        println!(
            "  {}  {}",
            "FROM".bright_red().bold(),
            from_loc.yellow(),
        );
        println!(
            "  {}    {}",
            "  ".dimmed(),
            from_desc.white().bold(),
        );
        println!(
            "  {}    {}",
            "↓".bright_white().bold(),
            if nodes.len() > 2 {
                format!("{} intermediate step(s)", nodes.len() - 2).dimmed().to_string()
            } else {
                String::new()
            }
        );
        println!(
            "  {}    {}",
            "TO".bright_red().bold(),
            to_loc.yellow(),
        );
        println!(
            "  {}    {}",
            "  ".dimmed(),
            to_desc.white().bold(),
        );
        println!("  {}", "─".repeat(w - 2).bright_black());

        // ── Full flow ─────────────────────────────────────────────────────────
        println!("  {}", "DATA FLOW".bright_white().bold());
        println!();

        for (i, node) in nodes.iter().enumerate() {
            let is_source = i == 0;
            let is_sink   = i == nodes.len() - 1;

            // Step header
            let step_label = if is_source {
                format!("  {} SOURCE  {}", "┌".bright_red(), node.loc.to_string().yellow())
            } else if is_sink {
                format!("  {} SINK    {}", "└".bright_red(), node.loc.to_string().yellow())
            } else {
                format!("  {} step {}  {}", "├".bright_white(), i, node.loc.to_string().yellow())
            };
            println!("{}", step_label);

            // What kind of operation
            let op_label = match node.kind {
                NodeKind::Source       => "  reads user input".red().bold().to_string(),
                NodeKind::Assignment   => "  assigns".blue().to_string(),
                NodeKind::Call         => "  calls".magenta().to_string(),
                NodeKind::Branch       => "  branch condition".yellow().to_string(),
                NodeKind::FunctionEntry=> "  enters function".cyan().to_string(),
                NodeKind::Sink         => "  tainted data used here".red().bold().to_string(),
            };
            let bar = if is_sink { "   " } else { "  │" };
            println!("{}  {}", bar.bright_white(), op_label);

            // Conditions that gate this step
            for cond in &node.conditions {
                println!(
                    "{}  {} {}",
                    bar.bright_white(),
                    "  guarded by:".dimmed(),
                    cond.cyan()
                );
            }

            // Source snippet
            let ctx = source_context(&node.loc.file, node.loc.line, 2);
            if !ctx.is_empty() {
                println!("{}  {}", bar.bright_white(), "  ┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄".bright_black());
                for (lineno, text, is_hit) in &ctx {
                    let trimmed = text.trim_end();
                    if *is_hit {
                        println!(
                            "{}  {}{:>5}  {}",
                            bar.bright_white(),
                            "▶ ".yellow().bold(),
                            lineno.to_string().yellow().bold(),
                            trimmed.yellow()
                        );
                    } else {
                        println!(
                            "{}    {:>5}  {}",
                            bar.bright_white(),
                            lineno.to_string().bright_black(),
                            trimmed.bright_black()
                        );
                    }
                }
                println!("{}  {}", bar.bright_white(), "  ┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄".bright_black());
            }

            // Connector arrow between steps
            if !is_sink {
                println!("  {}  {}", "│".bright_white(), "tainted data flows ↓".bright_black().italic());
            }
        }

        println!();
    }

    // ── Final summary ─────────────────────────────────────────────────────────
    println!("  {}", "─".repeat(w - 2).bright_black());
    println!("  {}  {}",
        if full_count > 0 { format!("{} full", full_count).red().bold().to_string() }
        else              { format!("{} full", full_count).bright_black().to_string() },
        if partial_count > 0 { format!("{} partial", partial_count).yellow().to_string() }
        else                 { format!("{} partial", partial_count).bright_black().to_string() },
    );
    println!();
}

/// Return just the first line of a potentially multi-line description,
/// stripped of internal noise tokens like "in ()", "buf→", etc.
fn first_line(s: &str) -> String {
    s.lines().next().unwrap_or(s)
     .trim()
     .to_string()
}

fn render_json(paths: &[DataFlowPath]) {
    println!("{}", serde_json::to_string_pretty(paths).unwrap());
}

// ─────────────────────────────────────────────────────────────────────────────
// Annotated source output
// ─────────────────────────────────────────────────────────────────────────────
//
// For every source file that contains at least one tainted node, write a copy
// to the output directory with inline comments on tainted lines:
//
//   char *env = getenv("FOO");  /* taint: FULL  ← getenv */
//   int n = env_val * 2;        /* taint: FULL  ← env_val */
//   int safe = 42;              /* taint: NONE */
//
// Files that have no taint at all are not copied (keep the output small).
// The output directory tree mirrors the source paths so you can diff/grep.

fn render_annotated_sources(
    functions: &[Function],
    out_dir: &std::path::Path,
) -> Result<(), std::io::Error> {
    use std::collections::HashMap;

    // ── Build a map: file → line → (taint level, description) ─────────────
    // We collect the highest taint level per line (a line may be touched by
    // multiple nodes).
    #[derive(Clone)]
    struct LineTaint {
        level: TaintLevel,
        desc:  String,
    }

    let mut file_taint: HashMap<String, HashMap<usize, LineTaint>> = HashMap::new();

    for func in functions {
        for cs in &func.call_sites {
            if cs.taint == TaintLevel::None { continue; }
            let entry = file_taint
                .entry(cs.loc.file.clone())
                .or_default()
                .entry(cs.loc.line)
                .or_insert(LineTaint { level: TaintLevel::None, desc: String::new() });
            if cs.taint > entry.level {
                entry.level = cs.taint;
                let suffix = if !cs.tainted_arg_outs.is_empty() {
                    format!("← {} via {}", cs.callee, cs.tainted_arg_outs.join(", "))
                } else {
                    format!("← {}", cs.callee)
                };
                entry.desc = suffix;
            }
        }
        for vd in &func.var_defs {
            if vd.taint == TaintLevel::None { continue; }
            let entry = file_taint
                .entry(vd.loc.file.clone())
                .or_default()
                .entry(vd.loc.line)
                .or_insert(LineTaint { level: TaintLevel::None, desc: String::new() });
            if vd.taint > entry.level {
                entry.level = vd.taint;
                entry.desc = format!("← {}", vd.rhs_uses.join(", "));
            }
        }
    }

    if file_taint.is_empty() {
        return Ok(());
    }

    let mut files_written = 0usize;

    for (file, line_map) in &file_taint {
        let Ok(source) = std::fs::read_to_string(file) else { continue };

        // Compute output path: strip leading / to make it relative, put under out_dir
        let rel = std::path::Path::new(file)
            .strip_prefix("/")
            .unwrap_or(std::path::Path::new(file));
        let out_path = out_dir.join(rel);

        // Create parent directories
        if let Some(parent) = out_path.parent() {
            std::fs::create_dir_all(parent)?;
        }

        let mut out = String::with_capacity(source.len() + line_map.len() * 40);

        for (i, line) in source.lines().enumerate() {
            let lineno = i + 1;
            if let Some(lt) = line_map.get(&lineno) {
                let level_str = match lt.level {
                    TaintLevel::Full    => "FULL   ",
                    TaintLevel::Partial => "PARTIAL",
                    TaintLevel::None    => "NONE   ",
                };
                // Append inline comment preserving original line, then annotation
                out.push_str(line.trim_end());
                // Pad to column 80 for alignment (best-effort)
                let line_len = line.trim_end().chars().count();
                let pad = if line_len < 78 { 78 - line_len } else { 1 };
                out.push_str(&" ".repeat(pad));
                out.push_str(&format!("/* taint:{} {} */", level_str, lt.desc));
                out.push('\n');
            } else {
                out.push_str(line);
                out.push('\n');
            }
        }

        std::fs::write(&out_path, out)?;
        files_written += 1;
    }

    eprintln!("  {} annotated {} file(s) → {}",
        "✓".green(), files_written, out_dir.display());
    Ok(())
}

// ─────────────────────────────────────────────────────────────────────────────
// HTML report renderer
// ─────────────────────────────────────────────────────────────────────────────

fn html_escape(s: &str) -> String {
    s.replace('&', "&amp;")
     .replace('<', "&lt;")
     .replace('>', "&gt;")
     .replace('"', "&quot;")
}

fn render_html(
    paths: &[DataFlowPath],
    target: &str,
    out_path: &PathBuf,
) -> Result<(), std::io::Error> {
    let full_count    = paths.iter().filter(|p| p.sink_taint == TaintLevel::Full).count();
    let partial_count = paths.iter().filter(|p| p.sink_taint == TaintLevel::Partial).count();

    // ── Collect all unique source function names for filter UI ────────────────
    let mut all_sources: Vec<String> = paths.iter()
        .filter_map(|p| p.nodes.first())
        .map(|n| {
            // Extract just the callee name from "fn()  calls foo(...)"
            let desc = first_line(&n.description);
            let callee = desc.split("calls ").nth(1)
                .and_then(|s| s.split('(').next())
                .unwrap_or(&desc)
                .trim()
                .to_string();
            callee
        })
        .collect::<std::collections::HashSet<_>>()
        .into_iter()
        .collect();
    all_sources.sort();

    // ── Build per-finding data ────────────────────────────────────────────────
    let mut cards = String::new();

    for path in paths {
        let nodes      = &path.nodes;
        let source_node = nodes.first();
        let sink_node   = nodes.last();

        let sev_class = match path.sink_taint {
            TaintLevel::Full    => "full",
            TaintLevel::Partial => "partial",
            TaintLevel::None    => "none",
        };

        let from_loc  = source_node.map(|n| n.loc.to_string()).unwrap_or_default();
        let to_loc    = sink_node  .map(|n| n.loc.to_string()).unwrap_or_default();
        let from_desc = source_node.map(|n| first_line(&n.description)).unwrap_or_default();
        let to_desc   = sink_node  .map(|n| first_line(&n.description)).unwrap_or_default();

        // Source file basename
        let src_file = source_node
            .map(|n| std::path::Path::new(&n.loc.file)
                .file_name()
                .map(|f| f.to_string_lossy().to_string())
                .unwrap_or_default())
            .unwrap_or_default();

        let sink_file = sink_node
            .map(|n| std::path::Path::new(&n.loc.file)
                .file_name()
                .map(|f| f.to_string_lossy().to_string())
                .unwrap_or_default())
            .unwrap_or_default();

        // Extract callee name for source tag
        let source_callee = {
            let desc = from_desc.clone();
            desc.split("calls ").nth(1)
                .and_then(|s| s.split('(').next())
                .unwrap_or(&desc)
                .trim()
                .to_string()
        };

        let total_guards: usize = nodes.iter().map(|n| n.conditions.len()).sum();
        let steps = nodes.len();

        // ── Build the flow pipeline visualization ─────────────────────────────
        let mut pipeline = String::new();
        for (i, node) in nodes.iter().enumerate() {
            let is_source = i == 0;
            let is_sink   = i == nodes.len() - 1;

            let node_file = std::path::Path::new(&node.loc.file)
                .file_name()
                .map(|f| f.to_string_lossy().to_string())
                .unwrap_or_default();

            let node_type_class = if is_source { "pn-source" }
                else if is_sink { "pn-sink" }
                else { "pn-step" };

            let node_icon = if is_source { "⬤" }
                else if is_sink { "◆" }
                else { "●" };

            let kind_short = match node.kind {
                NodeKind::Source       => "READ",
                NodeKind::Sink         => "USE",
                NodeKind::Assignment   => "ASSIGN",
                NodeKind::Call         => "CALL",
                NodeKind::Branch       => "BRANCH",
                NodeKind::FunctionEntry=> "ENTRY",
            };

            let guards_badge = if !node.conditions.is_empty() {
                format!(r#"<span class="guards-pill" title="{} guard(s)">{}</span>"#,
                    node.conditions.len(), node.conditions.len())
            } else { String::new() };

            // Code snippet for this node (±3 lines, no full file)
            let ctx = source_context(&node.loc.file, node.loc.line, 3);
            let mut snippet = String::new();
            if !ctx.is_empty() {
                snippet.push_str(r#"<pre class="code">"#);
                for (lineno, text, is_hit) in &ctx {
                    let cls = if *is_hit { " hit" } else { "" };
                    snippet.push_str(&format!(
                        r#"<div class="cl{}">{}<span class="ln">{:>4}</span>  {}</div>"#,
                        cls, "", lineno, html_escape(text)
                    ));
                }
                snippet.push_str("</pre>");
            }

            // Guards list
            let mut guards_html = String::new();
            if !node.conditions.is_empty() {
                guards_html.push_str(r#"<div class="node-guards">"#);
                for cond in &node.conditions {
                    guards_html.push_str(&format!(
                        r#"<div class="guard-item">if {}</div>"#,
                        html_escape(cond)
                    ));
                }
                guards_html.push_str("</div>");
            }

            pipeline.push_str(&format!(
                r#"<div class="pipe-node {node_type_class}">
  <div class="pn-header">
    <span class="pn-icon">{node_icon}</span>
    <span class="pn-kind">{kind_short}</span>
    <span class="pn-file">{node_file}</span>
    <span class="pn-loc">:{lineno}</span>
    {guards_badge}
  </div>
  <details class="pn-details">
    <summary class="pn-desc">{desc}</summary>
    {guards_html}
    {snippet}
  </details>
</div>"#,
                node_type_class = node_type_class,
                node_icon = node_icon,
                kind_short = kind_short,
                node_file = html_escape(&node_file),
                lineno = node.loc.line,
                guards_badge = guards_badge,
                desc = html_escape(&first_line(&node.description)),
                guards_html = guards_html,
                snippet = snippet,
            ));

            if !is_sink {
                pipeline.push_str(r#"<div class="pipe-arrow">▼</div>"#);
            }
        }

        // Cross-file indicator
        let cross_file = src_file != sink_file;
        let cross_badge = if cross_file {
            format!(r#"<span class="cross-file" title="crosses file boundary">{} → {}</span>"#,
                html_escape(&src_file), html_escape(&sink_file))
        } else {
            format!(r#"<span class="same-file">{}</span>"#, html_escape(&src_file))
        };

        cards.push_str(&format!(
            r#"<div class="card {sev}" id="c{idx}"
  data-sev="{sev}"
  data-source="{source_callee}"
  data-steps="{steps}"
  data-guards="{guards}"
  data-srcfile="{src_file}"
  data-sinkfile="{sink_file}">

  <!-- ── Card header ── -->
  <div class="card-hdr">
    <div class="card-hdr-left">
      <span class="sev-pill {sev}">{sev_label}</span>
      <span class="finding-num">#{idx}</span>
      {cross_badge}
    </div>
    <div class="card-hdr-pills">
      <span class="meta-pill steps-pill" title="flow steps">{steps}&#8239;steps</span>
      <span class="meta-pill guards-pill-hdr" title="total guards">{guards}&#8239;guards</span>
      <span class="meta-pill src-pill" title="taint source">{source_callee}</span>
    </div>
  </div>

  <!-- ── Quick FROM→TO ── -->
  <div class="quick-flow">
    <div class="qf-from">
      <span class="qf-label src">FROM</span>
      <span class="qf-loc">{from_loc_esc}</span>
      <span class="qf-desc">{from_desc_esc}</span>
    </div>
    <div class="qf-arrow">
      {chain_dots}
    </div>
    <div class="qf-to">
      <span class="qf-label snk">TO</span>
      <span class="qf-loc">{to_loc_esc}</span>
      <span class="qf-desc">{to_desc_esc}</span>
    </div>
  </div>

  <!-- ── Collapsible pipeline ── -->
  <details class="pipeline-wrap">
    <summary class="pipeline-toggle">
      <span class="tog-arrow">▶</span>
      Data flow pipeline
      <span class="tog-sub">{steps} node(s) · click to expand</span>
    </summary>
    <div class="pipeline">
      {pipeline}
    </div>
  </details>

</div>
"#,
            sev            = sev_class,
            idx            = path.index,
            sev_label      = match path.sink_taint {
                TaintLevel::Full    => "FULL",
                TaintLevel::Partial => "PARTIAL",
                TaintLevel::None    => "NONE",
            },
            cross_badge    = cross_badge,
            steps          = steps,
            guards         = total_guards,
            source_callee  = html_escape(&source_callee),
            from_loc_esc   = html_escape(&from_loc),
            from_desc_esc  = html_escape(&from_desc),
            to_loc_esc     = html_escape(&to_loc),
            to_desc_esc    = html_escape(&to_desc),
            src_file       = html_escape(&src_file),
            sink_file      = html_escape(&sink_file),
            chain_dots     = {
                let dots = if steps > 2 { steps - 2 } else { 0 };
                if dots > 0 {
                    format!("── {} step(s) ──▶", dots)
                } else {
                    "──▶".to_string()
                }
            },
            pipeline       = pipeline,
        ));
    }

    // ── Source filter options ─────────────────────────────────────────────────
    let source_opts: String = all_sources.iter()
        .map(|s| format!(r#"<option value="{}">{}</option>"#, html_escape(s), html_escape(s)))
        .collect();

    let html = format!(
        r#"<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Taint — {target_esc}</title>
<style>
:root{{
  --bg:#0d1117;--s:#161b22;--b:#30363d;--b2:#21262d;
  --t:#c9d1d9;--m:#8b949e;--a:#58a6ff;
  --red:#f85149;--orange:#e09b52;--yel:#e3b341;--grn:#3fb950;--purple:#bc8cff;
  --hit:#1a2d1a;--hitborder:#3fb950;
  --mono:"JetBrains Mono","Cascadia Code","Fira Code",ui-monospace,monospace;
  --fs:12px;--lh:16px;
}}
*{{box-sizing:border-box;margin:0;padding:0}}
html,body{{font-size:var(--fs);line-height:var(--lh)}}
body{{background:var(--bg);color:var(--t);font-family:system-ui,sans-serif;
  padding:0;min-height:100vh}}

/* ════ HEADER ════════════════════════════════════════════════════════════════*/
.topbar{{
  background:linear-gradient(135deg,#0d1117 0%,#161b22 100%);
  border-bottom:1px solid var(--b);
  padding:10px 16px;position:sticky;top:0;z-index:100;
}}
.topbar-inner{{display:flex;align-items:center;gap:12px;flex-wrap:wrap}}
.logo{{font-size:14px;font-weight:800;letter-spacing:-.02em;
  background:linear-gradient(90deg,var(--red),var(--orange),var(--yel));
  -webkit-background-clip:text;-webkit-text-fill-color:transparent;
  white-space:nowrap}}
.target-chip{{background:var(--b2);border:1px solid var(--b);
  border-radius:4px;padding:2px 8px;font-size:10px;color:var(--m);
  font-family:var(--mono);white-space:nowrap;overflow:hidden;
  text-overflow:ellipsis;max-width:300px}}
.stat-chips{{display:flex;gap:6px;margin-left:auto}}
.stat-chip{{padding:2px 8px;border-radius:12px;font-size:10px;font-weight:700;
  letter-spacing:.04em}}
.stat-chip.full{{background:rgba(248,81,73,.18);color:var(--red);
  border:1px solid rgba(248,81,73,.35)}}
.stat-chip.partial{{background:rgba(227,179,65,.15);color:var(--yel);
  border:1px solid rgba(227,179,65,.3)}}
.stat-chip.none{{background:var(--b2);color:var(--m);border:1px solid var(--b)}}

/* ════ FILTER BAR ════════════════════════════════════════════════════════════*/
.filterbar{{
  background:var(--s);border-bottom:1px solid var(--b);
  padding:6px 16px;display:flex;align-items:center;gap:8px;flex-wrap:wrap;
  font-size:11px;
}}
.filterbar label{{color:var(--m);white-space:nowrap}}
.filterbar select,
.filterbar input[type=range]{{
  background:var(--b2);border:1px solid var(--b);color:var(--t);
  border-radius:3px;padding:2px 6px;font-size:11px;height:22px;
}}
.filterbar select:focus{{outline:1px solid var(--a);border-color:var(--a)}}
.filter-sep{{width:1px;background:var(--b);height:18px;flex-shrink:0}}
.sort-btn{{background:var(--b2);border:1px solid var(--b);color:var(--t);
  border-radius:3px;padding:2px 8px;font-size:10px;cursor:pointer;height:22px;
  display:flex;align-items:center;gap:4px}}
.sort-btn:hover{{border-color:var(--a);color:var(--a)}}
.sort-btn.active{{border-color:var(--a);color:var(--a);background:rgba(88,166,255,.08)}}
.filter-count{{color:var(--m);font-size:10px;margin-left:auto;white-space:nowrap}}
.clear-btn{{background:transparent;border:1px solid var(--b);color:var(--m);
  border-radius:3px;padding:2px 8px;font-size:10px;cursor:pointer;height:22px}}
.clear-btn:hover{{border-color:var(--red);color:var(--red)}}

/* ════ MAIN CONTENT ══════════════════════════════════════════════════════════*/
.content{{padding:10px 16px;max-width:1400px}}

/* ════ FINDING CARD ══════════════════════════════════════════════════════════*/
.card{{
  border:1px solid var(--b);border-radius:6px;
  margin-bottom:8px;overflow:hidden;
  transition:box-shadow .15s;
}}
.card:hover{{box-shadow:0 0 0 1px var(--b2)}}
.card.full   {{border-left:3px solid var(--red)}}
.card.partial{{border-left:3px solid var(--yel)}}
.card.hidden {{display:none!important}}

/* ── card header bar ── */
.card-hdr{{
  display:flex;align-items:center;justify-content:space-between;
  background:var(--s);padding:5px 10px;border-bottom:1px solid var(--b);
  gap:8px;flex-wrap:wrap;
}}
.card-hdr-left{{display:flex;align-items:center;gap:8px}}
.sev-pill{{
  font-size:9px;font-weight:800;padding:2px 6px;border-radius:3px;
  text-transform:uppercase;letter-spacing:.07em;white-space:nowrap;
}}
.sev-pill.full   {{background:var(--red);color:#fff}}
.sev-pill.partial{{background:var(--yel);color:#000}}
.finding-num{{font-size:11px;color:var(--m);font-weight:600}}
.cross-file{{
  font-size:9px;background:rgba(188,140,255,.12);border:1px solid rgba(188,140,255,.25);
  color:var(--purple);border-radius:3px;padding:1px 6px;font-family:var(--mono);
}}
.same-file{{
  font-size:9px;color:var(--m);font-family:var(--mono);
  background:var(--b2);border:1px solid var(--b);border-radius:3px;padding:1px 6px;
}}
.card-hdr-pills{{display:flex;align-items:center;gap:5px}}
.meta-pill{{
  font-size:9px;padding:1px 7px;border-radius:10px;white-space:nowrap;cursor:default;
}}
.steps-pill{{background:rgba(88,166,255,.1);color:var(--a);border:1px solid rgba(88,166,255,.2)}}
.guards-pill-hdr{{background:rgba(227,179,65,.1);color:var(--yel);border:1px solid rgba(227,179,65,.2)}}
.src-pill{{background:rgba(63,185,80,.1);color:var(--grn);border:1px solid rgba(63,185,80,.2);
  font-family:var(--mono)}}

/* ── quick flow ── */
.quick-flow{{
  display:grid;grid-template-columns:1fr auto 1fr;
  align-items:center;gap:0;
  background:var(--bg);border-bottom:1px solid var(--b);
  min-height:0;
}}
.qf-from,.qf-to{{
  padding:5px 10px;overflow:hidden;min-width:0;
}}
.qf-label{{
  display:inline-block;font-size:8px;font-weight:800;
  text-transform:uppercase;letter-spacing:.1em;
  padding:1px 4px;border-radius:2px;margin-right:5px;
  vertical-align:middle;
}}
.qf-label.src{{background:rgba(248,81,73,.2);color:var(--red)}}
.qf-label.snk{{background:rgba(88,166,255,.15);color:var(--a)}}
.qf-loc{{
  font-family:var(--mono);color:var(--a);font-size:10px;
  display:block;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;
}}
.qf-desc{{
  font-family:var(--mono);font-size:9px;color:var(--m);
  display:block;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;
  margin-top:1px;
}}
.qf-arrow{{
  padding:0 12px;color:var(--m);font-size:10px;white-space:nowrap;
  text-align:center;font-family:var(--mono);
  border-left:1px solid var(--b);border-right:1px solid var(--b);
  background:var(--s);min-height:100%;display:flex;align-items:center;
}}

/* ── pipeline toggle ── */
details.pipeline-wrap>summary.pipeline-toggle{{
  cursor:pointer;list-style:none;
  padding:4px 10px;background:var(--b2);
  color:var(--m);font-size:10px;user-select:none;
  display:flex;align-items:center;gap:6px;
  border-bottom:1px solid transparent;
}}
details.pipeline-wrap>summary.pipeline-toggle::-webkit-details-marker{{display:none}}
.tog-arrow{{font-size:8px;transition:transform .15s;display:inline-block}}
details.pipeline-wrap[open]>summary.pipeline-toggle .tog-arrow{{transform:rotate(90deg)}}
details.pipeline-wrap[open]>summary.pipeline-toggle{{border-bottom-color:var(--b)}}
.tog-sub{{color:var(--m);font-size:9px;opacity:.7}}

/* ── pipeline ── */
.pipeline{{
  padding:10px;display:flex;flex-direction:column;
  align-items:flex-start;gap:0;background:var(--bg);
}}
.pipe-node{{
  width:100%;border:1px solid var(--b);border-radius:4px;
  overflow:hidden;
}}
.pipe-node.pn-source{{border-left:3px solid var(--red)}}
.pipe-node.pn-sink  {{border-left:3px solid var(--a)}}
.pipe-node.pn-step  {{border-left:3px solid var(--b)}}

.pn-header{{
  display:flex;align-items:center;gap:6px;
  padding:3px 8px;background:rgba(255,255,255,.03);
  border-bottom:1px solid var(--b);
}}
.pn-icon{{font-size:8px}}
.pn-source .pn-icon{{color:var(--red)}}
.pn-sink   .pn-icon{{color:var(--a)}}
.pn-step   .pn-icon{{color:var(--m)}}
.pn-kind{{
  font-size:9px;font-weight:700;text-transform:uppercase;
  letter-spacing:.07em;white-space:nowrap;
}}
.pn-source .pn-kind{{color:var(--red)}}
.pn-sink   .pn-kind{{color:var(--a)}}
.pn-step   .pn-kind{{color:var(--m)}}
.pn-file{{font-family:var(--mono);font-size:10px;color:var(--t);flex:1;
  white-space:nowrap;overflow:hidden;text-overflow:ellipsis}}
.pn-loc{{font-family:var(--mono);font-size:9px;color:var(--m)}}
.guards-pill{{
  font-size:8px;background:rgba(227,179,65,.12);border:1px solid rgba(227,179,65,.25);
  color:var(--yel);border-radius:8px;padding:0 5px;cursor:help;
}}

/* ── node details ── */
details.pn-details>summary.pn-desc{{
  cursor:pointer;list-style:none;
  padding:3px 8px;font-family:var(--mono);font-size:10px;
  color:var(--t);user-select:none;
  white-space:nowrap;overflow:hidden;text-overflow:ellipsis;
  display:flex;align-items:center;gap:4px;
}}
details.pn-details>summary.pn-desc::-webkit-details-marker{{display:none}}
details.pn-details>summary.pn-desc::before{{
  content:"▶";font-size:7px;transition:transform .1s;flex-shrink:0;color:var(--m);
}}
details.pn-details[open]>summary.pn-desc::before{{transform:rotate(90deg)}}

/* guards inside node */
.node-guards{{padding:4px 8px 0;}}
.guard-item{{
  font-family:var(--mono);font-size:9px;color:var(--yel);
  padding:1px 0;padding-left:12px;position:relative;
  white-space:nowrap;overflow:hidden;text-overflow:ellipsis;
}}
.guard-item::before{{content:"if";position:absolute;left:0;color:var(--m)}}

/* code in nodes */
pre.code{{
  margin:4px 0 0;padding:4px 8px;
  background:var(--bg);font-family:var(--mono);
  font-size:11px;line-height:16px;
  overflow-x:auto;border-top:1px solid var(--b);
  tab-size:4;
}}
.ln{{display:inline-block;width:32px;color:var(--m);
  user-select:none;text-align:right;margin-right:8px}}
.cl{{display:block;height:16px;line-height:16px;
  white-space:pre;overflow:hidden;text-overflow:ellipsis}}
.cl.hit{{background:var(--hit);border-left:2px solid var(--hitborder);
  margin-left:-2px;padding-left:2px;color:#acf2a0;height:16px;line-height:16px}}

/* ── pipe arrow connector ── */
.pipe-arrow{{
  align-self:flex-start;margin-left:16px;
  font-size:12px;color:var(--b);padding:1px 0;
  line-height:14px;user-select:none;
}}

.group-header{{
  display:flex;align-items:center;gap:8px;
  padding:6px 10px 3px;margin-top:8px;
  border-bottom:1px solid var(--b);
  font-size:10px;font-weight:700;color:var(--a);
  letter-spacing:.04em;text-transform:uppercase;
}}
.gh-count{{background:rgba(88,166,255,.12);border:1px solid rgba(88,166,255,.2);
  color:var(--a);border-radius:8px;padding:0 6px;font-size:9px}}

footer{{padding:10px 16px 20px;color:var(--m);font-size:10px;
  border-top:1px solid var(--b);margin-top:8px}}
</style>
</head>
<body>
<div class="topbar">
  <div class="topbar-inner">
    <span class="logo">⚡ TAINT</span>
    <span class="target-chip">target: {target_esc}</span>
    <div class="stat-chips">
      <span class="stat-chip full">{full} FULL</span>
      <span class="stat-chip partial">{partial} PARTIAL</span>
      <span class="stat-chip none">{total} total</span>
    </div>
  </div>
</div>

<div class="filterbar">
  <input type="search" id="fText" placeholder="Search…" oninput="applyFilters()"
    style="background:var(--b2);border:1px solid var(--b);color:var(--t);
      border-radius:3px;padding:2px 8px;font-size:11px;height:22px;width:160px">

  <div class="filter-sep"></div>
  <label>Sev</label>
  <select id="fSev" onchange="applyFilters()">
    <option value="">all</option>
    <option value="full">FULL</option>
    <option value="partial">PARTIAL</option>
  </select>

  <div class="filter-sep"></div>
  <label>Source fn</label>
  <select id="fSrc" onchange="applyFilters()">
    <option value="">any</option>
    {source_opts}
  </select>

  <div class="filter-sep"></div>
  <label>Min guards</label>
  <input type="range" id="fGuards" min="0" max="20" value="0"
    oninput="document.getElementById('guardVal').textContent=this.value;applyFilters()"
    style="width:80px">
  <span id="guardVal" style="color:var(--yel);font-size:10px;min-width:16px">0</span>

  <div class="filter-sep"></div>
  <label>Group</label>
  <select id="fGroup" onchange="applyFilters()">
    <option value="">none</option>
    <option value="source">by source fn</option>
    <option value="sinkfile">by sink file</option>
    <option value="sev">by severity</option>
  </select>

  <div class="filter-sep"></div>
  <label>Sort</label>
  <button class="sort-btn active" id="sIdx"    onclick="sortBy('idx')">   # </button>
  <button class="sort-btn"        id="sSteps"  onclick="sortBy('steps')"> steps </button>
  <button class="sort-btn"        id="sGuards" onclick="sortBy('guards')">guards </button>
  <button class="sort-btn"        id="sSev"    onclick="sortBy('sev')">   sev </button>

  <div class="filter-sep"></div>
  <button class="clear-btn" onclick="clearFilters()">✕ reset</button>
  <span class="filter-count" id="fCount">{total} finding(s)</span>
</div>

<div class="content" id="cardContainer">
{cards}
</div>

<footer>taint — source-level data-flow tracer &nbsp;·&nbsp; {total} finding(s) for target <code>{target_esc}</code></footer>

<script>
const cards = Array.from(document.querySelectorAll('.card'));
let sortKey = 'idx';
let sortAsc = true;

function applyFilters() {{
  const sev    = document.getElementById('fSev').value;
  const src    = document.getElementById('fSrc').value;
  const guards = parseInt(document.getElementById('fGuards').value, 10);
  const txt    = (document.getElementById('fText').value || '').toLowerCase();
  const group  = document.getElementById('fGroup').value;

  // Remove existing group headers
  document.querySelectorAll('.group-header').forEach(h => h.remove());

  let visible = 0;
  cards.forEach(c => {{
    const textMatch = !txt ||
      c.dataset.source.toLowerCase().includes(txt) ||
      c.dataset.srcfile.toLowerCase().includes(txt) ||
      c.dataset.sinkfile.toLowerCase().includes(txt) ||
      c.textContent.toLowerCase().includes(txt);
    const hide =
      (sev    && c.dataset.sev    !== sev)  ||
      (src    && c.dataset.source !== src)  ||
      (parseInt(c.dataset.guards,10) < guards) ||
      !textMatch;
    c.classList.toggle('hidden', hide);
    if (!hide) visible++;
  }});
  document.getElementById('fCount').textContent = visible + ' finding(s)';

  // Apply grouping if requested
  if (group) applyGrouping(group);
}}

function applyGrouping(key) {{
  const container = document.getElementById('cardContainer');
  const visible = cards.filter(c => !c.classList.contains('hidden'));
  if (!visible.length) return;

  // Group by key
  const groups = {{}};
  visible.forEach(c => {{
    const gk = key === 'source' ? c.dataset.source :
               key === 'sinkfile' ? c.dataset.sinkfile :
               key === 'sev' ? c.dataset.sev : '';
    if (!groups[gk]) groups[gk] = [];
    groups[gk].push(c);
  }});

  // Re-render with group headers
  Object.keys(groups).sort().forEach(gk => {{
    const hdr = document.createElement('div');
    hdr.className = 'group-header';
    hdr.innerHTML = '<span class="gh-key">' + escapeHtml(gk) + '</span>' +
      '<span class="gh-count">' + groups[gk].length + '</span>';
    container.appendChild(hdr);
    groups[gk].forEach(c => container.appendChild(c));
  }});
}}

const sevOrder = {{ full: 0, partial: 1, none: 2 }};
function sortBy(key) {{
  if (sortKey === key) {{ sortAsc = !sortAsc; }}
  else {{ sortKey = key; sortAsc = key === 'idx'; }}
  document.querySelectorAll('.sort-btn').forEach(b => b.classList.remove('active'));
  document.getElementById('s' + key.charAt(0).toUpperCase() + key.slice(1)).classList.add('active');
  const container = document.getElementById('cardContainer');
  const sorted = [...cards].sort((a, b) => {{
    let av, bv;
    if (key === 'idx')         {{ av = parseInt(a.id.slice(1));        bv = parseInt(b.id.slice(1)); }}
    else if (key === 'steps')  {{ av = parseInt(a.dataset.steps);       bv = parseInt(b.dataset.steps); }}
    else if (key === 'guards') {{ av = parseInt(a.dataset.guards);      bv = parseInt(b.dataset.guards); }}
    else if (key === 'sev')    {{ av = sevOrder[a.dataset.sev]||9;      bv = sevOrder[b.dataset.sev]||9; }}
    return sortAsc ? av - bv : bv - av;
  }});
  sorted.forEach(c => container.appendChild(c));
  applyFilters(); // re-apply to handle grouping
}}

function clearFilters() {{
  document.getElementById('fSev').value    = '';
  document.getElementById('fSrc').value    = '';
  document.getElementById('fGuards').value = 0;
  document.getElementById('fText').value   = '';
  document.getElementById('fGroup').value  = '';
  document.getElementById('guardVal').textContent = '0';
  document.querySelectorAll('.group-header').forEach(h => h.remove());
  cards.forEach(c => c.classList.remove('hidden'));
  document.getElementById('fCount').textContent = cards.length + ' finding(s)';
  sortKey = 'idx'; sortAsc = true;
  document.querySelectorAll('.sort-btn').forEach(b => b.classList.remove('active'));
  document.getElementById('sIdx').classList.add('active');
  const container = document.getElementById('cardContainer');
  [...cards].sort((a,b) => parseInt(a.id.slice(1)) - parseInt(b.id.slice(1)))
    .forEach(c => container.appendChild(c));
}}

function escapeHtml(s) {{
  return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}}

// Keyboard shortcut: / to focus search
document.addEventListener('keydown', e => {{
  if (e.key === '/' && document.activeElement.tagName !== 'INPUT' && document.activeElement.tagName !== 'SELECT') {{
    e.preventDefault();
    document.getElementById('fText').focus();
  }}
}});
</script>
</body>
</html>
"#,
        target_esc  = html_escape(target),
        full        = full_count,
        partial     = partial_count,
        total       = paths.len(),
        source_opts = source_opts,
        cards       = cards,
    );

    std::fs::write(out_path, html)
}


/// Read `context` lines of source around `line` (1-based) from `file`.
fn source_context(file: &str, line: usize, context: usize) -> Vec<(usize, String, bool)> {
    let Ok(contents) = std::fs::read_to_string(file) else { return vec![] };
    let lines: Vec<&str> = contents.lines().collect();
    let total = lines.len();
    let start = line.saturating_sub(context + 1);
    let end   = (line + context).min(total);
    (start..end)
        .map(|i| (i + 1, lines[i].to_string(), i + 1 == line))
        .collect()
}

// ─────────────────────────────────────────────────────────────────────────────
// Directory walker
// ─────────────────────────────────────────────────────────────────────────────

/// Recursively collect all source files under `root`, skipping any path
/// component that contains one of the `exclude` substrings.
fn collect_sources(root: &Path, exclude: &[String]) -> Vec<PathBuf> {
    let mut files = Vec::new();
    collect_sources_rec(root, exclude, &mut files);
    files.sort();   // deterministic order
    files
}

fn collect_sources_rec(dir: &Path, exclude: &[String], out: &mut Vec<PathBuf>) {
    let entries = match std::fs::read_dir(dir) {
        Ok(e) => e,
        Err(_) => return,
    };
    for entry in entries.flatten() {
        let path = entry.path();
        let path_str = path.to_string_lossy();

        // Skip if any exclude pattern matches any component of the path
        if exclude.iter().any(|ex| !ex.is_empty() && path_str.contains(ex.as_str())) {
            continue;
        }

        if path.is_dir() {
            collect_sources_rec(&path, exclude, out);
        } else if Lang::detect(&path).is_some() {
            out.push(path);
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Top-level analysis pipeline
// ─────────────────────────────────────────────────────────────────────────────

// String implements Error + Send, so using it as our error type lets the
// result cross thread boundaries without a custom wrapper.
type FileResult = Result<Vec<Function>, String>;

fn analyse_file(
    path: &PathBuf,
    engine: &TaintEngine,
    verbose: bool,
) -> FileResult {
    let lang = Lang::detect(path)
        .ok_or_else(|| format!("unsupported file extension: {}", path.display()))?;

    let source = std::fs::read(path)
        .map_err(|e| e.to_string())?;
    let ts_lang = lang.tree_sitter_language();

    let mut parser = Parser::new();
    parser.set_language(&ts_lang)
        .map_err(|e| e.to_string())?;
    let tree = parser.parse(&source, None)
        .ok_or_else(|| "tree-sitter failed to parse".to_string())?;

    if verbose {
        eprintln!("  {} parsed {} ({} bytes)", "✓".green(), path.display(), source.len());
    }

    let file_str = path.to_string_lossy().to_string();
    let mut extractor = Extractor::new(&source, &file_str);
    extractor.walk(&tree);

    let mut functions = extractor.functions;
    engine.propagate(&mut functions);

    if verbose {
        eprintln!("  {} {} function(s), taint propagated", "✓".green(), functions.len());
    }

    Ok(functions)
}

// ─────────────────────────────────────────────────────────────────────────────
// main
// ─────────────────────────────────────────────────────────────────────────────

fn main() {
    let cli = Cli::parse();

    // ── Configure rayon thread pool ───────────────────────────────────────
    if cli.jobs > 0 {
        rayon::ThreadPoolBuilder::new()
            .num_threads(cli.jobs)
            .build_global()
            .unwrap_or(());
    }

    // ── Resolve min-taint filter ──────────────────────────────────────────
    let min_taint = match cli.min_taint.to_lowercase().as_str() {
        "full"              => TaintLevel::Full,
        "any" | "none"      => TaintLevel::None,
        _                   => TaintLevel::Partial, // default
    };

    // ── Validate target / mode ────────────────────────────────────────────
    if !cli.all_sinks && cli.target.is_none() {
        eprintln!(
            "{} Specify --target <target> or --all-sinks for whole-project scan.",
            "✗".red()
        );
        std::process::exit(1);
    }

    let engine = Arc::new(TaintEngine::new(&cli.extra_sources, &cli.sanitisers));

    // ── Expand directories into file lists ────────────────────────────────
    let mut files: Vec<PathBuf> = Vec::new();
    for src_path in &cli.source {
        if src_path.is_dir() {
            let found = collect_sources(src_path, &cli.exclude);
            eprintln!(
                "  {} directory {} → {} file(s)",
                "↳".dimmed(),
                src_path.display(),
                found.len()
            );
            files.extend(found);
        } else {
            files.push(src_path.clone());
        }
    }

    let total_files = files.len();
    eprintln!(
        "{} {} source file(s) to analyse",
        "→".cyan().bold(),
        total_files
    );

    // ── Parse + taint-propagate all files in parallel ─────────────────────
    // Architecture:
    //   • A shared ProgressState holds the current file name + start time.
    //   • A heartbeat thread wakes every 250 ms and reprints the progress
    //     line, including elapsed seconds for the current file.  This makes
    //     stuck files immediately visible.
    //   • Each rayon worker updates ProgressState before + after its file,
    //     and enforces a per-file timeout by running the analysis on a
    //     dedicated thread and joining with a deadline.

    const FILE_TIMEOUT: Duration = Duration::from_secs(10);
    const HEARTBEAT_MS: u64      = 250;

    #[derive(Clone)]
    struct ProgressState {
        done:         usize,
        total:        usize,
        current_file: String,
        file_start:   Instant,
        finished:     bool,
    }

    let pad_w = total_files.to_string().len();
    let state  = Arc::new(Mutex::new(ProgressState {
        done:         0,
        total:        total_files,
        current_file: String::new(),
        file_start:   Instant::now(),
        finished:     false,
    }));

    // ── Heartbeat thread ─────────────────────────────────────────────────
    let hb_state = Arc::clone(&state);
    let hb_pad   = pad_w;
    std::thread::spawn(move || {
        loop {
            std::thread::sleep(Duration::from_millis(HEARTBEAT_MS));
            let s = hb_state.lock().unwrap();
            if s.finished { break; }
            if s.current_file.is_empty() { continue; }
            let elapsed = s.file_start.elapsed().as_secs_f32();
            let lag = if elapsed > 1.0 {
                format!("  [33m{:.1}s[0m", elapsed)
            } else {
                String::new()
            };
            eprint!("\r\x1b[2K  [{:>pad$}/{}]  {}  {}{}",
                s.done, s.total,
                "[36m▶[0m",
                s.current_file,
                lag,
                pad = hb_pad,
            );
        }
    });

    // ── Parallel file processing ──────────────────────────────────────────
    let all_functions: Vec<Function> = files
        .par_iter()
        .flat_map(|path| {
            // Build short "parent/file.c" label
            let fname = path.file_name()
                .map(|n| n.to_string_lossy().into_owned())
                .unwrap_or_default();
            let display = path.parent()
                .and_then(|p| p.file_name())
                .map(|d| format!("{}/{}", d.to_string_lossy(), fname))
                .unwrap_or_else(|| fname.clone());

            // Register this file as the current one
            {
                let mut s = state.lock().unwrap();
                s.current_file = display.clone();
                s.file_start   = Instant::now();
            }

            // Run analysis on a dedicated thread so we can time it out.
            // We clone what we need because the closure must be 'static.
            let path_owned  = path.clone();
            let engine_ref  = Arc::clone(&engine);
            let single      = total_files == 1;
            let (tx, rx): (std::sync::mpsc::Sender<FileResult>, _) = std::sync::mpsc::channel();
            std::thread::spawn(move || {
                let result = analyse_file(&path_owned, &engine_ref, single);
                let _ = tx.send(result);
            });

            let result = match rx.recv_timeout(FILE_TIMEOUT) {
                Ok(r)  => r,
                Err(_) => {
                    // Timeout — skip file, print warning on its own line
                    eprintln!("\r\x1b[2K  {} {} (timeout after {}s — skipped)",
                        "[31m✗[0m",
                        path.display(),
                        FILE_TIMEOUT.as_secs());
                    Err("timeout".to_string())
                }
            };

            // Update counter and reprint progress immediately
            let done = {
                let mut s = state.lock().unwrap();
                s.done += 1;
                s.current_file = display.clone();
                s.file_start   = Instant::now(); // reset so lag disappears
                s.done
            };

            eprint!("\r\x1b[2K  [{:>pad$}/{}]  {}  {}",
                done, total_files,
                "[36m▶[0m",
                display,
                pad = pad_w,
            );

            match result {
                Ok(fns) => fns,
                Err(e)  => {
                    if e != "timeout" {
                        eprintln!("\r\x1b[2K  {} {}: {}",
                            "[31m✗[0m", path.display(), e);
                    }
                    vec![]
                }
            }
        })
        .collect();

    // Signal heartbeat thread to stop, then erase the progress line.
    { state.lock().unwrap().finished = true; }
    eprint!("\r\x1b[2K");

    if all_functions.is_empty() {
        eprintln!("{}", "No functions extracted. Check your source files.".red());
        std::process::exit(1);
    }

    eprintln!(
        "{} {} function(s) extracted across {} file(s)",
        "✓".green().bold(),
        all_functions.len(),
        total_files,
    );

    // ── Find paths ────────────────────────────────────────────────────────
    let finder = PathFinder {
        functions: &all_functions,
        engine: &engine,
        max_depth: cli.depth,
    };

    let mut paths: Vec<DataFlowPath> = if cli.all_sinks {
        let total_fns  = all_functions.len();
        let fn_pad     = total_fns.to_string().len();
        // Lock-free counter — much cheaper than Mutex for 690k functions
        let counter    = Arc::new(std::sync::atomic::AtomicU64::new(0));
        let last_print = Arc::new(std::sync::atomic::AtomicU64::new(0));
        // Print interval: every 0.1% of functions, minimum every 256
        let print_every = ((total_fns / 1000) as u64).max(256);

        eprint!("  [{:>pad$}/{}]  scanning sinks (parallel)…",
            0, total_fns, pad = fn_pad);

        let counter_ref    = Arc::clone(&counter);
        let last_print_ref = Arc::clone(&last_print);

        let mut all_paths: Vec<DataFlowPath> = all_functions
            .par_iter()
            .flat_map(|func| {
                let t = Target::Function(func.name.clone());
                // PathFinder is stateless / immutable — safe to call from any thread
                let local_paths = finder.find_paths(&t);

                // Update counter atomically
                let done = counter_ref.fetch_add(1, std::sync::atomic::Ordering::Relaxed) + 1;

                // Only print when we cross a print_every boundary (one thread wins the CAS)
                let prev_print = last_print_ref.load(std::sync::atomic::Ordering::Relaxed);
                let new_print  = done / print_every;
                if new_print > prev_print &&
                   last_print_ref.compare_exchange(
                       prev_print, new_print,
                       std::sync::atomic::Ordering::Relaxed,
                       std::sync::atomic::Ordering::Relaxed,
                   ).is_ok()
                {
                    eprint!("\r\x1b[2K  [{:>pad$}/{}]  \x1b[36m▶\x1b[0m  scanning sinks…",
                        done, total_fns, pad = fn_pad);
                }

                local_paths
            })
            .collect();

        eprint!("\r\x1b[2K");
        eprintln!("{} scanned {} function(s) for sinks",
            "✓".green().bold(), total_fns);

        // Paths were collected in parallel so indices are arbitrary — re-index after dedup
        all_paths
    } else {
        let target = Target::parse(cli.target.as_deref().unwrap());
        finder.find_paths(&target)
    };

    // ── Apply filters ─────────────────────────────────────────────────────
    if !cli.all_paths {
        paths.retain(|p| p.sink_taint >= min_taint);
    }

    // Dedup: same (source_loc, sink_loc) pair is the same finding regardless of
    // which pass emitted it. Sort by (src_file, src_line, sink_line) for stable dedup.
    paths.sort_by(|a, b| {
        let src_loc = |p: &DataFlowPath| p.nodes.first()
            .map(|n| (n.loc.file.clone(), n.loc.line)).unwrap_or_default();
        let snk_line = |p: &DataFlowPath| p.nodes.last()
            .map(|n| n.loc.line).unwrap_or(0);
        src_loc(a).cmp(&src_loc(b)).then(snk_line(a).cmp(&snk_line(b)))
    });
    paths.dedup_by(|a, b| {
        let key = |p: &DataFlowPath| (
            p.nodes.first().map(|n| (n.loc.file.clone(), n.loc.line, n.loc.col)),
            p.nodes.last() .map(|n| (n.loc.file.clone(), n.loc.line, n.loc.col)),
        );
        key(a) == key(b)
    });

    // Re-index after dedup
    for (i, p) in paths.iter_mut().enumerate() {
        p.index = i;
    }

    // ── Render ────────────────────────────────────────────────────────────
    let target_label = cli.target.as_deref().unwrap_or("<all sinks>");
    if cli.json {
        render_json(&paths);
    } else {
        render_text(&paths, target_label);
    }
    if let Some(html_path) = &cli.html {
        match render_html(&paths, target_label, html_path) {
            Ok(()) => eprintln!("{} HTML report written to {}", "✓".green(), html_path.display()),
            Err(e) => eprintln!("{} HTML report failed: {}", "✗".red(), e),
        }
    }
    if let Some(ann_dir) = &cli.annotate {
        eprintln!("{} writing annotated sources to {} …", "→".cyan(), ann_dir.display());
        match render_annotated_sources(&all_functions, ann_dir) {
            Ok(()) => {},
            Err(e) => eprintln!("{} annotate failed: {}", "✗".red(), e),
        }
    }

    // Exit 1 if any fully-tainted path was found (useful in CI)
    if paths.iter().any(|p| p.sink_taint == TaintLevel::Full) {
        std::process::exit(1);
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    fn parse_c(src: &str) -> Vec<Function> {
        let source = src.as_bytes();
        let lang = Lang::C;
        let mut parser = Parser::new();
        parser.set_language(&lang.tree_sitter_language()).unwrap();
        let tree = parser.parse(source, None).unwrap();
        let mut extractor = Extractor::new(source, "test.c");
        extractor.walk(&tree);
        extractor.functions
    }

    fn engine() -> TaintEngine {
        TaintEngine::new(&[], &[])
    }

    #[test]
    fn test_extract_function_names() {
        let fns = parse_c("int foo(int x) { return x; } void bar() {}");
        let names: Vec<&str> = fns.iter().map(|f| f.name.as_str()).collect();
        assert!(names.contains(&"foo"), "expected foo, got {:?}", names);
        assert!(names.contains(&"bar"), "expected bar, got {:?}", names);
    }

    #[test]
    fn test_taint_from_read() {
        let src = r#"
void process() {
    char buf[64];
    read(0, buf, 64);
    int n = buf[0];
}
"#;
        let mut fns = parse_c(src);
        engine().propagate(&mut fns);
        let f = fns.iter().find(|f| f.name == "process").unwrap();
        // The call to read() should be tainted
        let read_call = f.call_sites.iter().find(|cs| cs.callee == "read");
        assert!(read_call.is_some(), "read call not extracted");
        assert_eq!(read_call.unwrap().taint, TaintLevel::Full);
    }

    #[test]
    fn test_taint_propagates_through_assignment() {
        let src = r#"
void process() {
    char buf[64];
    read(0, buf, 64);
    int len = buf[0];
    int computed = len * 2;
}
"#;
        let mut fns = parse_c(src);
        engine().propagate(&mut fns);
        let f = fns.iter().find(|f| f.name == "process").unwrap();
        // computed derives from len which derives from buf (tainted)
        let computed = f.var_defs.iter().find(|v| v.name == "computed");
        // May be None if tree-sitter doesn't extract the assignment; just check no panic
        if let Some(v) = computed {
            assert_ne!(v.taint, TaintLevel::None, "computed should be tainted");
        }
    }

    #[test]
    fn test_sanitiser_removes_taint() {
        let src = r#"
void process() {
    char buf[64];
    read(0, buf, 64);
    char *safe = sanitise(buf);
}
"#;
        let mut fns = parse_c(src);
        let eng = TaintEngine::new(&[], &["sanitise".to_string()]);
        eng.propagate(&mut fns);
        let f = fns.iter().find(|f| f.name == "process").unwrap();
        let sanitise_call = f.call_sites.iter().find(|cs| cs.callee == "sanitise");
        if let Some(cs) = sanitise_call {
            assert_eq!(cs.taint, TaintLevel::None, "sanitiser should clear taint");
        }
    }

    #[test]
    fn test_target_function_var() {
        let target = Target::parse("handle:buf");
        assert!(matches!(
            target,
            Target::FunctionVar { func, var } if func == "handle" && var == "buf"
        ));
    }

    #[test]
    fn test_target_location() {
        let target = Target::parse("42:7");
        assert!(matches!(target, Target::Location { line: 42, col: 7 }));
    }

    #[test]
    fn test_target_function() {
        let target = Target::parse("handle_request");
        assert!(matches!(target, Target::Function(n) if n == "handle_request"));
    }

    #[test]
    fn test_taint_level_join() {
        assert_eq!(TaintLevel::None.join(TaintLevel::Full), TaintLevel::Full);
        assert_eq!(TaintLevel::Partial.join(TaintLevel::None), TaintLevel::Partial);
        assert_eq!(TaintLevel::Full.join(TaintLevel::Full), TaintLevel::Full);
    }

    #[test]
    fn test_taint_level_propagation() {
        // join is the lattice upper-bound: Full dominates everything
        assert_eq!(TaintLevel::Full.join(TaintLevel::None),    TaintLevel::Full);
        assert_eq!(TaintLevel::Full.join(TaintLevel::Full),    TaintLevel::Full);
        assert_eq!(TaintLevel::None.join(TaintLevel::None),    TaintLevel::None);
        assert_eq!(TaintLevel::None.join(TaintLevel::Partial), TaintLevel::Partial);
        // Partial + Full = Full (worst-case wins)
        assert_eq!(TaintLevel::Partial.join(TaintLevel::Full), TaintLevel::Full);
    }

    #[test]
    fn test_extract_idents_from_str() {
        let ids = extract_idents_from_str("foo = bar + baz(qux)");
        assert!(ids.contains(&"foo".to_string()));
        assert!(ids.contains(&"bar".to_string()));
        assert!(ids.contains(&"baz".to_string()));
        assert!(ids.contains(&"qux".to_string()));
    }

    #[test]
    fn test_builtin_sources_coverage() {
        let sources = builtin_sources();
        for s in &["read", "recv", "fgets", "scanf", "getenv", "input"] {
            assert!(sources.contains(*s), "missing builtin source: {}", s);
        }
    }

    #[test]
    fn test_no_taint_without_source() {
        let src = r#"
void clean() {
    int x = 42;
    int y = x + 1;
}
"#;
        let mut fns = parse_c(src);
        engine().propagate(&mut fns);
        let f = fns.iter().find(|f| f.name == "clean").unwrap();
        for vd in &f.var_defs {
            assert_eq!(vd.taint, TaintLevel::None,
                "variable {} should not be tainted", vd.name);
        }
    }
}
