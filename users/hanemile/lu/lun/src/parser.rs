use crate::lexer::Tokens;
use crate::error::LunError;

#[derive(Debug, Clone, PartialEq)]
pub enum AstNode {
    Atom(String),
    List(Vec<AstNode>),
}

#[derive(Debug)]
pub struct Ast {
    pub exprs: Vec<AstNode>,
}

pub trait Parsable {
    fn parse(self) -> Result<Ast, LunError>;
}

impl Parsable for Tokens {
    fn parse(self) -> Result<Ast, LunError> {
        let mut tokens = self.into_iter().peekable();
        let mut exprs = Vec::new();
        
        while tokens.peek().is_some() {
            exprs.push(parse_expr(&mut tokens)?);
        }
        
        Ok(Ast { exprs })
    }
}

fn parse_expr(tokens: &mut std::iter::Peekable<std::vec::IntoIter<String>>) -> Result<AstNode, LunError> {
    let token = match tokens.next() {
        Some(t) => t,
        None => return Err(LunError::Parse("Unexpected end of tokens".into())),
    };
    
    if token == "(" {
        let mut list = Vec::new();
        while let Some(t) = tokens.peek() {
            if t == ")" {
                tokens.next(); // consume )
                return Ok(AstNode::List(list));
            }
            list.push(parse_expr(tokens)?);
        }
        Err(LunError::Parse("Unbalanced parenthesis: missing )".into()))
    } else if token == ")" {
        Err(LunError::Parse("Unexpected right parenthesis".into()))
    } else {
        Ok(AstNode::Atom(token))
    }
}
