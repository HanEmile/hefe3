use crate::types::{Value, NativeFn};
use crate::error::LunError;

pub fn get_native_fn(name: &str) -> Option<NativeFn> {
    match name {
        "+" => Some(native_add),
        "-" => Some(native_sub),
        "*" => Some(native_mul),
        "/" => Some(native_div),
        "=" => Some(native_eq),
        "<" => Some(native_lt),
        ">" => Some(native_gt),
        "cons" => Some(native_cons),
        "car" => Some(native_car),
        "cdr" => Some(native_cdr),
        "str" => Some(native_str),
        "read" => Some(native_read),
        "write" => Some(native_write),
        "print" => Some(native_print),
        "list-dir" => Some(native_list_dir),
        "str-split" => Some(native_str_split),
        "str-replace" => Some(native_str_replace),
        "str-trim" => Some(native_str_trim),
        "read-lines" => Some(native_read_lines),
        _ => None
    }
}

fn native_add(args: Vec<Value>) -> Result<Value, LunError> {
    num_op(args, |x, y| x + y)
}

fn native_sub(args: Vec<Value>) -> Result<Value, LunError> {
    num_op(args, |x, y| x - y)
}

fn native_mul(args: Vec<Value>) -> Result<Value, LunError> {
    num_op(args, |x, y| x * y)
}

fn native_div(args: Vec<Value>) -> Result<Value, LunError> {
    num_op(args, |x, y| x / y)
}

fn native_eq(args: Vec<Value>) -> Result<Value, LunError> {
    if args.len() != 2 {
        return Err(LunError::Arity("= requires 2 arguments".into()));
    }
    // Deep equality for numbers?
    // Value::eq is already implemented, use that.
    Ok(Value::Number(if args[0] == args[1] { 1.0 } else { 0.0 }))
}

fn native_lt(args: Vec<Value>) -> Result<Value, LunError> {
    cmp_op(args, |x, y| x < y)
}

fn native_gt(args: Vec<Value>) -> Result<Value, LunError> {
    cmp_op(args, |x, y| x > y)
}

fn native_cons(args: Vec<Value>) -> Result<Value, LunError> {
    if args.len() != 2 {
        return Err(LunError::Arity("cons requires 2 arguments".into()));
    }
    let head = args[0].clone();
    let tail = match &args[1] {
        Value::List(l) => l.clone(),
        _ => return Err(LunError::Type("cons second argument must be a list".into())),
    };
    
    let mut new_list = vec![head];
    new_list.extend(tail);
    Ok(Value::List(new_list))
}

fn native_car(args: Vec<Value>) -> Result<Value, LunError> {
    if let [Value::List(l)] = &args[..] {
        l.first().cloned().ok_or_else(|| LunError::Eval("car on empty list".into()))
    } else {
        Err(LunError::Type("car requires a list".into()))
    }
}

fn native_cdr(args: Vec<Value>) -> Result<Value, LunError> {
    if let [Value::List(l)] = &args[..] {
        if l.is_empty() {
            Err(LunError::Eval("cdr on empty list".into()))
        } else {
            Ok(Value::List(l[1..].to_vec()))
        }
    } else {
        Err(LunError::Type("cdr requires a list".into()))
    }
}

fn native_str(args: Vec<Value>) -> Result<Value, LunError> {
    let mut s = String::new();
    for arg in args {
        match arg {
            Value::String(s_val) => s.push_str(&s_val),
            Value::Number(n) => s.push_str(&n.to_string()),
            _ => { /* Ignore or convert? */ }
        }
    }
    Ok(Value::String(s))
}

fn native_read(args: Vec<Value>) -> Result<Value, LunError> {
    if let [Value::String(path)] = &args[..] {
        std::fs::read_to_string(path)
            .map(Value::String)
            .map_err(LunError::from)
    } else {
        Err(LunError::Arity("read requires 1 string argument".into()))
    }
}

fn native_write(args: Vec<Value>) -> Result<Value, LunError> {
    if let [Value::String(path), Value::String(content)] = &args[..] {
        std::fs::write(path, content)
            .map(|_| Value::Number(1.0))
            .map_err(LunError::from)
    } else {
        Err(LunError::Arity("write requires 2 string arguments".into()))
    }
}

fn native_print(args: Vec<Value>) -> Result<Value, LunError> {
    for x in args {
        print!("{:?} ", x);
    }
    println!();
    Ok(Value::Number(0.0))
}

fn num_op(args: Vec<Value>, op: fn(f64, f64) -> f64) -> Result<Value, LunError> {
    // Variadic arithmetic? The primitives definitions in main.rs treated them as binary.
    // But lun.lun defines + as `(lambda args (fold (foreign "+") ...))`?
    // Previous natives `num_op` checked `if let [Number(a), Number(b)]`.
    // So primitives are strictly binary.
    if let [Value::Number(a), Value::Number(b)] = &args[..] {
        Ok(Value::Number(op(*a, *b)))
    } else {
        Err(LunError::Type("Arithmetic op requires 2 numbers".into()))
    }
}

fn cmp_op(args: Vec<Value>, op: fn(f64, f64) -> bool) -> Result<Value, LunError> {
    if let [Value::Number(a), Value::Number(b)] = &args[..] {
        Ok(Value::Number(if op(*a, *b) { 1.0 } else { 0.0 }))
    } else {
        Err(LunError::Type("Comparison op requires 2 numbers".into()))
    }
}

fn native_list_dir(args: Vec<Value>) -> Result<Value, LunError> {
    if let [Value::String(path)] = &args[..] {
        let entries = std::fs::read_dir(path)
            .map_err(LunError::from)?;
        
        let mut files = Vec::new();
        for entry in entries {
            if let Ok(entry) = entry {
                if let Some(name) = entry.file_name().to_str() {
                    files.push(Value::String(name.to_string()));
                }
            }
        }
        Ok(Value::List(files))
    } else {
        Err(LunError::Arity("list-dir requires 1 string argument".into()))
    }
}

fn native_str_split(args: Vec<Value>) -> Result<Value, LunError> {
    if let [Value::String(s), Value::String(delimiter)] = &args[..] {
        let parts: Vec<Value> = s.split(delimiter.as_str())
            .map(|part| Value::String(part.to_string()))
            .collect();
        Ok(Value::List(parts))
    } else {
        Err(LunError::Arity("str-split requires 2 string arguments".into()))
    }
}

fn native_str_replace(args: Vec<Value>) -> Result<Value, LunError> {
    if let [Value::String(s), Value::String(from), Value::String(to)] = &args[..] {
        Ok(Value::String(s.replace(from.as_str(), to.as_str())))
    } else {
        Err(LunError::Arity("str-replace requires 3 string arguments".into()))
    }
}

fn native_str_trim(args: Vec<Value>) -> Result<Value, LunError> {
    if let [Value::String(s)] = &args[..] {
        Ok(Value::String(s.trim().to_string()))
    } else {
        Err(LunError::Arity("str-trim requires 1 string argument".into()))
    }
}

fn native_read_lines(args: Vec<Value>) -> Result<Value, LunError> {
    if let [Value::String(path)] = &args[..] {
        let content = std::fs::read_to_string(path)
            .map_err(LunError::from)?;
        let lines: Vec<Value> = content.lines()
            .map(|line| Value::String(line.to_string()))
            .collect();
        Ok(Value::List(lines))
    } else {
        Err(LunError::Arity("read-lines requires 1 string argument".into()))
    }
}

