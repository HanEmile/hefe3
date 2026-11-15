use crate::lexer::Tokenizable;
use crate::parser::Parsable;
use crate::eval::Evaluatable;
use crate::types::{Value, Environment};
use std::rc::Rc;
use std::cell::RefCell;

fn setup_env() -> Result<Rc<RefCell<Environment>>, Box<dyn std::error::Error>> {
    let env = Rc::new(RefCell::new(Environment::new()));
    
    // Load lun.lun (primitives)
    let content = std::fs::read_to_string("lun.lun").map_err(|e| format!("Could not read lun.lun: {}", e))?;
    env.borrow_mut().mark_loaded("lun.lun".to_string());
    let ast = content.tokenize().parse()?;
    ast.evaluate_with_env(env.clone()).map_err(|e| format!("Failed to load lun.lun: {}", e))?;

    // Load functions.lun
    let content = std::fs::read_to_string("functions.lun").map_err(|e| format!("Could not read functions.lun: {}", e))?;
    env.borrow_mut().mark_loaded("functions.lun".to_string());
    let ast = content.tokenize().parse()?;
    ast.evaluate_with_env(env.clone()).map_err(|e| format!("Failed to load functions.lun: {}", e))?;

    Ok(env)
}

#[test]
fn test_lambda_and_application() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "(define sq (lambda (x) (* x x))) (sq 4)";
    let ast = code.tokenize().parse()?;
    let result = ast.evaluate_with_env(env)?;
    match result {
        Value::Number(n) => assert_eq!(n, 16.0),
        _ => panic!("Expected number"),
    }
    Ok(())
}

#[test]
fn test_map() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "(map (lambda (x) (* x 2)) (list 1 2 3))";
    let ast = code.tokenize().parse()?;
    let result = ast.evaluate_with_env(env)?;
    // Result should be (2 4 6)
    match result {
        Value::List(l) => {
            assert_eq!(l.len(), 3);
            match &l[0] { Value::Number(n) => assert_eq!(*n, 2.0), _ => panic!() }
            match &l[1] { Value::Number(n) => assert_eq!(*n, 4.0), _ => panic!() }
            match &l[2] { Value::Number(n) => assert_eq!(*n, 6.0), _ => panic!() }
        }
        _ => panic!("Expected list, got {:?}", result),
    }
    Ok(())
}

#[test]
fn test_filter() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "(filter (lambda (x) (> x 1)) (list 1 2 3))";
    let ast = code.tokenize().parse()?;
    let result = ast.evaluate_with_env(env)?;
    // Result should be (2 3)
    match result {
        Value::List(l) => {
            assert_eq!(l.len(), 2);
            match &l[0] { Value::Number(n) => assert_eq!(*n, 2.0), _ => panic!() }
            match &l[1] { Value::Number(n) => assert_eq!(*n, 3.0), _ => panic!() }
        }
        _ => panic!("Expected list, got {:?}", result),
    }
    Ok(())
}

#[test]
fn test_arithmetic_variadic() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    
    // Addition
    let val = "(+ 1 2 3)".tokenize().parse()?.evaluate_with_env(env.clone())?;
    match val { Value::Number(n) => assert_eq!(n, 6.0), _ => panic!("Expected 6") }
    
    let val = "(+)".tokenize().parse()?.evaluate_with_env(env.clone())?;
    match val { Value::Number(n) => assert_eq!(n, 0.0), _ => panic!("Expected 0") }
    
    // Multiplication
    let val = "(* 2 3 4)".tokenize().parse()?.evaluate_with_env(env.clone())?;
    match val { Value::Number(n) => assert_eq!(n, 24.0), _ => panic!("Expected 24") }
    
    let val = "(*)".tokenize().parse()?.evaluate_with_env(env.clone())?;
    match val { Value::Number(n) => assert_eq!(n, 1.0), _ => panic!("Expected 1") }
    
    // Subtraction
    let val = "(- 10 2 3)".tokenize().parse()?.evaluate_with_env(env.clone())?;
    match val { Value::Number(n) => assert_eq!(n, 5.0), _ => panic!("Expected 5") }
    
    let val = "(- 10)".tokenize().parse()?.evaluate_with_env(env.clone())?;
    match val { Value::Number(n) => assert_eq!(n, -10.0), _ => panic!("Expected -10") }
    
    // Division
    let val = "(/ 20 2 2)".tokenize().parse()?.evaluate_with_env(env.clone())?;
    match val { Value::Number(n) => assert_eq!(n, 5.0), _ => panic!("Expected 5") }
    
    let val = "(/ 4)".tokenize().parse()?.evaluate_with_env(env.clone())?;
    match val { Value::Number(n) => assert_eq!(n, 0.25), _ => panic!("Expected 0.25") }
    Ok(())
}

