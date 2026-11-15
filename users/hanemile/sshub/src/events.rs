use anyhow::Result;
use crossterm::event::{self, Event, KeyCode, KeyEvent, KeyEventKind, KeyModifiers};

use crate::app::*;
use crate::db::Db;

pub fn handle_events(app: &mut App, db: &Db) -> Result<()> {
    if let Event::Key(key) = event::read()? {
        if key.kind == KeyEventKind::Press {
            handle_key(app, key, db)?;
        }
    }
    Ok(())
}

pub fn handle_key(app: &mut App, key: KeyEvent, db: &Db) -> Result<()> {
    // Ctrl+O: toggle settings popup (works from anywhere).
    if key.modifiers.contains(KeyModifiers::CONTROL)
        && matches!(key.code, KeyCode::Char('o') | KeyCode::Char('O'))
    {
        app.popup = if matches!(app.popup, Popup::Settings) {
            Popup::None
        } else {
            app.settings_editing = false;
            app.settings_field = SettingsField::Username;
            Popup::Settings
        };
        return Ok(());
    }

    // Popups intercept input.
    match &app.popup {
        Popup::Settings => return handle_settings(app, key, db),
        Popup::UserProfile(_) => {
            if matches!(key.code, KeyCode::Esc | KeyCode::Char('q') | KeyCode::Enter) {
                app.popup = Popup::None;
            }
            return Ok(());
        }
        Popup::ReplyChain(_) => {
            match key.code {
                KeyCode::Up | KeyCode::Char('k') => {
                    app.popup_scroll = app.popup_scroll.saturating_sub(1);
                }
                KeyCode::Down | KeyCode::Char('j') => app.popup_scroll += 1,
                KeyCode::Esc | KeyCode::Char('q') => app.popup = Popup::None,
                _ => app.popup = Popup::None,
            }
            return Ok(());
        }
        Popup::ConfirmDelete => {
            if matches!(key.code, KeyCode::Char('y') | KeyCode::Char('Y')) {
                if let Some(uid) = &app.user_id {
                    db.delete_user(uid)?;
                }
                app.exit = true;
            }
            app.popup = Popup::None;
            return Ok(());
        }
        Popup::None => {}
    }

    if app.search_active {
        return handle_search(app, key);
    }
    if app.input_mode == InputMode::Composing {
        return handle_composing(app, key, db);
    }

    // Normal-mode globals.
    match key.code {
        KeyCode::Char('q') | KeyCode::Char('Q') => {
            app.exit = true;
            return Ok(());
        }
        KeyCode::Tab => {
            app.workspace = match app.workspace {
                Workspace::Chat => Workspace::Forum,
                Workspace::Forum => Workspace::Chat,
            };
            app.entered = false;
            return Ok(());
        }
        KeyCode::Char('s') => {
            app.search_active = true;
            app.search_query.clear();
            app.search_selected = 0;
            app.search_kind = current_search_kind(app);
            return Ok(());
        }
        _ => {}
    }

    match app.workspace {
        Workspace::Chat => handle_chat(app, key, db),
        Workspace::Forum => handle_forum(app, key),
    }
    Ok(())
}

fn current_search_kind(app: &App) -> SearchKind {
    match (app.workspace, app.chat_focus, app.forum_focus) {
        (Workspace::Chat, ChatFocus::Rooms, _) => SearchKind::Rooms,
        (Workspace::Chat, ChatFocus::Users, _) => SearchKind::Users,
        (Workspace::Chat, _, _) => SearchKind::Messages,
        (Workspace::Forum, _, ForumFocus::Subforums) => SearchKind::Subforums,
        (Workspace::Forum, _, _) => SearchKind::Posts,
    }
}

// ── composing ────────────────────────────────────────────────────────────

