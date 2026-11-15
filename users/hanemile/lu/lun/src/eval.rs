use std::rc::Rc;
use std::cell::RefCell;
use std::collections::{HashMap, HashSet};

use crate::types::{Value, Environment};
use crate::parser::{Ast, AstNode, Parsable};
use crate::lexer::Tokenizable;
use crate::natives::get_native_fn;
use crate::error::LunError;

pub trait Evaluatable {
    fn evaluate_with_env(self, env: Rc<RefCell<Environment>>) -> Result<Value, LunError>;
}

impl Evaluatable for Ast {
    fn evaluate_with_env(self, env: Rc<RefCell<Environment>>) -> Result<Value, LunError> {
        let mut last_value = Value::Number(0.0);
        for expr in &self.exprs {
            last_value = evaluate_node(expr, env.clone())?;
        }
        Ok(last_value)
    }
}

pub fn evaluate_node(node: &AstNode, env: Rc<RefCell<Environment>>) -> Result<Value, LunError> {
    // First, expand macros
    let expanded = expand_macros(node, env.clone())?;
    
    match &expanded {
        AstNode::Atom(s) => eval_atom(s, env),
        AstNode::List(list) => eval_list(list, env),
    }
}

fn expand_macros(node: &AstNode, env: Rc<RefCell<Environment>>) -> Result<AstNode, LunError> {
    match node {
        AstNode::Atom(_) => Ok(node.clone()),
        AstNode::List(list) => {
            if list.is_empty() {
                return Ok(node.clone());
            }
            
            // Check if this is a macro call
            if let AstNode::Atom(name) = &list[0] {
                if let Some(macro_val) = env.borrow().get_macro(name) {
                    // This is a macro call - expand it
                    return expand_macro_call(macro_val, &list[1..], env.clone());
                }
            }
            
            // Not a macro call - recursively expand children
            let expanded_children: Result<Vec<AstNode>, LunError> = list.iter()
                .map(|child| expand_macros(child, env.clone()))
                .collect();
            Ok(AstNode::List(expanded_children?))
        }
    }
}

fn expand_macro_call(macro_val: Value, args: &[AstNode], _: Rc<RefCell<Environment>>) -> Result<AstNode, LunError> {
    match macro_val {
        Value::Macro { params, body, env: macro_env } => {
            // Check arity
            if args.len() != params.len() {
                return Err(LunError::Arity(format!("Macro arity mismatch: expected {}, got {}", params.len(), args.len())));
            }
            
            // Create new environment with macro parameters bound to UNEVALUATED arguments
            let mut new_vars = HashMap::new();
            for (param, arg) in params.iter().zip(args.iter()) {
                // Convert AstNode to Value for binding
                // For macros, we need to pass the AST itself, not evaluate it
                // We'll use a simple conversion here
                new_vars.insert(param.clone(), ast_to_value(arg)?);
            }
            
            let expansion_env = Rc::new(RefCell::new(Environment {
                vars: new_vars,
                macros: HashMap::new(),
                loaded_files: HashSet::new(),
                parent: Some(macro_env),
            }));
            
            // Evaluate the macro body to get the expansion
            let expansion_value = evaluate_node(&body, expansion_env)?;
            
            // Convert the result back to AST
            value_to_ast(&expansion_value)
        }
        _ => Err(LunError::Type("Not a macro".into()))
    }
}

// Helper: Convert AstNode to Value (for macro parameter binding)
fn ast_to_value(node: &AstNode) -> Result<Value, LunError> {
    match node {
        AstNode::Atom(s) => {
            // Try to parse as number
            if let Ok(n) = s.parse::<f64>() {
                Ok(Value::Number(n))
            } else if s.starts_with('"') && s.ends_with('"') {
                Ok(Value::String(s[1..s.len()-1].to_string()))
            } else {
                // It's a symbol - represent as string
                Ok(Value::String(s.clone()))
            }
        }
        AstNode::List(list) => {
            let values: Result<Vec<Value>, LunError> = list.iter()
                .map(|node| ast_to_value(node))
                .collect();
            Ok(Value::List(values?))
        }
    }
}

// Helper: Convert Value back to AstNode (for macro expansion result)
fn value_to_ast(value: &Value) -> Result<AstNode, LunError> {
    match value {
        Value::Number(n) => Ok(AstNode::Atom(n.to_string())),
        Value::String(s) => {
            // If it looks like a symbol (no quotes), return as atom
            // Otherwise, return as quoted string
            if s.chars().all(|c| c.is_alphanumeric() || c == '-' || c == '_' || c == '?' || c == '!') {
                Ok(AstNode::Atom(s.clone()))
            } else {
                Ok(AstNode::Atom(format!("\"{}\"", s)))
            }
        }
        Value::List(list) => {
            let nodes: Result<Vec<AstNode>, LunError> = list.iter()
                .map(|v| value_to_ast(v))
                .collect();
            Ok(AstNode::List(nodes?))
        }
        _ => Err(LunError::Type("Cannot convert function/native to AST".into()))
    }
}