#[test]
fn test_comparison() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    
    let codes = vec![
        ("(= 1 1)", 1.0),
        ("(= 1 2)", 0.0),
        ("(!= 1 2)", 1.0),
        ("(!= 1 1)", 0.0),
        ("(< 1 2)", 1.0),
        ("(< 2 1)", 0.0),
        ("(> 2 1)", 1.0),
        ("(> 1 2)", 0.0),
    ];

    for (code, expected) in codes {
        let val = code.tokenize().parse()?.evaluate_with_env(env.clone())?;
        match val { 
            Value::Number(n) => assert_eq!(n, expected, "Failed checking {}", code), 
            _ => panic!("Expected number for {}", code) 
        }
    }
    Ok(())
}

#[test]
fn test_recursion_factorial() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "
        (define fact (lambda (n) 
            (if (< n 2) 
                1 
                (* n (fact (- n 1))))))
        (fact 5)
    ";
    let val = code.tokenize().parse()?.evaluate_with_env(env)?;
    match val { Value::Number(n) => assert_eq!(n, 120.0), _ => panic!("Factorial failed") }
    Ok(())
}

#[test]
fn test_closures_and_scoping() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "
        (define make-adder (lambda (x) (lambda (y) (+ x y))))
        (define add10 (make-adder 10))
        (add10 5)
    ";
    let val = code.tokenize().parse()?.evaluate_with_env(env)?;
    match val { Value::Number(n) => assert_eq!(n, 15.0), _ => panic!("Closure failed") }
    Ok(())
}

#[test]
fn test_list_ops() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    
    // list construction
    let val = "(list 1 2 3)".tokenize().parse()?.evaluate_with_env(env.clone())?;
    match val { Value::List(l) => assert_eq!(l.len(), 3), _ => panic!("List failed") }
    
    // car/first
    let val = "(first (list 10 20))".tokenize().parse()?.evaluate_with_env(env.clone())?;
    match val { Value::Number(n) => assert_eq!(n, 10.0), _ => panic!("First failed") }
    
    // cdr/rest
    let val = "(rest (list 10 20))".tokenize().parse()?.evaluate_with_env(env.clone())?;
    match val { 
        Value::List(l) => {
            assert_eq!(l.len(), 1);
            match &l[0] { Value::Number(n) => assert_eq!(*n, 20.0), _ => panic!() }
        }, 
        _ => panic!("Rest failed") 
    }
    
    // length
    let val = "(length (list 1 2 3 4 5))".tokenize().parse()?.evaluate_with_env(env.clone())?;
    match val { Value::Number(n) => assert_eq!(n, 5.0), _ => panic!("Length failed") }
    
    // nth
    let val = "(nth 2 (list 0 1 2 3))".tokenize().parse()?.evaluate_with_env(env.clone())?;
    match val { Value::Number(n) => assert_eq!(n, 2.0), _ => panic!("Nth failed") }
    
    // range
    let val = "(range 0 4)".tokenize().parse()?.evaluate_with_env(env.clone())?;
    // (0 1 2 3)
    match val { Value::List(l) => assert_eq!(l.len(), 4), _ => panic!("Range failed") }
    Ok(())
}

