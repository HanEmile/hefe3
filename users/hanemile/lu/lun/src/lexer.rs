pub type Tokens = Vec<String>;

pub trait Tokenizable {
    fn tokenize(&self) -> Tokens;
}

impl Tokenizable for str {
    fn tokenize(&self) -> Tokens {
        tokenize_str(self)
    }
}

impl Tokenizable for String {
    fn tokenize(&self) -> Tokens {
        tokenize_str(self)
    }
}

pub fn tokenize_str(input: &str) -> Tokens {
    let mut tokens = Vec::new();
    let mut current = String::new();
    let mut in_string = false;
    let mut escape = false;
    
    let chars: Vec<char> = input.chars().collect();
    let mut pos = 0;
    
    while pos < chars.len() {
        let ch = chars[pos];
        
        if in_string {
            if escape {
                current.push(ch);
                escape = false;
            } else if ch == '\\' {
                escape = true;
            } else if ch == '"' {
                in_string = false;
                current.push('"');
                tokens.push(current.clone());
                current.clear();
            } else {
                current.push(ch);
            }
            pos += 1;
            continue;
        }
        
        match ch {
            ';' => {
                // Comment, skip rest of line
                while pos < chars.len() && chars[pos] != '\n' {
                    pos += 1;
                }
            }
            '"' => {
                if !current.is_empty() {
                    tokens.push(current.clone());
                    current.clear();
                }
                in_string = true;
                current.push('"');
                pos += 1;
            }
            '(' | ')' => {
                if !current.is_empty() {
                    tokens.push(current.clone());
                    current.clear();
                }
                tokens.push(ch.to_string());
                pos += 1;
            }
            c if c.is_whitespace() => {
                if !current.is_empty() {
                    tokens.push(current.clone());
                    current.clear();
                }
                pos += 1;
            }
            c => {
                current.push(c);
                pos += 1;
            }
        }
        
        // Check for (markdown MARKER start
        if tokens.len() >= 2 && tokens[tokens.len()-1] == "markdown" && tokens[tokens.len()-2] == "(" && current.is_empty() {
             // Look ahead for marker
             let mut p = pos;
             // skip whitespace
             while p < chars.len() && chars[p].is_whitespace() { p += 1; }
             
             let mut marker = String::new();
             while p < chars.len() && !chars[p].is_whitespace() && chars[p] != ')' {
                 marker.push(chars[p]);
                 p += 1;
             }
             
             if !marker.is_empty() {
                 // Found marker, consume it from input stream so regular tokenizer doesn't see it yet?
                 // No, standard tokenizer loop will see it next.
                 // We want to hijack reading until end marker.
                 pos = p; // Advance past marker
                 
                 let mut content = String::new();
                 let end_marker = marker.clone();
                 let mut matched_end = false;
                 
                 // Read until whitespace + end_marker + whitespace/paren
                 while pos < chars.len() {
                     // Check if we are at end_marker
                     // We need to match end_marker, followed by `)` or whitespace?
                     // Actually parser expects: (markdown MARKER ... MARKER)
                     // So we need to end at MARKER. The closing `)` will be tokenized normally.
                     // Wait, `tokenize_str` produces tokens.
                     // If we consume MARKER here, we should NOT emit it as a token?
                     // Previous implementation consumed MARKER and returns content string.
                     // The parser sees `(markdown "content")`.
                     // So we must remove `markdown` and `(`? No.
                     // `tokens` has `(`, `markdown`.
                     // Next we want a string token representing the content.
                     // And we should swallow the markers.
                     // So:
                     // 1. Consume content until we see `\sMARKER`.
                     // 2. Add content as string token.
                     // 3. Continue tokenization.
                     
                     // Helper to check if we are at end marker
                     let remaining = chars.len() - pos;
                     if remaining >= end_marker.len() {
                         let slice: String = chars[pos..pos+end_marker.len()].iter().collect();
                         if slice == end_marker {
                             // Check if followed by boundary
                             let next_char = if pos + end_marker.len() < chars.len() { chars[pos + end_marker.len()] } else { ' ' };
                             if next_char.is_whitespace() || next_char == ')' {
                                 matched_end = true;
                                 pos += end_marker.len();
                                 break;
                             }
                         }
                     }
                     
                     content.push(chars[pos]);
                     pos += 1;
                 }
                 
                 if matched_end {
                     // We captured content (including embedded newlines).
                     // Add as a string token (with quotes for standard string handling)
                     // Or just raw string? AstNode::Atom handles it?
                     // Tokenizer produces strings.
                     // If I push "content", parse_expr will see Atom("content").
                     // If I push "\"content\"", parse_expr sees Atom("\"content\"").
                     // Standard string tokenization includes quotes: `current.push('"'); ... current.push('"');`.
                     // So we should wrap in quotes and escape?
                     // Or assume `markdown` returns a raw string known to not need escaping logic here, 
                     // but `Value::String` needs unescaped string.
                     // `parse_expr` treats `Atom` starting with `"` as string literal.
                     // So we should probably escape it?
                     // Simplest: Just push the raw content string, but wrap in quotes to mark it as string?
                     // Actually, `markdown` special form evaluates its argument.
                     // If we pass a string literal token, it evaluates to string value.
                     // A string literal token MUST start/end with `"`.
                     // So yes, we need to format it as a string literal.
                     // `format!("{:?}", content)` does exactly that (escapes quotes etc).
                     tokens.push(format!("{:?}", content));
                 }
             }
        }
    }
    
    if !current.is_empty() {
        tokens.push(current);
    }
    
    tokens
}