fn eval_atom(s: &str, env: Rc<RefCell<Environment>>) -> Result<Value, LunError> {
    if let Ok(n) = s.parse::<f64>() {
        return Ok(Value::Number(n));
    }
    if s.starts_with('"') && s.ends_with('"') {
        return Ok(Value::String(s[1..s.len()-1].to_string()));
    }
    
    // Variable lookup
    match env.borrow().get(s) {
        Some(v) => Ok(v),
        None => {
            Err(LunError::UnknownSymbol(s.to_string()))
        },
    }
}

fn eval_list(list: &Vec<AstNode>, env: Rc<RefCell<Environment>>) -> Result<Value, LunError> {
    if list.is_empty() {
        return Ok(Value::List(Vec::new()));
    }
    
    // Check for special forms if first element is an atom
    if let AstNode::Atom(func_name) = &list[0] {
        match func_name.as_str() {
            "include" => return eval_include(list, env),
            "require" => return eval_require(list, env),
            "markdown" => return eval_markdown(list, env),
            "foreign" => return eval_foreign(list, env),
            "if" => return eval_if(list, env),
            "lambda" => return eval_lambda(list, env),
            "define" => return eval_define(list, env),
            "defmacro" => return eval_defmacro(list, env),
            "quote" => {
                 if list.len() != 2 { return Err(LunError::Arity("quote requires 1 argument".into())); }
                 // Need to convert AstNode to Value recursively?
                 // Or just return keys? Value::List holds Values.
                 // We can't trivially convert AST to Value without evaluating unless we define quoting.
                 // For now, let's leave quote unimplemented or basic.
                 // Ideally quote would turn (quote (1 2)) into List([Number(1), Number(2)]).
                 // But (quote (a b)) -> List([Unknown(a)...]).
                 // We don't have Symbol type in Value yet? Actually String usually used.
                 // return Err(LunError::Eval("quote not implemented (AST to Value conversion needed)".into()));
                 return ast_to_value(&list[1]);
            }, 
            _ => {} // Not a special form, fall through
        }
    }
    
    evaluate_application(list, env)
}

fn eval_include(list: &Vec<AstNode>, env: Rc<RefCell<Environment>>) -> Result<Value, LunError> {
    if list.len() != 2 { return Err(LunError::Arity("include requires 1 argument".into())); }
    let filename_val = evaluate_node(&list[1], env.clone())?;
    let filename = match filename_val {
        Value::String(s) => s,
        _ => return Err(LunError::Type("include argument must be a string".into()))
    };
    
    let content = std::fs::read_to_string(&filename).map_err(LunError::from)?;
    let ast = content.tokenize().parse()?;
    ast.evaluate_with_env(env)
}

fn eval_require(list: &Vec<AstNode>, env: Rc<RefCell<Environment>>) -> Result<Value, LunError> {
    if list.len() != 2 { return Err(LunError::Arity("require requires 1 argument".into())); }
    let filename_val = evaluate_node(&list[1], env.clone())?;
    let filename = match filename_val {
        Value::String(s) => s,
        _ => return Err(LunError::Type("require argument must be a string".into()))
    };
    
    if env.borrow().is_loaded(&filename) {
        return Ok(Value::Number(0.0));
    }
    env.borrow_mut().mark_loaded(filename.clone());
    
    let content = std::fs::read_to_string(&filename).map_err(LunError::from)?;
    let ast = content.tokenize().parse()?;
    ast.evaluate_with_env(env)
}

fn eval_markdown(list: &Vec<AstNode>, env: Rc<RefCell<Environment>>) -> Result<Value, LunError> {
    if list.len() != 2 { return Err(LunError::Arity("markdown requires content string".into())); }
    evaluate_node(&list[1], env)
}

fn eval_foreign(list: &Vec<AstNode>, env: Rc<RefCell<Environment>>) -> Result<Value, LunError> {
    if list.len() != 2 { return Err(LunError::Arity("foreign requires 1 argument".into())); }
    let name_val = evaluate_node(&list[1], env.clone())?;
    let name_str = match name_val {
        Value::String(s) => s,
        _ => return Err(LunError::Type("foreign argument must be a string".into()))
    };
    match get_native_fn(&name_str) {
        Some(f) => Ok(Value::Native(f)),
        None => Err(LunError::Eval(format!("Unknown native function: {}", name_str)))
    }
}

