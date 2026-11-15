//! SQLite persistence. One file per server, path via SSHUB_DB env
//! (default: ~/.local/share/sshub/sshub.db).

use anyhow::{Context, Result};
use chrono::{TimeZone, Utc};
use rusqlite::{params, Connection, Row};
use std::path::PathBuf;
use uuid::Uuid;

use crate::models::*;

pub struct Db(Connection);

const SCHEMA: &str = r#"
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    pubkey TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS rooms (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    topic TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS messages (
    id TEXT PRIMARY KEY,
    room_id TEXT NOT NULL REFERENCES rooms(id),
    author TEXT NOT NULL,
    content TEXT NOT NULL,
    reply_to TEXT,
    ts INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS subforums (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS posts (
    id TEXT PRIMARY KEY,
    subforum_id TEXT NOT NULL REFERENCES subforums(id),
    author TEXT NOT NULL,
    title TEXT NOT NULL,
    content TEXT NOT NULL,
    ts INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS comments (
    id TEXT PRIMARY KEY,
    post_id TEXT NOT NULL REFERENCES posts(id),
    author TEXT NOT NULL,
    content TEXT NOT NULL,
    reply_to TEXT,
    ts INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS settings (
    user_id TEXT NOT NULL,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    PRIMARY KEY (user_id, key)
);
CREATE TABLE IF NOT EXISTS presence (
    user_id TEXT PRIMARY KEY,
    last_seen INTEGER NOT NULL  -- unix millis UTC
);
"#;

const SEED_ROOMS: &[(&str, &str)] = &[
    ("general", "General chat"),
    ("tech", "Technology"),
    ("random", "Anything goes"),
];

const SEED_SUBFORUMS: &[&str] = &["meta", "tech", "off-topic"];

impl Db {
    pub fn open() -> Result<Self> {
        let path = db_path();
        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent)
                .with_context(|| format!("creating {}", parent.display()))?;
        }
        let conn = Connection::open(&path)
            .with_context(|| format!("opening DB at {}", path.display()))?;
        conn.execute_batch("PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;")?;
        let db = Self(conn);
        db.migrate()?;
        Ok(db)
    }

    fn migrate(&self) -> Result<()> {
        self.0.execute_batch(SCHEMA)?;
        if self.count("rooms")? == 0 {
            for (name, topic) in SEED_ROOMS {
                self.insert_room(name, topic)?;
            }
        }
        if self.count("subforums")? == 0 {
            for name in SEED_SUBFORUMS {
                self.insert_subforum(name)?;
            }
        }
        Ok(())
    }

    fn count(&self, table: &str) -> Result<i64> {
        Ok(self.0.query_row(&format!("SELECT COUNT(*) FROM {}", table), [], |r| r.get(0))?)
    }

    // ── users ────────────────────────────────────────────────────────────

    pub fn find_user_by_pubkey(&self, pubkey: &str) -> Result<Option<User>> {
        self.0
            .query_row(
                "SELECT id, name FROM users WHERE pubkey = ?1",
                params![pubkey],
                |r| Ok(User::new(&r.get::<_, String>(0)?, &r.get::<_, String>(1)?, true)),
            )
            .optional()
            .map_err(Into::into)
    }

    pub fn create_user(&self, name: &str, pubkey: &str) -> Result<User> {
        let id = Uuid::new_v4().to_string();
        self.0.execute(
            "INSERT INTO users (id, name, pubkey) VALUES (?1, ?2, ?3)",
            params![id, name, pubkey],
        )?;
        Ok(User::new(&id, name, true))
    }

    pub fn rename_user(&self, user_id: &str, new_name: &str) -> Result<()> {
        self.0.execute(
            "UPDATE users SET name = ?1 WHERE id = ?2",
            params![new_name, user_id],
        )?;
        Ok(())
    }

    pub fn delete_user(&self, user_id: &str) -> Result<()> {
        self.0
            .execute("DELETE FROM users WHERE id = ?1", params![user_id])?;
        Ok(())
    }

    pub fn unique_guest_name(&self) -> Result<String> {
        // Pick the lowest "guestN" not yet taken.
        for n in 1.. {
            let candidate = format!("guest{}", n);
            let taken: bool = self.0.query_row(
                "SELECT EXISTS(SELECT 1 FROM users WHERE name = ?1)",
                params![candidate],
                |r| r.get(0),
            )?;
            if !taken { return Ok(candidate); }
        }
        unreachable!()
    }

    pub fn list_users(&self) -> Result<Vec<User>> {
        let mut stmt = self.0.prepare("SELECT id, name FROM users ORDER BY name")?;
        let rows = stmt.query_map([], |r| {
            Ok(User::new(&r.get::<_, String>(0)?, &r.get::<_, String>(1)?, false))
        })?;
        Ok(rows.collect::<rusqlite::Result<_>>()?)
    }

    // ── rooms & messages ─────────────────────────────────────────────────

    pub fn insert_room(&self, name: &str, topic: &str) -> Result<Room> {
        let id = Uuid::new_v4().to_string();
        self.0.execute(
            "INSERT INTO rooms (id, name, topic) VALUES (?1, ?2, ?3)",
            params![id, name, topic],
        )?;
        Ok(Room::new(id, name.into(), topic.into()))
    }

    pub fn load_rooms(&self) -> Result<Vec<Room>> {
        let mut stmt = self
            .0
            .prepare("SELECT id, name, topic FROM rooms ORDER BY name")?;
        let rooms: Vec<Room> = stmt
            .query_map([], |r| {
                Ok(Room::new(
                    r.get::<_, String>(0)?,
                    r.get::<_, String>(1)?,
                    r.get::<_, String>(2)?,
                ))
            })?
            .collect::<rusqlite::Result<_>>()?;
        let mut out = Vec::with_capacity(rooms.len());
        for mut room in rooms {
            room.messages = self.load_messages(&room.id)?;
            out.push(room);
        }
        Ok(out)
    }

    pub fn load_messages(&self, room_id: &str) -> Result<Vec<Message>> {
        self.query_all(
            "SELECT id, author, content, reply_to, ts FROM messages \
             WHERE room_id = ?1 ORDER BY ts ASC",
            params![room_id],
            row_to_message,
        )
    }

    pub fn insert_message(&self, room_id: &str, msg: &Message) -> Result<()> {
        self.0.execute(
            "INSERT INTO messages (id, room_id, author, content, reply_to, ts) \
             VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
            params![
                msg.id,
                room_id,
                msg.author,
                msg.content,
                msg.reply_to,
                msg.timestamp.timestamp_millis()
            ],
        )?;
        Ok(())
    }

    // ── forum ────────────────────────────────────────────────────────────

    pub fn insert_subforum(&self, name: &str) -> Result<Subforum> {
        let id = Uuid::new_v4().to_string();
        self.0.execute(
            "INSERT INTO subforums (id, name) VALUES (?1, ?2)",
            params![id, name],
        )?;
        Ok(Subforum::new(id, name.into()))
    }

    pub fn load_subforums(&self) -> Result<Vec<Subforum>> {
        let mut stmt = self
            .0
            .prepare("SELECT id, name FROM subforums ORDER BY name")?;
        let sfs: Vec<Subforum> = stmt
            .query_map([], |r| {
                Ok(Subforum::new(r.get::<_, String>(0)?, r.get::<_, String>(1)?))
            })?
            .collect::<rusqlite::Result<_>>()?;
        let mut out = Vec::with_capacity(sfs.len());
        for mut sf in sfs {
            sf.posts = self.load_posts(&sf.id)?;
            out.push(sf);
        }
        Ok(out)
    }

    fn load_posts(&self, subforum_id: &str) -> Result<Vec<Post>> {
        let mut stmt = self.0.prepare(
            "SELECT id, author, title, content, ts FROM posts \
             WHERE subforum_id = ?1 ORDER BY ts ASC",
        )?;
        let rows: Vec<(String, String, String, String, i64)> = stmt
            .query_map(params![subforum_id], |r| {
                Ok((r.get(0)?, r.get(1)?, r.get(2)?, r.get(3)?, r.get(4)?))
            })?
            .collect::<rusqlite::Result<_>>()?;
        let mut out = Vec::with_capacity(rows.len());
        for (id, author, title, content, ts) in rows {
            let mut p = Post::new(&author, &title, &content);
            p.id = id.clone();
            p.timestamp = Utc.timestamp_millis_opt(ts).single().unwrap_or_else(Utc::now);
            p.comments = self.load_comments(&id)?;
            out.push(p);
        }
        Ok(out)
    }

    pub fn insert_post(&self, subforum_id: &str, post: &Post) -> Result<()> {
        self.0.execute(
            "INSERT INTO posts (id, subforum_id, author, title, content, ts) \
             VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
            params![
                post.id,
                subforum_id,
                post.author,
                post.title,
                post.content,
                post.timestamp.timestamp_millis()
            ],
        )?;
        Ok(())
    }

    fn load_comments(&self, post_id: &str) -> Result<Vec<Comment>> {
        self.query_all(
            "SELECT id, author, content, reply_to, ts FROM comments \
             WHERE post_id = ?1 ORDER BY ts ASC",
            params![post_id],
            row_to_comment,
        )
    }

    fn query_all<T, F>(
        &self,
        sql: &str,
        params: impl rusqlite::Params,
        map: F,
    ) -> Result<Vec<T>>
    where
        F: FnMut(&Row) -> rusqlite::Result<T>,
    {
        let mut stmt = self.0.prepare(sql)?;
        let rows: rusqlite::Result<Vec<T>> = stmt.query_map(params, map)?.collect();
        Ok(rows?)
    }

    pub fn insert_comment(&self, post_id: &str, c: &Comment) -> Result<()> {
        self.0.execute(
            "INSERT INTO comments (id, post_id, author, content, reply_to, ts) \
             VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
            params![
                c.id,
                post_id,
                c.author,
                c.content,
                c.reply_to,
                c.timestamp.timestamp_millis()
            ],
        )?;
        Ok(())
    }

    // ── settings ─────────────────────────────────────────────────────────

    pub fn get_setting(&self, user_id: &str, key: &str) -> Result<Option<String>> {
        self.0
            .query_row(
                "SELECT value FROM settings WHERE user_id = ?1 AND key = ?2",
                params![user_id, key],
                |r| r.get::<_, String>(0),
            )
            .optional()
            .map_err(Into::into)
    }

    // ── presence ─────────────────────────────────────────────────────────

    pub fn presence_heartbeat(&self, user_id: &str) -> Result<()> {
        self.0.execute(
            "INSERT INTO presence (user_id, last_seen) VALUES (?1, ?2) \
             ON CONFLICT(user_id) DO UPDATE SET last_seen = excluded.last_seen",
            params![user_id, chrono::Utc::now().timestamp_millis()],
        )?;
        Ok(())
    }

    pub fn presence_clear(&self, user_id: &str) -> Result<()> {
        self.0.execute("DELETE FROM presence WHERE user_id = ?1", params![user_id])?;
        Ok(())
    }

    /// User IDs whose last heartbeat is within `window_secs`.
    pub fn online_user_ids(&self, window_secs: i64) -> Result<std::collections::HashSet<String>> {
        let threshold = chrono::Utc::now().timestamp_millis() - window_secs * 1000;
        let mut stmt = self.0.prepare("SELECT user_id FROM presence WHERE last_seen >= ?1")?;
        let rows: rusqlite::Result<Vec<String>> = stmt
            .query_map(params![threshold], |r| r.get::<_, String>(0))?
            .collect();
        Ok(rows?.into_iter().collect())
    }

    pub fn set_setting(&self, user_id: &str, key: &str, value: &str) -> Result<()> {
        self.0.execute(
            "INSERT INTO settings (user_id, key, value) VALUES (?1, ?2, ?3) \
             ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value",
            params![user_id, key, value],
        )?;
        Ok(())
    }
}

// ── row mappers ──────────────────────────────────────────────────────────
//
// Messages and Comments share the same column layout
// `(id, author, content, reply_to, ts)`; this macro generates the mapper for
// either struct.

macro_rules! make_authored_row_mapper {
    ($name:ident, $ty:ty) => {
        fn $name(r: &Row) -> rusqlite::Result<$ty> {
            let mut item = <$ty>::new(
                &r.get::<_, String>(1)?,
                &r.get::<_, String>(2)?,
                r.get::<_, Option<String>>(3)?,
            );
            item.id = r.get(0)?;
            item.timestamp = Utc
                .timestamp_millis_opt(r.get::<_, i64>(4)?)
                .single()
                .unwrap_or_else(Utc::now);
            Ok(item)
        }
    };
}

make_authored_row_mapper!(row_to_message, Message);
make_authored_row_mapper!(row_to_comment, Comment);

fn db_path() -> PathBuf {
    std::env::var("SSHUB_DB").map(PathBuf::from).unwrap_or_else(|_| {
        dirs::data_local_dir()
            .unwrap_or_else(|| PathBuf::from("."))
            .join("sshub")
            .join("sshub.db")
    })
}

trait OptionalExt<T> {
    fn optional(self) -> rusqlite::Result<Option<T>>;
}

impl<T> OptionalExt<T> for rusqlite::Result<T> {
    fn optional(self) -> rusqlite::Result<Option<T>> {
        match self {
            Ok(v) => Ok(Some(v)),
            Err(rusqlite::Error::QueryReturnedNoRows) => Ok(None),
            Err(e) => Err(e),
        }
    }
}