#[test]
fn test_strings() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "(str-concat \"Hello\" \" \" \"World\")";
    let val = code.tokenize().parse()?.evaluate_with_env(env)?;
    match val { Value::String(s) => assert_eq!(s, "Hello World"), _ => panic!("String concat failed") }
    Ok(())
}

#[test]
fn test_custom_variadic() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    // Identity for args
    let code = "(define my-list (lambda args args)) (my-list 1 2 3)";
    let val = code.tokenize().parse()?.evaluate_with_env(env)?;
    match val { Value::List(l) => assert_eq!(l.len(), 3), _ => panic!("Variadic failed") }
    Ok(())
}

#[test]
fn test_recursion_fibonacci() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "
        (define fib (lambda (n)
            (if (< n 2)
                n
                (+ (fib (- n 1)) (fib (- n 2))))))
        (fib 10)
    ";
    let val = code.tokenize().parse()?.evaluate_with_env(env)?;
    match val { Value::Number(n) => assert_eq!(n, 55.0), _ => panic!("Fib failed") }
    Ok(())
}

#[test]
fn test_closure_counter() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code_shadow = "
        (define x 10)
        (define f (lambda () (define x 20) x))
        (list x (f))
    ";
    let val = code_shadow.tokenize().parse()?.evaluate_with_env(env)?;
    match val {
        Value::List(l) => {
            match &l[0] { Value::Number(n) => assert_eq!(*n, 10.0), _ => panic!() }
            match &l[1] { Value::Number(n) => assert_eq!(*n, 20.0), _ => panic!() }
        }
        _ => panic!("Shadowing test failed")
    }
    Ok(())
}

#[test]
fn test_tokenizer_edge_cases() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    // Empty
    let res = "".tokenize().parse()?.evaluate_with_env(env.clone())?;
    match res { Value::Number(n) => assert_eq!(n, 0.0), _ => panic!() }
    
    // Comments only
    let res = "; comment\n".tokenize().parse()?.evaluate_with_env(env.clone())?;
    match res { Value::Number(n) => assert_eq!(n, 0.0), _ => panic!() }
    Ok(())
}

#[test]
fn test_parser_unbalanced_open() -> Result<(), Box<dyn std::error::Error>> {
    let _env = setup_env()?;
    let res = "(".tokenize().parse();
    assert!(res.is_err());
    Ok(())
}

#[test]
fn test_parser_unbalanced_close() -> Result<(), Box<dyn std::error::Error>> {
    let _env = setup_env()?;
    let res = ")".tokenize().parse();
    assert!(res.is_err());
    Ok(())
}

#[test]
fn test_logic_truthiness() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    // 0 is false
    let val = "(if 0 10 20)".tokenize().parse()?.evaluate_with_env(env.clone())?;
    match val { Value::Number(n) => assert_eq!(n, 20.0), _ => panic!("0 should be false") }
    
    // 1 is true
    let val = "(if 1 10 20)".tokenize().parse()?.evaluate_with_env(env.clone())?;
    match val { Value::Number(n) => assert_eq!(n, 10.0), _ => panic!("1 should be true") }
    
    // Non-empty list is true
    let val = "(if (list 1) 10 20)".tokenize().parse()?.evaluate_with_env(env.clone())?;
    match val { Value::Number(n) => assert_eq!(n, 10.0), _ => panic!("List should be true") }
    Ok(())
}

#[test]
fn test_recursion_mutual() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "
        (define is-even? (lambda (n) (if (= n 0) 1 (is-odd? (- n 1)))))
        (define is-odd? (lambda (n) (if (= n 0) 0 (is-even? (- n 1)))))
        (list (is-even? 4) (is-odd? 4))
    ";
    let val = code.tokenize().parse()?.evaluate_with_env(env)?;
    match val { 
        Value::List(l) => {
            match l[0] { Value::Number(n) => assert_eq!(n, 1.0), _ => panic!() }
            match l[1] { Value::Number(n) => assert_eq!(n, 0.0), _ => panic!() }
        }
        _ => panic!() 
    }
    Ok(())
}

