/// Small helpers shared across panels.
///
/// Last reviewed: 2026-05-01 | Review frequency: monthly

/// Truncate `s` to at most `max` *characters* (not bytes), appending an
/// ellipsis when truncation actually happens. Used by panel headers and
/// dropdowns to keep long asset ids / collection names readable.
pub fn trim(s: &str, max: usize) -> String {
    if s.chars().count() <= max {
        s.to_string()
    } else {
        let cut: String = s.chars().take(max.saturating_sub(1)).collect();
        format!("{cut}…")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn passes_through_short_strings() {
        assert_eq!(trim("abc", 5), "abc");
        assert_eq!(trim("abcde", 5), "abcde");
    }

    #[test]
    fn ellipses_when_too_long() {
        assert_eq!(trim("abcdef", 5), "abcd…");
    }

    #[test]
    fn handles_unicode_by_chars_not_bytes() {
        // 5 chars even though byte length is larger.
        assert_eq!(trim("a→b→c", 5), "a→b→c");
        assert_eq!(trim("a→b→cd", 5), "a→b→…");
    }
}
