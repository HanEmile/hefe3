use crate::broadcast;
use crate::db::Db;
use crate::models::*;
use crate::theme::Theme;
use anyhow::Result;

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum Workspace { Chat, Forum }

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum ChatFocus { Rooms, Messages, Users }

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum ForumFocus { Subforums, Posts, Thread }

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum InputMode { Normal, Composing }

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum ComposeStage {
    None,
    PostTitle,
    PostBody,
    Comment,
    RoomName,
    RoomTopic,
    SubforumName,
}

#[derive(Debug, Clone, PartialEq)]
pub enum Popup {
    None,
    ReplyChain(Vec<String>),
    Settings,
    UserProfile(String),
    ConfirmDelete,
}

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum SettingsField { Username, UsersKey, Theme, DeleteAccount }

impl SettingsField {
    pub fn next(self) -> Self {
        match self {
            Self::Username => Self::UsersKey,
            Self::UsersKey => Self::Theme,
            Self::Theme => Self::DeleteAccount,
            Self::DeleteAccount => Self::DeleteAccount,
        }
    }
    pub fn prev(self) -> Self {
        match self {
            Self::Username => Self::Username,
            Self::UsersKey => Self::Username,
            Self::Theme => Self::UsersKey,
            Self::DeleteAccount => Self::Theme,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum SearchKind { Rooms, Subforums, Users, Messages, Posts }

#[derive(Debug)]
pub struct App {
    pub user_id: Option<String>,
    pub username: String,
    pub theme: Theme,

    pub workspace: Workspace,
    pub entered: bool,

    // chat
    pub rooms: Vec<Room>,
    pub selected_room: usize,
    pub chat_focus: ChatFocus,
    pub chat_scroll: usize,
    pub selected_message: Option<usize>,
    pub reply_to_id: Option<String>,
    /// last seen message id per room
    pub last_read: std::collections::HashMap<String, String>,

    // forum
    pub subforums: Vec<Subforum>,
    pub selected_subforum: usize,
    pub selected_post: usize,
    pub forum_focus: ForumFocus,
    pub thread_scroll: usize,
    pub selected_comment: Option<usize>,
    pub reply_to_comment_id: Option<String>,
    pub collapsed_comments: Vec<String>,
    pub subforum_filter: String,

    // users
    pub users: Vec<User>,
    pub selected_user: usize,

    // input
    pub input_mode: InputMode,
    pub input_buffer: String,
    pub cursor_pos: usize,
    pub compose_title: String,
    pub compose_stage: ComposeStage,

    // popups
    pub popup: Popup,
    pub popup_scroll: usize,

    // settings popup
    pub settings_field: SettingsField,
    pub settings_editing: bool,
    pub users_view_key: char,

    // search popup
    pub search_active: bool,
    pub search_query: String,
    pub search_kind: SearchKind,
    pub search_selected: usize,

    pub broadcast_tx: Option<broadcast::Sender>,
    pub exit: bool,
}

impl App {
    pub fn from_db(db: &Db, user_id: Option<&str>, username: &str) -> Result<Self> {
        let mut app = Self {
            user_id: user_id.map(String::from),
            username: username.into(),
            theme: Theme::default(),
            workspace: Workspace::Chat,
            entered: false,

            rooms: db.load_rooms()?,
            selected_room: 0,
            chat_focus: ChatFocus::Messages,
            chat_scroll: 0,
            selected_message: None,
            reply_to_id: None,
            last_read: std::collections::HashMap::new(),

            subforums: db.load_subforums()?,
            selected_subforum: 0,
            selected_post: 0,
            forum_focus: ForumFocus::Posts,
            thread_scroll: 0,
            selected_comment: None,
            reply_to_comment_id: None,
            collapsed_comments: vec![],
            subforum_filter: String::new(),

            users: vec![], // filled below after we know who I am
            selected_user: 0,

            input_mode: InputMode::Normal,
            input_buffer: String::new(),
            cursor_pos: 0,
            compose_title: String::new(),
            compose_stage: ComposeStage::None,

            popup: Popup::None,
            popup_scroll: 0,

            settings_field: SettingsField::Username,
            settings_editing: false,
            users_view_key: 'u',

            search_active: false,
            search_query: String::new(),
            search_kind: SearchKind::Rooms,
            search_selected: 0,

            broadcast_tx: None,
            exit: false,
        };
        app.load_user_settings(db);
        app.refresh_users(db)?;
        app.chat_scroll = app.current_room().map(|r| r.messages.len().saturating_sub(1)).unwrap_or(0);
        app.mark_current_room_read(db);
        Ok(app)
    }

    /// Reload users from DB and mark online based on the presence table
    /// (heartbeats within the last 30 seconds).
    pub fn refresh_users(&mut self, db: &Db) -> anyhow::Result<()> {
        let mut users = db.list_users()?;
        let online = db.online_user_ids(30).unwrap_or_default();
        for u in users.iter_mut() {
            if online.contains(&u.id) { u.online = true; }
        }
        // Always show ourselves as online.
        if let Some(me) = self.user_id.as_deref() {
            if let Some(u) = users.iter_mut().find(|u| u.id == me) {
                u.online = true;
            }
        }
        self.users = users;
        Ok(())
    }

    // ── lookups ──────────────────────────────────────────────────────────

    pub fn current_room(&self) -> Option<&Room> { self.rooms.get(self.selected_room) }
    pub fn current_subforum(&self) -> Option<&Subforum> { self.subforums.get(self.selected_subforum) }
    pub fn current_post(&self) -> Option<&Post> {
        self.current_subforum()?.posts.get(self.selected_post)
    }

    // ── input editing ────────────────────────────────────────────────────

    pub fn insert_char(&mut self, c: char) {
        self.input_buffer.insert(self.cursor_pos, c);
        self.cursor_pos += 1;
    }

    pub fn backspace(&mut self) {
        if self.cursor_pos > 0 {
            self.cursor_pos -= 1;
            self.input_buffer.remove(self.cursor_pos);
        }
    }

    pub fn reset_input(&mut self) {
        self.input_buffer.clear();
        self.cursor_pos = 0;
        self.compose_title.clear();
        self.compose_stage = ComposeStage::None;
        self.input_mode = InputMode::Normal;
    }

    pub fn start_compose(&mut self, stage: ComposeStage) {
        self.compose_stage = stage;
        self.input_mode = InputMode::Composing;
        self.input_buffer.clear();
        self.compose_title.clear();
        self.cursor_pos = 0;
    }

    // ── mutations ────────────────────────────────────────────────────────

    pub fn send_message(&mut self, db: &Db) -> Result<()> {
        let content = self.input_buffer.trim().to_string();
        if content.is_empty() { return Ok(()); }
        let Some(room) = self.current_room() else { return Ok(()); };
        let room_id = room.id.clone();
        let msg = Message::new(&self.username, &content, self.reply_to_id.clone());
        db.insert_message(&room_id, &msg)?;
        self.notify(broadcast::Event::Message { room_id: room_id.clone() });
        self.rooms[self.selected_room].messages.push(msg);
        self.reply_to_id = None;
        let len = self.rooms[self.selected_room].messages.len();
        self.chat_scroll = len.saturating_sub(1);
        self.selected_message = None;
        self.reset_input();
        self.mark_current_room_read(db);
        Ok(())
    }

    pub fn submit_post(&mut self, db: &Db) -> Result<()> {
        let title = self.compose_title.trim().to_string();
        let content = self.input_buffer.trim().to_string();
        if title.is_empty() || content.is_empty() { return Ok(()); }
        let Some(sf) = self.current_subforum() else { return Ok(()); };
        let sf_id = sf.id.clone();
        let post = Post::new(&self.username, &title, &content);
        db.insert_post(&sf_id, &post)?;
        self.notify(broadcast::Event::Forum);
        self.subforums[self.selected_subforum].posts.push(post);
        self.selected_post = self.subforums[self.selected_subforum].posts.len() - 1;
        self.forum_focus = ForumFocus::Thread;
        self.reset_input();
        Ok(())
    }

    pub fn submit_comment(&mut self, db: &Db) -> Result<()> {
        let content = self.input_buffer.trim().to_string();
        if content.is_empty() { return Ok(()); }
        let Some(post) = self.current_post() else { return Ok(()); };
        let comment = Comment::new(&self.username, &content, self.reply_to_comment_id.clone());
        db.insert_comment(&post.id.clone(), &comment)?;
        self.notify(broadcast::Event::Forum);
        self.subforums[self.selected_subforum].posts[self.selected_post]
            .comments.push(comment);
        self.reply_to_comment_id = None;
        self.reset_input();
        Ok(())
    }

    pub fn submit_room(&mut self, db: &Db) -> Result<()> {
        let name = self.compose_title.trim();
        if name.is_empty() { return Ok(()); }
        let room = db.insert_room(name, self.input_buffer.trim())?;
        self.notify(broadcast::Event::Room);
        self.rooms.push(room);
        self.selected_room = self.rooms.len() - 1;
        self.reset_input();
        Ok(())
    }

    pub fn submit_subforum(&mut self, db: &Db) -> Result<()> {
        let name = self.compose_title.trim();
        if name.is_empty() { return Ok(()); }
        let sf = db.insert_subforum(name)?;
        self.notify(broadcast::Event::Forum);
        self.subforums.push(sf);
        self.selected_subforum = self.subforums.len() - 1;
        self.selected_post = 0;
        self.reset_input();
        Ok(())
    }

    fn notify(&self, event: broadcast::Event) {
        if let Some(tx) = &self.broadcast_tx {
            // A failed send means there are no subscribers — fine, swallow.
            let _ = tx.send(event);
        }
    }

    pub fn apply_broadcast(&mut self, event: &broadcast::Event, db: &Db) -> Result<()> {
        match event {
            broadcast::Event::Message { room_id } => {
                if let Some(room) = self.rooms.iter_mut().find(|r| &r.id == room_id) {
                    room.messages = db.load_messages(room_id)?;
                }
                if self.current_room().map(|r| &r.id) == Some(room_id) {
                    self.mark_current_room_read(db);
                }
            }
            broadcast::Event::Forum => self.subforums = db.load_subforums()?,
            broadcast::Event::Room => self.rooms = db.load_rooms()?,
            broadcast::Event::Presence => self.refresh_users(db)?,
        }
        Ok(())
    }

    // ── chat selection ───────────────────────────────────────────────────

    pub fn select_message(&mut self, delta: i32) {
        let len = self.rooms[self.selected_room].messages.len();
        if len == 0 { return; }
        let new = match self.selected_message {
            None if delta < 0 => len - 1,
            None => 0,
            Some(i) => ((i as i32 + delta).clamp(0, len as i32 - 1)) as usize,
        };
        self.selected_message = Some(new);
        self.chat_scroll = new;
    }

    pub fn build_reply_chain(&self) -> Vec<String> {
        let Some(room) = self.current_room() else { return vec![]; };
        let Some(start) = self.selected_message else { return vec![]; };
        let Some(msg) = room.messages.get(start) else { return vec![]; };

        let mut chain = vec![msg.id.clone()];
        let mut current_reply = msg.reply_to.clone();
        while let Some(id) = current_reply {
            if let Some(m) = room.messages.iter().find(|m| m.id == id) {
                chain.push(m.id.clone());
                current_reply = m.reply_to.clone();
            } else {
                break;
            }
        }
        chain.reverse();
        chain
    }

    // ── forum helpers ────────────────────────────────────────────────────

    pub fn filtered_subforum_indices(&self) -> Vec<usize> {
        if self.subforum_filter.is_empty() {
            (0..self.subforums.len()).collect()
        } else {
            let f = self.subforum_filter.to_lowercase();
            self.subforums.iter().enumerate()
                .filter(|(_, s)| s.name.to_lowercase().contains(&f))
                .map(|(i, _)| i)
                .collect()
        }
    }

    pub fn toggle_comment_collapsed(&mut self, id: &str) {
        if let Some(pos) = self.collapsed_comments.iter().position(|c| c == id) {
            self.collapsed_comments.remove(pos);
        } else {
            self.collapsed_comments.push(id.into());
        }
    }

    pub fn is_comment_collapsed(&self, id: &str) -> bool {
        self.collapsed_comments.iter().any(|c| c == id)
    }

    pub fn count_descendants(comments: &[Comment], id: &str) -> (usize, usize) {
        let direct = comments.iter().filter(|c| c.reply_to.as_deref() == Some(id)).count();
        let mut total = 0;
        for c in comments {
            if c.reply_to.as_deref() == Some(id) {
                total += 1 + Self::count_descendants(comments, &c.id).1;
            }
        }
        (direct, total)
    }

    // ── settings ─────────────────────────────────────────────────────────

    pub fn load_user_settings(&mut self, db: &Db) {
        let Some(uid) = &self.user_id else { return; };
        if let Ok(Some(v)) = db.get_setting(uid, "users_view_key") {
            if let Some(c) = v.chars().next() {
                self.users_view_key = c;
            }
        }
        if let Ok(Some(v)) = db.get_setting(uid, "theme") {
            self.theme = Theme::named(&v);
        }
        for room in &self.rooms {
            if let Ok(Some(v)) = db.get_setting(uid, &format!("read:{}", room.id)) {
                self.last_read.insert(room.id.clone(), v);
            }
        }
    }

    pub fn unread_count(&self, room: &Room) -> usize {
        let Some(last_id) = self.last_read.get(&room.id) else {
            return room.messages.len();
        };
        match room.messages.iter().position(|m| m.id == *last_id) {
            Some(idx) => room.messages.len().saturating_sub(idx + 1),
            None => room.messages.len(),
        }
    }

    /// Persist the last-seen message id for the currently selected room.
    pub fn mark_current_room_read(&mut self, db: &Db) {
        let Some(room) = self.rooms.get(self.selected_room) else { return; };
        let Some(last) = room.messages.last() else { return; };
        let room_id = room.id.clone();
        let msg_id = last.id.clone();
        self.last_read.insert(room_id.clone(), msg_id.clone());
        let _ = self.save_setting(db, &format!("read:{}", room_id), &msg_id);
    }

    pub fn save_setting(&self, db: &Db, key: &str, value: &str) -> Result<()> {
        if let Some(uid) = &self.user_id {
            db.set_setting(uid, key, value)?;
        }
        Ok(())
    }

    // ── search ───────────────────────────────────────────────────────────

    pub fn fuzzy_match(needle: &str, haystack: &str) -> bool {
        if needle.is_empty() { return true; }
        let needle = needle.to_lowercase();
        let haystack = haystack.to_lowercase();
        let mut hs = haystack.chars();
        for nc in needle.chars() {
            if !hs.any(|hc| hc == nc) { return false; }
        }
        true
    }
}