#[test]
fn test_higher_order_ret() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "
        (define gen (lambda (op) (lambda (x) (op x x))))
        (define doubler (gen +))
        (define squarer (gen *))
        (list (doubler 5) (squarer 5))
    ";
    let val = code.tokenize().parse()?.evaluate_with_env(env)?;
        match val { 
        Value::List(l) => {
            match l[0] { Value::Number(n) => assert_eq!(n, 10.0), _ => panic!() }
            match l[1] { Value::Number(n) => assert_eq!(n, 25.0), _ => panic!() }
        }
        _ => panic!() 
    }
    Ok(())
}

#[test]
fn test_deep_cons() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "(car (cdr (cdr (cons 1 (cons 2 (cons 3 (list)))))))"; // (car (cdr (cdr (1 2 3)))) -> (car (cdr (2 3))) -> (car (3)) -> 3
    let val = code.tokenize().parse()?.evaluate_with_env(env)?;
    match val { Value::Number(n) => assert_eq!(n, 3.0), _ => panic!() }
    Ok(())
}

#[test]
fn test_error_handling() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "(read-file \"non_existent_file_definitely_123.txt\")";
    let res = code.tokenize().parse()?.evaluate_with_env(env);
    assert!(res.is_err());
    Ok(())
}

#[test]
fn test_markdown_heredoc() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "
        (markdown EOF
        # Title
        Body content
        EOF)
    ";
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res {
        Value::String(s) => assert!(s.contains("# Title") && s.contains("Body content")),
        _ => panic!("Expected string from markdown"),
    }
    Ok(())
}
#[test]
fn test_include() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let file_path = "test_include_temp.lun";
    std::fs::write(file_path, "(define included-var 42)")?;
    
    // Cleanup wrapper using closure
    let res = (|| -> Result<(), Box<dyn std::error::Error>> {
        let code = format!("(include \"{}\") included-var", file_path);
        let ast = code.tokenize().parse()?;
        let result = ast.evaluate_with_env(env.clone())?;
        match result {
            Value::Number(n) => assert_eq!(n, 42.0),
            _ => panic!("Expected number from included var"),
        }
        Ok(())
    })();
    
    let _ = std::fs::remove_file(file_path);
    res
}

#[test]
fn test_require_idempotency() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let file_path = "test_require_temp.lun";
    // File increments a counter. If loaded twice by include, counter += 2. If require, only += 1.
    // But we need a global counter.
    // Let's define a variable, and try to redefine it to something else? 
    // Or better: use a side-effect if we had mutable globals. We don't really.
    // But we can check `env.is_loaded`.
    std::fs::write(file_path, "(define required-var 1)")?;
    
    let res = (|| -> Result<(), Box<dyn std::error::Error>> {
        let code = format!("(require \"{}\") (require \"{}\")", file_path, file_path);
        let ast = code.tokenize().parse()?;
        ast.evaluate_with_env(env.clone())?;
        
        assert!(env.borrow().is_loaded(file_path));
        Ok(())
    })();

    let _ = std::fs::remove_file(file_path);
    res
}

#[test]
fn test_scope_isolation_lambda() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "
        ((lambda () (define internal-x 999)))
        internal-x
    ";
    // Should fail finding internal-x
    let res = code.tokenize().parse()?.evaluate_with_env(env);
    assert!(res.is_err()); // Unknown symbol
    Ok(())
}

#[test]
fn test_immediate_application() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "((lambda (x) (* x x)) 10)";
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res { Value::Number(n) => assert_eq!(n, 100.0), _ => panic!() }
    Ok(())
}

#[test]
fn test_redefinition() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "
        (define x 1)
        (define x 2)
        x
    ";
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res { Value::Number(n) => assert_eq!(n, 2.0), _ => panic!() }
    Ok(())
}

