mod app;
mod broadcast;
mod components;
mod db;
mod events;
mod models;
mod ssh;
mod theme;
mod tui_session;
mod ui;

use anyhow::Result;
use db::Db;
use std::sync::{Arc, Mutex};

fn main() -> Result<()> {
    let db = Db::open()?;

    // SSH server mode: SSHUB_LISTEN=0.0.0.0:2222 cargo run
    if let Ok(bind) = std::env::var("SSHUB_LISTEN") {
        let db = Arc::new(Mutex::new(db));
        let (broadcast_tx, _) = broadcast::channel();
        let rt = tokio::runtime::Runtime::new()?;
        rt.block_on(ssh::SshServer::new(db, broadcast_tx).run(&bind))?;
        return Ok(());
    }

    // Standalone TUI mode (used by mosh-server, or for local dev).
    // Identity: if SSHUB_PUBKEY is set (by the mosh exec handler), look that
    // user up; otherwise fall back to SSHUB_USER (default "dev") for local dev.
    let user = resolve_user(&db)?;
    let mut app = app::App::from_db(&db, Some(&user.id), &user.name)?;

    // Heartbeat: keep our presence row fresh while the TUI is running.
    let heartbeat_stop = std::sync::Arc::new(std::sync::atomic::AtomicBool::new(false));
    let hb_stop = heartbeat_stop.clone();
    let hb_uid = user.id.clone();
    let hb_thread = std::thread::spawn(move || {
        // Re-open a DB connection from this thread. (Connection isn't Send.)
        let Ok(local_db) = Db::open() else { return; };
        while !hb_stop.load(std::sync::atomic::Ordering::Relaxed) {
            let _ = local_db.presence_heartbeat(&hb_uid);
            std::thread::sleep(std::time::Duration::from_secs(10));
        }
        let _ = local_db.presence_clear(&hb_uid);
    });

    let mut terminal = ratatui::init();
    let result = run_local(&mut terminal, &mut app, &db);
    ratatui::restore();

    heartbeat_stop.store(true, std::sync::atomic::Ordering::Relaxed);
    let _ = hb_thread.join();
    let _ = db.presence_clear(&user.id);

    result
}

fn resolve_user(db: &Db) -> Result<crate::models::User> {
    if let Ok(pubkey) = std::env::var("SSHUB_PUBKEY") {
        if !pubkey.is_empty() {
            if let Some(u) = db.find_user_by_pubkey(&pubkey)? {
                return Ok(u);
            }
            let name = db.unique_guest_name()?;
            return db.create_user(&name, &pubkey);
        }
    }
    let username = std::env::var("SSHUB_USER").unwrap_or_else(|_| "dev".into());
    if let Some(u) = db.list_users()?.into_iter().find(|u| u.name == username) {
        return Ok(u);
    }
    db.create_user(&username, "local-dev-no-key")
}

fn run_local(
    terminal: &mut ratatui::DefaultTerminal,
    app: &mut app::App,
    db: &db::Db,
) -> Result<()> {
    use std::time::{Duration, Instant};
    let mut last_refresh = Instant::now();
    while !app.exit {
        terminal.draw(|f| ui::render(app, f))?;

        // Block for a keypress for up to 5s; on timeout, refresh presence/data.
        if crossterm::event::poll(Duration::from_secs(5))? {
            events::handle_events(app, db)?;
        }
        if last_refresh.elapsed() >= Duration::from_secs(5) {
            let _ = app.refresh_users(db);
            if let Ok(rooms) = db.load_rooms() {
                // Preserve in-memory selection; just merge new messages.
                for r_new in rooms {
                    if let Some(r) = app.rooms.iter_mut().find(|r| r.id == r_new.id) {
                        r.messages = r_new.messages;
                    }
                }
            }
            last_refresh = Instant::now();
        }
    }
    Ok(())
}
