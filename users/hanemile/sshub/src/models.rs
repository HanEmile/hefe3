use chrono::{DateTime, Utc};
use uuid::Uuid;

fn uid() -> String { Uuid::new_v4().to_string() }

#[derive(Debug, Clone)]
pub struct User {
    pub id: String,
    pub name: String,
    pub online: bool,
}

impl User {
    pub fn new(id: &str, name: &str, online: bool) -> Self {
        Self { id: id.into(), name: name.into(), online }
    }
}

#[derive(Debug, Clone)]
pub struct Message {
    pub id: String,
    pub author: String,
    pub content: String,
    pub reply_to: Option<String>,
    pub timestamp: DateTime<Utc>,
}

impl Message {
    pub fn new(author: &str, content: &str, reply_to: Option<String>) -> Self {
        Self {
            id: uid(),
            author: author.into(),
            content: content.into(),
            reply_to,
            timestamp: Utc::now(),
        }
    }
}

#[derive(Debug, Clone)]
pub struct Room {
    pub id: String,
    pub name: String,
    pub topic: String,
    pub messages: Vec<Message>,
}

impl Room {
    pub fn new(id: String, name: String, topic: String) -> Self {
        Self { id, name, topic, messages: vec![] }
    }
}

#[derive(Debug, Clone)]
pub struct Comment {
    pub id: String,
    pub author: String,
    pub content: String,
    pub reply_to: Option<String>,
    pub timestamp: DateTime<Utc>,
}

impl Comment {
    pub fn new(author: &str, content: &str, reply_to: Option<String>) -> Self {
        Self {
            id: uid(),
            author: author.into(),
            content: content.into(),
            reply_to,
            timestamp: Utc::now(),
        }
    }
}

#[derive(Debug, Clone)]
pub struct Post {
    pub id: String,
    pub title: String,
    pub author: String,
    pub content: String,
    pub timestamp: DateTime<Utc>,
    pub comments: Vec<Comment>,
}

impl Post {
    pub fn new(author: &str, title: &str, content: &str) -> Self {
        Self {
            id: uid(),
            title: title.into(),
            author: author.into(),
            content: content.into(),
            timestamp: Utc::now(),
            comments: vec![],
        }
    }
}

#[derive(Debug, Clone)]
pub struct Subforum {
    pub id: String,
    pub name: String,
    pub posts: Vec<Post>,
}

impl Subforum {
    pub fn new(id: String, name: String) -> Self {
        Self { id, name, posts: vec![] }
    }
}