#[test]
fn test_y_combinator() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "
        (define Y (lambda (f) ((lambda (x) (f (lambda (v) ((x x) v)))) (lambda (x) (f (lambda (v) ((x x) v)))))))
        (define fact (Y (lambda (partial) (lambda (n) (if (= n 0) 1 (* n (partial (- n 1))))))))
        (fact 5)
    ";
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res { Value::Number(n) => assert_eq!(n, 120.0), _ => panic!() }
    Ok(())
}

#[test]
fn test_ackermann() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "
        (define ack (lambda (m n)
            (if (= m 0) (+ n 1)
                (if (= n 0) (ack (- m 1) 1)
                    (ack (- m 1) (ack m (- n 1)))))))
        (ack 3 2)
    "; // 29
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res { Value::Number(n) => assert_eq!(n, 29.0), _ => panic!() }
    Ok(())
}

#[test]
fn test_late_binding_closure() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "
        (define f (lambda () g-val))
        (define g-val 100)
        (f)
    ";
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res { Value::Number(n) => assert_eq!(n, 100.0), _ => panic!() }
    Ok(())
}

#[test]
fn test_shadowing_tower() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "
        ((lambda (x) 
            ((lambda (x) 
                ((lambda (x) x) 3)) 
            2)) 
            1)
    ";
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res { Value::Number(n) => assert_eq!(n, 3.0), _ => panic!() }
    Ok(())
}

#[test]
fn test_church_numerals() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "
        (define one (lambda (f) (lambda (x) (f x))))
        (define succ (lambda (n) (lambda (f) (lambda (x) (f ((n f) x))))))
        (define to-int (lambda (n) ((n (lambda (x) (+ x 1))) 0)))
        (to-int (succ one))
    "; // succ one = 2
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res { Value::Number(n) => assert_eq!(n, 2.0), _ => panic!() }
    Ok(())
}

#[test]
fn test_currying() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "
        (define curried-add (lambda (x) (lambda (y) (lambda (z) (+ x (+ y z))))))
        (((curried-add 10) 20) 30)
    ";
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res { Value::Number(n) => assert_eq!(n, 60.0), _ => panic!() }
    Ok(())
}

#[test]
fn test_lazy_stream() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "
        (define make-stream (lambda (head thunk) (list head thunk)))
        (define stream-car car)
        (define stream-cdr (lambda (s) ((car (cdr s)))))
        (define naturals (lambda (n) (make-stream n (lambda () (naturals (+ n 1))))))
        (define nats (naturals 0))
        (stream-car (stream-cdr (stream-cdr (stream-cdr nats))))
    "; // 3rd next: 0 -> 1 -> 2 -> 3
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res { Value::Number(n) => assert_eq!(n, 3.0), _ => panic!() }
    Ok(())
}

#[test]
fn test_long_variadic_arithmetic() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "(+ 1 2 3 4 5 6 7 8 9 10)"; // 55
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res { Value::Number(n) => assert_eq!(n, 55.0), _ => panic!() }
    Ok(())
}

#[test]
fn test_functional_quicksort() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "
        (define filter (lambda (pred l) 
            (if (empty? l) (list)
                (if (pred (first l)) 
                    (cons (first l) (filter pred (rest l)))
                    (filter pred (rest l))))))
        (define append (lambda (l1 l2)
            (if (empty? l1) l2 (cons (first l1) (append (rest l1) l2)))))
        (define qsort (lambda (l)
            (if (empty? l) (list)
                (append (qsort (filter (lambda (x) (< x (first l))) (rest l)))
                        (cons (first l)
                            (qsort (filter (lambda (x) (if (< x (first l)) 0 1)) (rest l))))))))
        (qsort (list 3 1 4 1 5 9 2 6))
    ";
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    // check list sorted
    match res {
        Value::List(l) => {
                let nums: Vec<f64> = l.iter().map(|v| match v { Value::Number(n) => *n, _ => 0.0 }).collect();
                assert_eq!(nums, vec![1.0, 1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 9.0]);
        }
        _ => panic!("Expected list")
    }
    Ok(())
}

