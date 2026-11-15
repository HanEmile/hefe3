use anyhow::Result;
use crossterm::event::{KeyCode, KeyEvent, KeyModifiers};
use ratatui::backend::Backend;
use ratatui::buffer::Cell;
use ratatui::layout::{Position, Size};
use russh::server::Handle;
use russh::ChannelId;
use std::io::{self, Write};
use std::sync::{Arc, Mutex};
use tokio::sync::mpsc;

use crate::app::App;
use crate::broadcast;
use crate::db::Db;
use crate::ssh::SessionEvent;
use crate::ui;

enum Redraw {
    Input(Vec<u8>),
    Broadcast(broadcast::Event),
    Resize(u16, u16),
}

pub async fn run_tui(
    handle: Handle,
    channel: ChannelId,
    db: Arc<Mutex<Db>>,
    user_id: Option<String>,
    username: String,
    _pubkey: String,
    mut rx: mpsc::UnboundedReceiver<SessionEvent>,
    broadcast_tx: broadcast::Sender,
    pty_size: (u16, u16),
    _presence: broadcast::Presence,
) -> Result<()> {
    let mut broadcast_rx = broadcast_tx.subscribe();
    let (redraw_tx, mut redraw_rx) = mpsc::unbounded_channel::<Redraw>();

    // Heartbeat: every 10s write a presence row and tell the world.
    if let Some(uid) = user_id.clone() {
        let db_hb = Arc::clone(&db);
        let tx_hb = broadcast_tx.clone();
        tokio::spawn(async move {
            let mut tick = tokio::time::interval(std::time::Duration::from_secs(10));
            loop {
                tick.tick().await;
                let res = {
                    let lock = db_hb.lock();
                    match lock {
                        Ok(g) => g.presence_heartbeat(&uid),
                        Err(_) => break,
                    }
                };
                if res.is_err() { break; }
                let _ = tx_hb.send(broadcast::Event::Presence);
            }
        });
    }

    // SSH input -> redraw channel.
    let redraw_input = redraw_tx.clone();
    tokio::spawn(async move {
        while let Some(event) = rx.recv().await {
            let msg = match event {
                SessionEvent::Input(d) => Redraw::Input(d),
                SessionEvent::Resize(w, h) => Redraw::Resize(w, h),
            };
            if redraw_input.send(msg).is_err() { break; }
        }
    });

    // Broadcast -> redraw channel.
    tokio::spawn(async move {
        loop {
            match broadcast_rx.recv().await {
                Ok(e) => {
                    if redraw_tx.send(Redraw::Broadcast(e)).is_err() { break; }
                }
                Err(tokio::sync::broadcast::error::RecvError::Lagged(_)) => continue,
                Err(_) => break,
            }
        }
    });

    // Run the TUI on a blocking thread.
    let handle_for_tui = handle.clone();
    let broadcast_for_cleanup = broadcast_tx.clone();
    let db_for_cleanup = Arc::clone(&db);
    let user_id_for_cleanup = user_id.clone();
    let result = tokio::task::spawn_blocking(move || -> Result<()> {
        let mut app = {
            let db_lock = db.lock().map_err(|_| anyhow::anyhow!("db mutex poisoned"))?;
            let mut app = App::from_db(&db_lock, user_id.as_deref(), &username)?;
            app.broadcast_tx = Some(broadcast_tx);
            app
        };

        let writer = SshWriter::new(handle_for_tui, channel);
        let backend = SshBackend {
            inner: ratatui::backend::CrosstermBackend::new(writer),
            width: pty_size.0,
            height: pty_size.1,
        };
        let mut terminal = ratatui::Terminal::new(backend)?;

        // Enter alternate screen + clear.
        terminal.backend_mut().write_escape(b"\x1b[?1049h\x1b[2J\x1b[1;1H");
        terminal.draw(|f| ui::render(&app, f))?;

        while !app.exit {
            let Some(msg) = redraw_rx.blocking_recv() else { break; };
            match msg {
                Redraw::Input(data) => {
                    for key in parse_input(&data) {
                        let db_lock = db.lock().map_err(|_| anyhow::anyhow!("db mutex poisoned"))?;
                        if let Err(e) = crate::events::handle_key(&mut app, key, &db_lock) {
                            eprintln!("key handler error: {e:#}");
                        }
                    }
                }
                Redraw::Broadcast(event) => {
                    let db_lock = db.lock().map_err(|_| anyhow::anyhow!("db mutex poisoned"))?;
                    if let Err(e) = app.apply_broadcast(&event, &db_lock) {
                        eprintln!("broadcast apply error: {e:#}");
                    }
                }
                Redraw::Resize(w, h) => {
                    terminal.backend_mut().width = w;
                    terminal.backend_mut().height = h;
                    terminal.backend_mut().write_escape(b"\x1b[2J");
                }
            }
            terminal.draw(|f| ui::render(&app, f))?;
        }

        // Restore terminal.
        terminal.backend_mut().write_escape(b"\x1b[?1049l\x1b[?25h");
        Ok(())
    }).await?;

    // Clear presence and notify other sessions.
    if let Some(uid) = &user_id_for_cleanup {
        if let Ok(guard) = db_for_cleanup.lock() {
            let _ = guard.presence_clear(uid);
        }
        let _ = broadcast_for_cleanup.send(broadcast::Event::Presence);
    }

    // Close the SSH channel cleanly when the TUI exits.
    let _ = handle.eof(channel).await;
    let _ = handle.close(channel).await;

    result
}

