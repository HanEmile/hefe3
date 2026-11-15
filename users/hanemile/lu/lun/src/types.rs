use crate::error::LunError;
use std::rc::Rc;
use std::cell::RefCell;

pub type NativeFn = fn(Vec<Value>) -> Result<Value, LunError>;

#[derive(Clone)]
pub enum Value {
    Number(f64),
    String(String),
    List(Vec<Value>),
    Function {
        params: Vec<String>,
        vararg: Option<String>,
        body: crate::parser::AstNode,
        env: Rc<RefCell<Environment>>,
    },
    Macro {
        params: Vec<String>,
        body: crate::parser::AstNode,
        env: Rc<RefCell<Environment>>,
    },
    Native(NativeFn),
}

impl std::fmt::Debug for Value {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Value::Number(n) => write!(f, "Number({:?})", n),
            Value::String(s) => write!(f, "String({:?})", s),
            Value::List(l) => write!(f, "List({:?})", l),
            Value::Function { params, .. } => write!(f, "Function({:?})", params),
            Value::Macro { params, .. } => write!(f, "Macro({:?})", params),
            Value::Native(_) => write!(f, "NativeFn"),
        }
    }
}

impl PartialEq for Value {
    fn eq(&self, other: &Self) -> bool {
        match (self, other) {
            (Value::Number(a), Value::Number(b)) => a == b,
            (Value::String(a), Value::String(b)) => a == b,
            (Value::List(a), Value::List(b)) => a == b,
            (Value::Native(a), Value::Native(b)) => *a as usize == *b as usize,
            _ => false,
        }
    }
}

#[derive(Debug, Clone)]
pub struct Environment {
    pub vars: std::collections::HashMap<String, Value>,
    pub macros: std::collections::HashMap<String, Value>,
    pub loaded_files: std::collections::HashSet<String>,
    pub parent: Option<Rc<RefCell<Environment>>>,
}

impl Environment {
    pub fn new() -> Self {
        Environment {
            vars: std::collections::HashMap::new(),
            macros: std::collections::HashMap::new(),
            loaded_files: std::collections::HashSet::new(),
            parent: None,
        }
    }
    
    pub fn mark_loaded(&mut self, filename: String) {
        self.loaded_files.insert(filename);
    }
    
    pub fn define(&mut self, name: String, value: Value) {
        self.vars.insert(name, value);
    }
    
    pub fn define_macro(&mut self, name: String, value: Value) {
        self.macros.insert(name, value);
    }
    
    pub fn get(&self, name: &str) -> Option<Value> {
        if let Some(v) = self.vars.get(name) {
            Some(v.clone())
        } else if let Some(ref p) = self.parent {
            p.borrow().get(name)
        } else {
            None
        }
    }
    
    pub fn get_macro(&self, name: &str) -> Option<Value> {
        if let Some(v) = self.macros.get(name) {
            Some(v.clone())
        } else if let Some(ref p) = self.parent {
            p.borrow().get_macro(name)
        } else {
            None
        }
    }
    
    pub fn is_loaded(&self, filename: &str) -> bool {
        self.loaded_files.contains(filename)
            || self.parent.as_ref().map_or(false, |p| p.borrow().is_loaded(filename))
    }
}