#[test]
fn test_floating_point_infinity() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "(/ 1 0)";
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res { 
        Value::Number(n) => assert!(n.is_infinite()), 
        _ => panic!() 
    }
    Ok(())
}

#[test]
fn test_stack_depth_recursion() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "
        (define deep (lambda (n) (if (= n 0) 0 (+ 1 (deep (- n 1))))))
        (deep 50)
    ";
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res { Value::Number(n) => assert_eq!(n, 50.0), _ => panic!() }
    Ok(())
}

#[test]
fn test_cps_factorial() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "
        (define fact-cps (lambda (n k)
            (if (= n 0)
                (k 1)
                (fact-cps (- n 1) (lambda (v) (k (* n v)))))))
        (fact-cps 5 (lambda (x) x))
    ";
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res { Value::Number(n) => assert_eq!(n, 120.0), _ => panic!() }
    Ok(())
}

#[test]
fn test_env_chain_stress() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    // Use `if` for sequencing: (if (define ...) (lambda ...))
    let code = "
        (define a 1)
        (define f (lambda ()
            (if (define b 2)
            (lambda ()
                (if (define c 3)
                (lambda ()
                    (if (define d 4)
                    (+ a (+ b (+ c d)))
                    0))
                0))
            0)))
        (((f)))
    ";
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res { Value::Number(n) => assert_eq!(n, 10.0), _ => panic!() }
    Ok(())
}