fn handle_composing(app: &mut App, key: KeyEvent, db: &Db) -> Result<()> {
    match key.code {
        KeyCode::Esc => {
            app.reset_input();
            app.reply_to_id = None;
            app.reply_to_comment_id = None;
        }
        KeyCode::Enter => match app.compose_stage {
            ComposeStage::None => match app.workspace {
                Workspace::Chat => app.send_message(db)?,
                Workspace::Forum => {}
            },
            ComposeStage::PostTitle => {
                if !app.compose_title.trim().is_empty() {
                    app.compose_stage = ComposeStage::PostBody;
                    app.input_buffer.clear();
                    app.cursor_pos = 0;
                }
            }
            ComposeStage::PostBody => app.submit_post(db)?,
            ComposeStage::Comment => app.submit_comment(db)?,
            ComposeStage::RoomName => {
                if !app.compose_title.trim().is_empty() {
                    app.compose_stage = ComposeStage::RoomTopic;
                    app.input_buffer.clear();
                    app.cursor_pos = 0;
                }
            }
            ComposeStage::RoomTopic => app.submit_room(db)?,
            ComposeStage::SubforumName => app.submit_subforum(db)?,
        },
        KeyCode::Backspace => match app.compose_stage {
            ComposeStage::PostTitle
            | ComposeStage::RoomName
            | ComposeStage::SubforumName => { app.compose_title.pop(); }
            _ => app.backspace(),
        },
        KeyCode::Char(c) => match app.compose_stage {
            ComposeStage::PostTitle
            | ComposeStage::RoomName
            | ComposeStage::SubforumName => app.compose_title.push(c),
            _ => app.insert_char(c),
        },
        _ => {}
    }
    Ok(())
}

// ── chat ─────────────────────────────────────────────────────────────────

fn handle_chat(app: &mut App, key: KeyEvent, db: &Db) {
    // Users-panel quick-jump (configurable key).
    if !app.entered {
        if let KeyCode::Char(c) = key.code {
            if c == app.users_view_key && app.chat_focus != ChatFocus::Users {
                app.chat_focus = ChatFocus::Users;
                app.selected_user = 0;
                return;
            }
        }
    }

    // Enter toggles entered, except in Users where it opens profile.
    if matches!(key.code, KeyCode::Enter) {
        if !app.entered && app.chat_focus == ChatFocus::Users {
            if let Some(u) = app.users.get(app.selected_user) {
                app.popup = Popup::UserProfile(u.name.clone());
            }
            return;
        }
        app.entered = !app.entered;
        if !app.entered { app.input_mode = InputMode::Normal; }
        return;
    }

    // h/l: panel switching (only when not entered).
    if !app.entered && matches!(key.code, KeyCode::Left | KeyCode::Char('h') | KeyCode::Right | KeyCode::Char('l')) {
        let dir_right = matches!(key.code, KeyCode::Right | KeyCode::Char('l'));
        app.chat_focus = match (app.chat_focus, dir_right) {
            (ChatFocus::Rooms, true) => ChatFocus::Messages,
            (ChatFocus::Messages, true) => ChatFocus::Users,
            (ChatFocus::Users, false) => ChatFocus::Messages,
            (ChatFocus::Messages, false) => ChatFocus::Rooms,
            (other, _) => other,
        };
        if app.chat_focus == ChatFocus::Rooms { app.selected_message = None; }
        return;
    }

    match (app.chat_focus, key.code) {
        (_, KeyCode::Esc) => {
            app.selected_message = None;
            app.entered = false;
        }
        (ChatFocus::Rooms, KeyCode::Up | KeyCode::Char('k')) if app.selected_room > 0 => {
            app.selected_room -= 1;
            app.chat_scroll = app.rooms[app.selected_room].messages.len().saturating_sub(1);
            app.selected_message = None;
            app.mark_current_room_read(db);
        }
        (ChatFocus::Rooms, KeyCode::Down | KeyCode::Char('j')) if app.selected_room + 1 < app.rooms.len() => {
            app.selected_room += 1;
            app.chat_scroll = app.rooms[app.selected_room].messages.len().saturating_sub(1);
            app.selected_message = None;
            app.mark_current_room_read(db);
        }
        (ChatFocus::Rooms, KeyCode::Char('n')) => app.start_compose(ComposeStage::RoomName),
        (ChatFocus::Messages, KeyCode::Up | KeyCode::Char('k')) => app.select_message(-1),
        (ChatFocus::Messages, KeyCode::Down | KeyCode::Char('j')) => app.select_message(1),
        (ChatFocus::Messages, KeyCode::Char('i') | KeyCode::Char('a')) => {
            app.input_mode = InputMode::Composing;
            app.compose_stage = ComposeStage::None;
            app.entered = true;
        }
        (ChatFocus::Messages, KeyCode::Char('r')) => {
            if let Some(idx) = app.selected_message {
                if let Some(msg) = app.rooms[app.selected_room].messages.get(idx) {
                    app.reply_to_id = Some(msg.id.clone());
                    app.input_mode = InputMode::Composing;
                    app.compose_stage = ComposeStage::None;
                    app.entered = true;
                }
            }
        }
        (ChatFocus::Messages, KeyCode::Char('v')) => {
            let chain = app.build_reply_chain();
            if !chain.is_empty() {
                app.popup = Popup::ReplyChain(chain);
                app.popup_scroll = 0;
            }
        }
        (ChatFocus::Messages, KeyCode::Char('G')) => {
            let len = app.rooms[app.selected_room].messages.len();
            app.chat_scroll = len.saturating_sub(1);
            app.selected_message = (len > 0).then(|| len - 1);
        }
        (ChatFocus::Users, KeyCode::Up | KeyCode::Char('k')) if app.selected_user > 0 => {
            app.selected_user -= 1;
        }
        (ChatFocus::Users, KeyCode::Down | KeyCode::Char('j')) if app.selected_user + 1 < app.users.len() => {
            app.selected_user += 1;
        }
        _ => {}
    }
}