fn eval_if(list: &Vec<AstNode>, env: Rc<RefCell<Environment>>) -> Result<Value, LunError> {
    if list.len() != 4 { return Err(LunError::Arity("if requires 3 arguments".into())); }
    let cond = evaluate_node(&list[1], env.clone())?;
    let is_true = match cond {
        Value::Number(n) => n != 0.0,
        Value::String(s) => !s.is_empty(),
        Value::List(l) => !l.is_empty(),
        _ => true,
    };
    if is_true { evaluate_node(&list[2], env) } else { evaluate_node(&list[3], env) }
}

fn eval_lambda(list: &Vec<AstNode>, env: Rc<RefCell<Environment>>) -> Result<Value, LunError> {
    if list.len() < 3 { return Err(LunError::Arity("lambda requires params and body".into())); }
    
    let (params, vararg) = match &list[1] {
        AstNode::List(l) => {
            let mut names = Vec::new();
            for n in l {
                match n {
                    AstNode::Atom(s) => names.push(s.clone()),
                    _ => return Err(LunError::Type("lambda param must be atom".into())),
                }
            }
            (names, None)
        },
        AstNode::Atom(s) => (Vec::new(), Some(s.clone())),
    };
    
    // Body logic: implicit begin? Or single expression?
    // Current tests assume single expression or hack it.
    // If list.len() > 3, we simply ignore the rest?
    // Previous implementation: `let body = list[2].clone();`
    // Let's stick to that to maintain behavior, but maybe warn or fix?
    // Fixing it to `App(Begin, [exprs...])` requires `begin`.
    let body = list[2].clone(); 
    Ok(Value::Function {
        params,
        vararg,
        body,
        env: env.clone(),
    })
}

fn eval_define(list: &Vec<AstNode>, env: Rc<RefCell<Environment>>) -> Result<Value, LunError> {
    if list.len() < 3 { return Err(LunError::Arity("define requires 2 arguments".into())); }
    let name = match &list[1] { AstNode::Atom(n) => n.clone(), _ => return Err(LunError::Type("define name must be atom".into())) };
    let value = evaluate_node(&list[2], env.clone())?; 
    env.borrow_mut().define(name, value.clone());
    Ok(value)
}

fn eval_defmacro(list: &Vec<AstNode>, env: Rc<RefCell<Environment>>) -> Result<Value, LunError> {
    // (defmacro name (params) body)
    if list.len() < 4 { return Err(LunError::Arity("defmacro requires name, params, and body".into())); }
    
    let name = match &list[1] {
        AstNode::Atom(n) => n.clone(),
        _ => return Err(LunError::Type("defmacro name must be atom".into()))
    };
    
    let params = match &list[2] {
        AstNode::List(l) => {
            let mut param_names = Vec::new();
            for param in l {
                match param {
                    AstNode::Atom(s) => param_names.push(s.clone()),
                    _ => return Err(LunError::Type("macro param must be atom".into())),
                }
            }
            param_names
        },
        _ => return Err(LunError::Type("defmacro params must be a list".into()))
    };
    
    let body = list[3].clone();
    
    let macro_value = Value::Macro {
        params,
        body,
        env: env.clone(),
    };
    
    env.borrow_mut().define_macro(name.clone(), macro_value.clone());
    Ok(macro_value)
}


fn evaluate_application(list: &Vec<AstNode>, env: Rc<RefCell<Environment>>) -> Result<Value, LunError> {
    // 1. Evaluate first element to get function
    let func_val = evaluate_node(&list[0], env.clone())?;
    
    // 2. Evaluate args
    let args: Result<Vec<Value>, LunError> = list[1..].iter().map(|arg| evaluate_node(arg, env.clone())).collect();
    let args = args?;

    match func_val {
        Value::Function { params, vararg, body, env: closure_env } => {
            let mut new_vars = HashMap::new();
            
            // Check fixed params
            if args.len() < params.len() {
                 return Err(LunError::Arity(format!("Arity mismatch: expected at least {}, got {}", params.len(), args.len())));
            }
            if vararg.is_none() && args.len() > params.len() {
                 return Err(LunError::Arity(format!("Arity mismatch: expected {}, got {}", params.len(), args.len())));
            }
            
            // Bind fixed params
            for (param, arg) in params.iter().zip(args.iter()) {
                new_vars.insert(param.clone(), arg.clone());
            }
            
            // Bind vararg if present
            if let Some(vararg_name) = vararg {
                let remaining_args = args[params.len()..].to_vec();
                new_vars.insert(vararg_name, Value::List(remaining_args));
            }

            let func_env = Rc::new(RefCell::new(Environment {
                vars: new_vars,
                macros: HashMap::new(),
                loaded_files: HashSet::new(),
                parent: Some(closure_env),
            }));
            
            evaluate_node(&body, func_env)
        }
        Value::Native(f) => f(args),
        _ => Err(LunError::Type(format!("Not a function: {:?}", list[0])))
    }
}
