mod lexer;
mod parser;
mod types;
mod natives;
mod eval;
mod error;
#[cfg(test)]
mod tests;

use crate::types::Environment;
use crate::lexer::Tokenizable;
use crate::parser::Parsable;
use crate::eval::Evaluatable;

/// Main entry point: loads lun.lun standard library, then evaluates specified files
fn main() -> Result<(), Box<dyn std::error::Error>> {
    let env = std::rc::Rc::new(std::cell::RefCell::new(Environment::new()));
    
    // Load standard library first (lun.lun which defines foreign mappings)
    if let Ok(content) = std::fs::read_to_string("lun.lun") {
        env.borrow_mut().mark_loaded("lun.lun".to_string());
        // Handle parser error
        let ast = content.tokenize().parse()?;
        ast.evaluate_with_env(env.clone())?;
    }

    // Load functions.lun which defines the language functions
    if let Ok(content) = std::fs::read_to_string("functions.lun") {
        env.borrow_mut().mark_loaded("functions.lun".to_string());
        let ast = content.tokenize().parse()?;
        ast.evaluate_with_env(env.clone())?;
    }
    
    // Get files to evaluate from command line arguments or default to main.lun
    let args: Vec<String> = std::env::args().collect();
    let files: Vec<&str> = if args.len() > 1 {
        args[1..].iter().map(|s| s.as_str()).collect()
    } else {
        vec!["main.lun"]
    };
    
    // Evaluate each file
    for file in files {
        if !env.borrow().is_loaded(file) {
            env.borrow_mut().mark_loaded(file.to_string());
            let content = std::fs::read_to_string(file)?;
            let ast = content.tokenize().parse()?;
            match ast.evaluate_with_env(env.clone()) {
                Ok(result) => println!("{}: {:?}", file, result),
                Err(e) => eprintln!("Error evaluating {}: {}", file, e),
            }
        }
    }
    
    Ok(())
}
