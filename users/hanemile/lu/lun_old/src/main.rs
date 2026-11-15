use std::path::Path;
use std::fs;

#[derive(Default)]
struct Lun {
    source: String,
}

impl Lun {
    fn readfile<P: AsRef<Path>>(&mut self, path: P) {
        self.source = fs::read_to_string(path).unwrap();
    }

    fn send(&self, val: String) {
        println!("Evaluating {val}")
    }

    fn lex(&self) {
        println!("lexing");
    }
    fn parse(&self) {
        println!("parsing");
    }
}

fn main() {
    println!("Welcome to lun!");

    let mut lun: Lun = Default::default();

    lun.readfile("./in/a.lun");

    // (+ 1 2)
    // -> ["(", "+", "1", "2", ")"] 
    lun.lex();

    // ["(", "+", "1", "2", ")"] 
    // -> [LEFTPAREN, PLUS, 1, 2, RIGHTPAREN]
    lun.parse();

    lun.send("(+ 1 2)".to_owned());
}
