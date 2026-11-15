use std::collections::HashSet;
use std::sync::{Arc, Mutex};
use tokio::sync::broadcast;

pub type Presence = Arc<Mutex<HashSet<String>>>;

pub fn new_presence() -> Presence {
    Arc::new(Mutex::new(HashSet::new()))
}

#[derive(Debug, Clone)]
pub enum Event {
    Message { room_id: String },
    Forum,
    Room,
    Presence,
}

pub type Sender = broadcast::Sender<Event>;
pub type Receiver = broadcast::Receiver<Event>;

pub fn channel() -> (Sender, Receiver) {
    broadcast::channel(256)
}