// ── forum ────────────────────────────────────────────────────────────────

fn handle_forum(app: &mut App, key: KeyEvent) {
    if matches!(key.code, KeyCode::Enter) {
        app.entered = !app.entered;
        return;
    }

    // h/l: panel switching (only when not entered, with caveat for collapse/expand).
    if !app.entered {
        match key.code {
            KeyCode::Left | KeyCode::Char('h') => {
                app.forum_focus = match app.forum_focus {
                    ForumFocus::Subforums => ForumFocus::Subforums,
                    ForumFocus::Posts => ForumFocus::Subforums,
                    ForumFocus::Thread => ForumFocus::Posts,
                };
                return;
            }
            KeyCode::Right | KeyCode::Char('l') => {
                app.forum_focus = match app.forum_focus {
                    ForumFocus::Subforums => ForumFocus::Posts,
                    ForumFocus::Posts if app.current_subforum().map_or(false, |s| !s.posts.is_empty()) => {
                        app.thread_scroll = 0;
                        app.selected_comment = None;
                        ForumFocus::Thread
                    }
                    other => other,
                };
                return;
            }
            _ => {}
        }
    }

    match app.forum_focus {
        ForumFocus::Subforums => handle_subforum(app, key),
        ForumFocus::Posts => handle_posts(app, key),
        ForumFocus::Thread => handle_thread(app, key),
    }
}

fn handle_subforum(app: &mut App, key: KeyEvent) {
    let filtered = app.filtered_subforum_indices();
    match key.code {
        KeyCode::Up | KeyCode::Char('k') => {
            if let Some(pos) = filtered.iter().position(|&i| i == app.selected_subforum) {
                if pos > 0 {
                    app.selected_subforum = filtered[pos - 1];
                    app.selected_post = 0;
                }
            }
        }
        KeyCode::Down | KeyCode::Char('j') => {
            if let Some(pos) = filtered.iter().position(|&i| i == app.selected_subforum) {
                if pos + 1 < filtered.len() {
                    app.selected_subforum = filtered[pos + 1];
                    app.selected_post = 0;
                }
            }
        }
        KeyCode::Backspace => { app.subforum_filter.pop(); }
        KeyCode::Esc => app.subforum_filter.clear(),
        KeyCode::Char('n') => app.start_compose(ComposeStage::SubforumName),
        KeyCode::Char(c) => {
            app.subforum_filter.push(c);
            let f = app.filtered_subforum_indices();
            if !f.contains(&app.selected_subforum) {
                if let Some(&first) = f.first() {
                    app.selected_subforum = first;
                    app.selected_post = 0;
                }
            }
        }
        _ => {}
    }
}