#[test]
fn test_simulated_object() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "
        (define make-account (lambda (balance)
            (lambda (msg)
            (if (= msg \"get\") balance
                (if (= msg \"inc\") (make-account (+ balance 10))
                    balance)))))
        (define acc (make-account 100))
        (define acc2 (acc \"inc\"))
        (list (acc \"get\") (acc2 \"get\"))
    ";
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res { 
        Value::List(l) => {
            match l[0] { Value::Number(n) => assert_eq!(n, 100.0), _ => panic!() }
            match l[1] { Value::Number(n) => assert_eq!(n, 110.0), _ => panic!() }
        }
        _ => panic!() 
    }
    Ok(())
}

#[test]
fn test_prime_sieve_functional() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "
        ;; Implement simple mod for positive integers: a % b
        (define mod (lambda (a b) (if (< a b) a (mod (- a b) b))))
        (define divisible? (lambda (n k) (= (mod n k) 0)))
        
        (define filter (lambda (f l) 
                (if (empty? l) (list)
                    (if (f (first l)) 
                        (cons (first l) (filter f (rest l)))
                        (filter f (rest l))))))
        (define sieve (lambda (l)
            (if (empty? l) (list)
                (cons (first l) 
                    (sieve (filter (lambda (x) (if (divisible? x (first l)) 0 1)) (rest l)))))))
        (sieve (range 2 20))
    ";
    // Primes up to 20: 2 3 5 7 11 13 17 19
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res {
        Value::List(l) => assert_eq!(l.len(), 8),
        _ => panic!()
    }
    Ok(())
}

#[test]
fn test_boolean_circuit() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    // (xor 1 0) -> 1
    // xor a b = (or (and a (not b)) (and (not a) b))
    let code = "
        (define not (lambda (x) (if x 0 1)))
        (define and (lambda (a b) (if a (if b 1 0) 0)))
        (define or (lambda (a b) (if a 1 (if b 1 0))))
        (define xor (lambda (a b) (or (and a (not b)) (and (not a) b))))
        (list (xor 0 0) (xor 0 1) (xor 1 0) (xor 1 1))
    ";
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res {
        Value::List(l) => {
                match l[0] { Value::Number(n) => assert_eq!(n, 0.0), _ => panic!() } // 0 0 -> 0
                match l[1] { Value::Number(n) => assert_eq!(n, 1.0), _ => panic!() } // 0 1 -> 1
                match l[2] { Value::Number(n) => assert_eq!(n, 1.0), _ => panic!() } // 1 0 -> 1
                match l[3] { Value::Number(n) => assert_eq!(n, 0.0), _ => panic!() } // 1 1 -> 0
        }
        _ => panic!()
    }
    Ok(())
}

#[test]
fn test_map_reduce_pipeline() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    // Sum of squares of 1..10
    let code = "
        (fold + 0 (map (lambda (x) (* x x)) (range 1 11)))
    "; // 1^2 + ... + 10^2 = 385
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res { Value::Number(n) => assert_eq!(n, 385.0), _ => panic!() }
    Ok(())
}

#[test]
fn test_trampoline_mutual() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    // Since we don't have true TCO, this just tests function returning function
    let code = "
        (define trampoline (lambda (f) (if (function? f) (trampoline (f)) f)))
        ;; Wait, we don't have function? primitive exposed.
        ;; Let's try simple 'tag' return: (list \"done\" val) or (list \"call\" func)
        (define tramp (lambda (res)
            (if (= (first res) \"done\") (first (rest res))
                (tramp ((first (rest res)))))))
        
        (define even-t (lambda (n) 
            (if (= n 0) (list \"done\" 1) (list \"call\" (lambda () (odd-t (- n 1)))))))
        (define odd-t (lambda (n)
            (if (= n 0) (list \"done\" 0) (list \"call\" (lambda () (even-t (- n 1)))))))
            
        (tramp (even-t 10))
    ";
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res { Value::Number(n) => assert_eq!(n, 1.0), _ => panic!() }
    Ok(())
}

#[test]
fn test_variadic_only_lambda() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "((lambda args args) 1 2 3)";
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res {
        Value::List(l) => assert_eq!(l.len(), 3),
        _ => panic!()
    }
    Ok(())
}

#[test]
fn test_deep_list_equality() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "
        (define l1 (list 1 (list 2 3) 4))
        (define l2 (list 1 (list 2 3) 4))
        (= l1 l2)
    ";
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res { Value::Number(n) => assert_eq!(n, 1.0), _ => panic!() }
    Ok(())
}

#[test]
fn test_internal_mutual_recursion() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    // Use `if` sequences to simulate `begin` block since lambda only accepts one expr
    let code = "
        (define solve (lambda (n)
            (if (define part-a (lambda (x) (if (= x 0) 1 (part-b (- x 1)))))
                (if (define part-b (lambda (x) (if (= x 0) 0 (part-a (- x 1)))))
                    (part-a n)
                    0)
                0)
        ))
        (solve 4) 
    ";
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    match res { Value::Number(n) => assert_eq!(n, 1.0), _ => panic!() }
    Ok(())
}

#[test]
fn test_error_propagation_deep() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    let code = "
            (define f (lambda (x) (g x)))
            (define g (lambda (x) (h x)))
            (define h (lambda (x) (+ x (list)))) ;; Type error: Number + List
            (f 1)
    ";
    let res = code.tokenize().parse()?.evaluate_with_env(env);
    assert!(res.is_err());
    Ok(())
}

#[test]
fn test_deep_nested_list_literals() -> Result<(), Box<dyn std::error::Error>> {
    let env = setup_env()?;
    // (1 (2 (3 (4 5))))
    let code = "(list 1 (list 2 (list 3 (list 4 5))))";
    let res = code.tokenize().parse()?.evaluate_with_env(env)?;
    // Check depth
    if let Value::List(l1) = res {
            assert_eq!(l1.len(), 2);
            if let Value::List(l2) = &l1[1] {
                assert_eq!(l2.len(), 2);
                if let Value::List(l3) = &l2[1] {
                    assert_eq!(l3.len(), 2);
                    if let Value::List(l4) = &l3[1] {
                        assert_eq!(l4.len(), 2);
                    } else { panic!() }
                } else { panic!() }
            } else { panic!() }
    } else { panic!() }
    Ok(())
}
