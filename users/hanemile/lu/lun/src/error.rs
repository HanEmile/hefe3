use std::fmt;

#[derive(Debug, Clone)]
pub enum LunError {
    Parse(String),
    Eval(String),
    Io(String),
    Arity(String),
    Type(String),
    UnknownSymbol(String),
}

impl fmt::Display for LunError {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match self {
            LunError::Parse(s) => write!(f, "Parse Error: {}", s),
            LunError::Eval(s) => write!(f, "Eval Error: {}", s),
            LunError::Io(s) => write!(f, "IO Error: {}", s),
            LunError::Arity(s) => write!(f, "Arity Error: {}", s),
            LunError::Type(s) => write!(f, "Type Error: {}", s),
            LunError::UnknownSymbol(s) => write!(f, "Unknown Symbol: {}", s),
        }
    }
}

impl std::error::Error for LunError {}

impl From<std::io::Error> for LunError {
    fn from(err: std::io::Error) -> Self {
        LunError::Io(err.to_string())
    }
}

impl From<String> for LunError {
    fn from(s: String) -> Self {
        LunError::Eval(s)
    }
}

impl From<&str> for LunError {
    fn from(s: &str) -> Self {
        LunError::Eval(s.to_string())
    }
}