fn handle_posts(app: &mut App, key: KeyEvent) {
    let count = app.current_subforum().map_or(0, |s| s.posts.len());
    match key.code {
        KeyCode::Up | KeyCode::Char('k') if app.selected_post > 0 => app.selected_post -= 1,
        KeyCode::Down | KeyCode::Char('j') if app.selected_post + 1 < count => app.selected_post += 1,
        KeyCode::Char('n') => app.start_compose(ComposeStage::PostTitle),
        _ => {}
    }
}

fn handle_thread(app: &mut App, key: KeyEvent) {
    let count = app.current_post().map_or(0, |p| p.comments.len());
    match key.code {
        KeyCode::Char('h') if app.selected_comment.is_some() => {
            if let Some(idx) = app.selected_comment {
                if let Some(c) = app.current_post().and_then(|p| p.comments.get(idx)) {
                    let id = c.id.clone();
                    if !app.is_comment_collapsed(&id) {
                        app.toggle_comment_collapsed(&id);
                    }
                }
            }
        }
        KeyCode::Char('l') if app.selected_comment.is_some() => {
            if let Some(idx) = app.selected_comment {
                if let Some(c) = app.current_post().and_then(|p| p.comments.get(idx)) {
                    let id = c.id.clone();
                    if app.is_comment_collapsed(&id) {
                        app.toggle_comment_collapsed(&id);
                    }
                }
            }
        }
        KeyCode::Up | KeyCode::Char('k') if count > 0 => {
            app.selected_comment = Some(match app.selected_comment {
                None => count - 1,
                Some(0) => 0,
                Some(i) => i - 1,
            });
        }
        KeyCode::Down | KeyCode::Char('j') if count > 0 => {
            app.selected_comment = Some(match app.selected_comment {
                None => 0,
                Some(i) if i + 1 < count => i + 1,
                Some(i) => i,
            });
        }
        KeyCode::Char('r') => {
            if let Some(idx) = app.selected_comment {
                if let Some(c) = app.current_post().and_then(|p| p.comments.get(idx)) {
                    app.reply_to_comment_id = Some(c.id.clone());
                    app.start_compose(ComposeStage::Comment);
                }
            }
        }
        KeyCode::Char('c') if app.current_post().is_some() => {
            app.reply_to_comment_id = None;
            app.start_compose(ComposeStage::Comment);
        }
        KeyCode::Char('n') => app.start_compose(ComposeStage::PostTitle),
        KeyCode::Esc => app.selected_comment = None,
        _ => {}
    }
}

// ── settings popup ───────────────────────────────────────────────────────

fn handle_settings(app: &mut App, key: KeyEvent, db: &Db) -> Result<()> {
    if app.settings_editing {
        match app.settings_field {
            SettingsField::Username => match key.code {
                KeyCode::Enter => {
                    let new_name = app.input_buffer.trim().to_string();
                    if !new_name.is_empty() && new_name != app.username {
                        if let Some(uid) = &app.user_id {
                            db.rename_user(uid, &new_name)?;
                        }
                        app.username = new_name;
                        app.refresh_users(db)?;
                    }
                    app.reset_input();
                    app.settings_editing = false;
                }
                KeyCode::Esc => {
                    app.reset_input();
                    app.settings_editing = false;
                }
                KeyCode::Backspace => app.backspace(),
                KeyCode::Char(c) => app.insert_char(c),
                _ => {}
            },
            SettingsField::UsersKey => match key.code {
                KeyCode::Esc => app.settings_editing = false,
                KeyCode::Char(c) => {
                    app.users_view_key = c;
                    app.save_setting(db, "users_view_key", &c.to_string())?;
                    app.settings_editing = false;
                }
                _ => {}
            },
            SettingsField::Theme => match key.code {
                KeyCode::Esc | KeyCode::Enter => app.settings_editing = false,
                KeyCode::Left | KeyCode::Right | KeyCode::Char('h') | KeyCode::Char('l') | KeyCode::Tab => {
                    app.theme = app.theme.cycle_next();
                    app.save_setting(db, "theme", app.theme.name())?;
                }
                _ => {}
            },
            SettingsField::DeleteAccount => {
                app.settings_editing = false;
                app.popup = Popup::ConfirmDelete;
            }
        }
        return Ok(());
    }

    match key.code {
        KeyCode::Esc => app.popup = Popup::None,
        KeyCode::Up | KeyCode::Char('k') => app.settings_field = app.settings_field.prev(),
        KeyCode::Down | KeyCode::Char('j') => app.settings_field = app.settings_field.next(),
        KeyCode::Enter | KeyCode::Char('i') => {
            app.settings_editing = true;
            if app.settings_field == SettingsField::Username {
                app.input_mode = InputMode::Composing;
                app.input_buffer = app.username.clone();
                app.cursor_pos = app.input_buffer.len();
            }
        }
        _ => {}
    }
    Ok(())
}