// ── SSH-backed ratatui backend ───────────────────────────────────────────

struct SshBackend {
    inner: ratatui::backend::CrosstermBackend<SshWriter>,
    width: u16,
    height: u16,
}

impl SshBackend {
    fn write_escape(&mut self, seq: &[u8]) {
        let _ = io::Write::write_all(&mut self.inner, seq);
        let _ = io::Write::flush(&mut self.inner);
    }
}

impl Backend for SshBackend {
    fn draw<'a, I>(&mut self, content: I) -> io::Result<()>
    where I: Iterator<Item = (u16, u16, &'a Cell)>,
    {
        self.inner.draw(content)
    }
    fn hide_cursor(&mut self) -> io::Result<()> { self.inner.hide_cursor() }
    fn show_cursor(&mut self) -> io::Result<()> { self.inner.show_cursor() }
    fn get_cursor_position(&mut self) -> io::Result<Position> { Ok(Position::new(0, 0)) }
    fn set_cursor_position<P: Into<Position>>(&mut self, p: P) -> io::Result<()> {
        self.inner.set_cursor_position(p)
    }
    fn clear(&mut self) -> io::Result<()> { self.inner.clear() }
    fn size(&self) -> io::Result<Size> { Ok(Size { width: self.width, height: self.height }) }
    fn window_size(&mut self) -> io::Result<ratatui::backend::WindowSize> {
        Ok(ratatui::backend::WindowSize {
            columns_rows: Size { width: self.width, height: self.height },
            pixels: Size { width: 0, height: 0 },
        })
    }
    fn flush(&mut self) -> io::Result<()> { Backend::flush(&mut self.inner) }
}

// ── input parser ─────────────────────────────────────────────────────────

fn parse_input(data: &[u8]) -> Vec<KeyEvent> {
    let mut keys = Vec::new();
    let mut i = 0;
    while i < data.len() {
        let byte = data[i];
        // ESC sequence
        if byte == 0x1b {
            if let Some(consumed) = parse_csi(&data[i..], &mut keys) {
                i += consumed;
                continue;
            }
            // ALT+key (ESC followed by printable)
            if i + 1 < data.len() && (32..127).contains(&data[i + 1]) {
                keys.push(KeyEvent::new(KeyCode::Char(data[i + 1] as char), KeyModifiers::ALT));
                i += 2;
                continue;
            }
            keys.push(KeyEvent::new(KeyCode::Esc, KeyModifiers::empty()));
            i += 1;
            continue;
        }

        let code = match byte {
            b'\r' | b'\n' => KeyCode::Enter,
            8 | 127 => KeyCode::Backspace,
            b'\t' => KeyCode::Tab,
            1..=26 => {
                let c = (byte + b'a' - 1) as char;
                keys.push(KeyEvent::new(KeyCode::Char(c), KeyModifiers::CONTROL));
                i += 1;
                continue;
            }
            32..=126 => KeyCode::Char(byte as char),
            _ => { i += 1; continue; }
        };
        keys.push(KeyEvent::new(code, KeyModifiers::empty()));
        i += 1;
    }
    keys
}

/// Parse a CSI escape sequence; returns bytes consumed on success.
fn parse_csi(data: &[u8], keys: &mut Vec<KeyEvent>) -> Option<usize> {
    if data.len() < 3 || data[0] != 0x1b || data[1] != b'[' { return None; }
    let code = match data[2] {
        b'A' => KeyCode::Up,
        b'B' => KeyCode::Down,
        b'C' => KeyCode::Right,
        b'D' => KeyCode::Left,
        b'H' => KeyCode::Home,
        b'F' => KeyCode::End,
        b'3' if data.get(3) == Some(&b'~') => {
            keys.push(KeyEvent::new(KeyCode::Delete, KeyModifiers::empty()));
            return Some(4);
        }
        _ => return Some(2), // unknown CSI: skip ESC [
    };
    keys.push(KeyEvent::new(code, KeyModifiers::empty()));
    Some(3)
}

// ── SSH writer (buffered, flushes via russh handle) ──────────────────────

struct SshWriter {
    handle: Handle,
    channel: ChannelId,
    buf: Vec<u8>,
}

impl SshWriter {
    fn new(handle: Handle, channel: ChannelId) -> Self {
        Self { handle, channel, buf: Vec::with_capacity(16384) }
    }
}

impl Write for SshWriter {
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        self.buf.extend_from_slice(buf);
        Ok(buf.len())
    }

    fn flush(&mut self) -> io::Result<()> {
        if self.buf.is_empty() { return Ok(()); }
        let data = std::mem::take(&mut self.buf);
        let handle = self.handle.clone();
        let channel = self.channel;
        tokio::task::block_in_place(|| {
            tokio::runtime::Handle::current().block_on(async move {
                let _ = handle.data(channel, russh::CryptoVec::from_slice(&data)).await;
            });
        });
        Ok(())
    }
}