// ── search popup ─────────────────────────────────────────────────────────

fn handle_search(app: &mut App, key: KeyEvent) -> Result<()> {
    match key.code {
        KeyCode::Esc => {
            app.search_active = false;
            app.search_query.clear();
        }
        KeyCode::Enter => {
            apply_search(app);
            app.search_active = false;
            app.search_query.clear();
        }
        KeyCode::Up => app.search_selected = app.search_selected.saturating_sub(1),
        KeyCode::Down => {
            let max = search_results(app).len().saturating_sub(1);
            if app.search_selected < max { app.search_selected += 1; }
        }
        KeyCode::Backspace => { app.search_query.pop(); app.search_selected = 0; }
        KeyCode::Char(c) => { app.search_query.push(c); app.search_selected = 0; }
        _ => {}
    }
    Ok(())
}

pub fn search_results(app: &App) -> Vec<(usize, String)> {
    let q = &app.search_query;
    let m = |s: &str| App::fuzzy_match(q, s);
    match app.search_kind {
        SearchKind::Rooms => app.rooms.iter().enumerate()
            .filter(|(_, r)| m(&r.name) || m(&r.topic))
            .map(|(i, r)| (i, format!("#{} — {}", r.name, r.topic))).collect(),
        SearchKind::Subforums => app.subforums.iter().enumerate()
            .filter(|(_, s)| m(&s.name))
            .map(|(i, s)| (i, format!("f/{} ({} posts)", s.name, s.posts.len()))).collect(),
        SearchKind::Users => app.users.iter().enumerate()
            .filter(|(_, u)| m(&u.name))
            .map(|(i, u)| (i, u.name.clone())).collect(),
        SearchKind::Messages => app.current_room().map_or(vec![], |room| {
            room.messages.iter().enumerate()
                .filter(|(_, msg)| m(&msg.content) || m(&msg.author))
                .map(|(i, msg)| {
                    let preview: String = msg.content.chars().take(60).collect();
                    (i, format!("{}: {}", msg.author, preview))
                }).collect()
        }),
        SearchKind::Posts => app.current_subforum().map_or(vec![], |sf| {
            sf.posts.iter().enumerate()
                .filter(|(_, p)| m(&p.title) || m(&p.content))
                .map(|(i, p)| (i, format!("{} — by {}", p.title, p.author))).collect()
        }),
    }
}

fn apply_search(app: &mut App) {
    let results = search_results(app);
    let Some((idx, _)) = results.get(app.search_selected).cloned() else { return; };
    match app.search_kind {
        SearchKind::Rooms => {
            app.workspace = Workspace::Chat;
            app.chat_focus = ChatFocus::Messages;
            app.selected_room = idx;
            app.chat_scroll = app.rooms[idx].messages.len().saturating_sub(1);
        }
        SearchKind::Subforums => {
            app.workspace = Workspace::Forum;
            app.forum_focus = ForumFocus::Posts;
            app.selected_subforum = idx;
            app.selected_post = 0;
        }
        SearchKind::Users => {
            if let Some(u) = app.users.get(idx) {
                app.popup = Popup::UserProfile(u.name.clone());
            }
        }
        SearchKind::Messages => {
            app.chat_focus = ChatFocus::Messages;
            app.selected_message = Some(idx);
            app.chat_scroll = idx;
        }
        SearchKind::Posts => {
            app.forum_focus = ForumFocus::Thread;
            app.selected_post = idx;
            app.thread_scroll = 0;
        }
    }
}

