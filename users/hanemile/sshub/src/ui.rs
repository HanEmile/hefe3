use ratatui::{
    layout::{Constraint, Layout, Rect},
    style::{Color, Style},
    text::{Line, Span, Text},
    widgets::{Block, Borders, List, ListItem, Paragraph},
    Frame,
};

use crate::app::*;
use crate::components::{b, render_popup, s, Panel, PanelState};
use crate::models::Comment;
use crate::style;
use crate::theme::Theme;

pub fn render(app: &App, frame: &mut Frame) {
    let area = frame.area();
    let t = &app.theme;
    frame.render_widget(Block::default().style(Style::default().bg(t.bg)), area);

    let chunks = Layout::vertical([
        Constraint::Length(1), Constraint::Min(0), Constraint::Length(1),
    ]).split(area);

    render_titlebar(app, frame, chunks[0]);
    match app.workspace {
        Workspace::Chat => render_chat(app, frame, chunks[1]),
        Workspace::Forum => render_forum(app, frame, chunks[1]),
    }
    render_statusbar(app, frame, chunks[2]);

    match &app.popup {
        Popup::ReplyChain(ids) => render_reply_chain(app, frame, area, ids),
        Popup::Settings => render_settings(app, frame, area),
        Popup::UserProfile(name) => render_user_profile(app, frame, area, name),
        Popup::ConfirmDelete => render_confirm_delete(app, frame, area),
        Popup::None => {}
    }
    if app.search_active { render_search(app, frame, area); }
}

// ── chrome ───────────────────────────────────────────────────────────────

fn render_titlebar(app: &App, frame: &mut Frame, area: Rect) {
    let t = &app.theme;
    let tab = |name: &'static str, active: bool, color: Color| {
        if active { b(name, color) } else { s(name, t.fg_dim) }
    };
    let line = Line::from(vec![
        b(" ◈ sshub ", t.accent),
        s("│ ", t.border),
        tab("chat ", app.workspace == Workspace::Chat, t.accent),
        tab("forum ", app.workspace == Workspace::Forum, t.accent2),
        s("│ ", t.border),
        b(format!(" {} ", app.username), t.good),
        s("│ tab:switch  s:search  ^O:settings  q:quit", t.fg_dim),
    ]);
    frame.render_widget(Paragraph::new(line).style(Style::default().bg(t.surface)), area);
}

fn render_statusbar(app: &App, frame: &mut Frame, area: Rect) {
    let t = &app.theme;
    let mode = if app.entered { " [entered] " } else { " " };
    let body = match (app.workspace, app.chat_focus, app.forum_focus) {
        (Workspace::Chat, ChatFocus::Rooms, _) => "j/k:navigate  enter:enter  n:new room  h/l:switch",
        (Workspace::Chat, ChatFocus::Messages, _) => "j/k:select  i:compose  r:reply  v:chain  G:latest  h/l:switch",
        (Workspace::Chat, ChatFocus::Users, _) => "j/k:navigate  enter:profile  h:back",
        (Workspace::Forum, _, ForumFocus::Subforums) => "j/k:navigate  type:filter  n:new subforum  l:posts",
        (Workspace::Forum, _, ForumFocus::Posts) => "j/k:navigate  enter:open  h:back  n:new post",
        (Workspace::Forum, _, ForumFocus::Thread) => "j/k:select  h:collapse  l:expand  r:reply  c:comment  h/l:switch",
    };
    frame.render_widget(
        Paragraph::new(s(format!("{}{}", mode, body), t.fg_dim))
            .style(Style::default().bg(t.surface)),
        area,
    );
}

// ── chat workspace ───────────────────────────────────────────────────────

fn render_chat(app: &App, frame: &mut Frame, area: Rect) {
    let chunks = Layout::horizontal([
        Constraint::Length(20), Constraint::Min(0), Constraint::Length(20),
    ]).split(area);
    render_rooms(app, frame, chunks[0]);
    render_messages(app, frame, chunks[1]);
    render_users(app, frame, chunks[2]);
}

fn render_rooms(app: &App, frame: &mut Frame, area: Rect) {
    let t = &app.theme;
    let state = PanelState::focused(app.chat_focus == ChatFocus::Rooms, app.entered);
    // Panel inner width = area.width - 2 (borders). Reserve trailing space for the unread badge.
    let inner_w = area.width.saturating_sub(2) as usize;
    let items: Vec<ListItem> = app.rooms.iter().enumerate().map(|(i, r)| {
        let selected = i == app.selected_room;
        let unread = app.unread_count(r);
        let name_style = if selected {
            style!(fg: t.accent, bg: t.selection_bg, bold)
        } else {
            Style::default().fg(t.fg)
        };

        let prefix = if selected { " ▶ " } else { "   " };
        let name: String = r.name.chars().take(13).collect();
        let badge = if unread > 0 { format!("{}", unread.min(999)) } else { String::new() };

        // Pad so the badge sits flush right.
        let used = prefix.chars().count() + name.chars().count() + badge.chars().count();
        let pad = inner_w.saturating_sub(used).saturating_sub(1);
        let spaces = " ".repeat(pad);

        let badge_style = if unread > 0 && !selected {
            style!(fg: t.warn, bold)
        } else {
            name_style
        };
        ListItem::new(Line::from(vec![
            Span::styled(format!("{}{}", prefix, name), name_style),
            Span::styled(spaces, name_style),
            Span::styled(format!("{} ", badge), badge_style),
        ]))
    }).collect();

    let block = Panel::new(t, Line::from(b(" rooms ", t.accent))).state(state).block();
    frame.render_widget(List::new(items).block(block), area);
}

fn render_messages(app: &App, frame: &mut Frame, area: Rect) {
    let t = &app.theme;
    let state = PanelState::focused(app.chat_focus == ChatFocus::Messages, app.entered);
    let Some(room) = app.current_room() else { return; };

    let chunks = Layout::vertical([Constraint::Min(0), Constraint::Length(3)]).split(area);

    let lines: Vec<Line> = room.messages.iter().enumerate().map(|(i, m)| {
        let line = message_row(t, m);
        if app.selected_message == Some(i) {
            line.style(Style::default().bg(t.selection_bg))
        } else {
            line
        }
    }).collect();

    let visible = chunks[0].height.saturating_sub(2) as usize;
    let scroll = if lines.len() > visible {
        app.chat_scroll.saturating_sub(visible.saturating_sub(1)).min(lines.len() - visible)
    } else { 0 };

    let mut title_spans = vec![
        b(format!(" #{} ", room.name), t.accent),
        s(format!("— {} ", room.topic), t.fg_dim),
    ];
    if let Some(reply_id) = &app.reply_to_id {
        if let Some(m) = room.messages.iter().find(|m| &m.id == reply_id) {
            let p: String = m.content.chars().take(20).collect();
            title_spans.push(s(format!("│ ↩ {}: {}… ", m.author, p), t.accent2));
        }
    }

    let block = Panel::new(t, Line::from(title_spans)).state(state).bg(t.bg).block();
    frame.render_widget(
        Paragraph::new(Text::from(lines)).block(block).scroll((scroll as u16, 0)),
        chunks[0],
    );

    let popup_open = app.popup != Popup::None;
    let composing = !popup_open && app.input_mode == InputMode::Composing;
    let (label, text, active) = match app.compose_stage {
        ComposeStage::RoomName if composing => (
            " new room name — enter:next  esc:cancel ",
            app.compose_title.clone(),
            true,
        ),
        ComposeStage::RoomTopic if composing => (
            " new room topic — enter:create  esc:cancel ",
            app.input_buffer.clone(),
            true,
        ),
        ComposeStage::None if composing && app.chat_focus == ChatFocus::Messages => {
            let label = if app.reply_to_id.is_some() {
                " reply — enter:send  esc:cancel "
            } else {
                " message — enter:send  esc:cancel "
            };
            (label, app.input_buffer.clone(), true)
        }
        _ => (" press i to compose ", String::new(), false),
    };
    render_input(frame, chunks[1], t, label, &text, active);
}

fn render_users(app: &App, frame: &mut Frame, area: Rect) {
    let t = &app.theme;
    let focused = app.chat_focus == ChatFocus::Users;
    let state = PanelState::focused(focused, app.entered);

    let items: Vec<ListItem> = app.users.iter().enumerate().map(|(i, u)| {
        let me = u.name == app.username;
        let selected = focused && i == app.selected_user;
        let marker = if u.online { "◉" } else { "○" };
        let marker_color = if u.online { t.good } else { t.fg_dim };
        let style = match (selected, me, u.online) {
            (true, _, _) => style!(fg: t.warn, bg: t.selection_bg, bold),
            (_, true, _) => style!(fg: t.warn, bold),
            (_, _, true) => Style::default().fg(t.fg),
            _ => Style::default().fg(t.fg_dim),
        };
        let label = format!("{}{}", u.name, if me { " (you)" } else { "" });
        ListItem::new(Line::from(vec![
            s(format!(" {} ", marker), marker_color),
            Span::styled(label, style),
        ]))
    }).collect();

    let title = Line::from(vec![
        b(" users ", t.accent2),
        s(format!("({}) ", app.users.len()), t.fg_dim),
    ]);
    frame.render_widget(List::new(items).block(Panel::new(t, title).state(state).block()), area);
}

// ── forum workspace ──────────────────────────────────────────────────────

fn render_forum(app: &App, frame: &mut Frame, area: Rect) {
    let chunks = Layout::horizontal([
        Constraint::Length(20), Constraint::Length(32), Constraint::Min(0),
    ]).split(area);
    render_subforums(app, frame, chunks[0]);
    render_post_list(app, frame, chunks[1]);
    render_thread(app, frame, chunks[2]);
}

fn render_subforums(app: &App, frame: &mut Frame, area: Rect) {
    let t = &app.theme;
    let state = PanelState::focused(app.forum_focus == ForumFocus::Subforums, app.entered);
    let filtered = app.filtered_subforum_indices();

    let items: Vec<ListItem> = filtered.iter().map(|&i| {
        let sf = &app.subforums[i];
        let selected = i == app.selected_subforum;
        let style = if selected {
            style!(fg: t.accent2, bg: t.selection_bg, bold)
        } else {
            Style::default().fg(t.fg)
        };
        ListItem::new(Line::from(Span::styled(
            format!(" f/{} ({})", sf.name, sf.posts.len()), style,
        )))
    }).collect();

    let mut title = vec![b(" subforums ", t.accent2)];
    if !app.subforum_filter.is_empty() {
        title.push(s(format!("/{} ", app.subforum_filter), t.warn));
    }
    frame.render_widget(List::new(items).block(Panel::new(t, Line::from(title)).state(state).block()), area);
}

fn render_post_list(app: &App, frame: &mut Frame, area: Rect) {
    let t = &app.theme;
    let state = PanelState::focused(app.forum_focus == ForumFocus::Posts, app.entered);
    let Some(sf) = app.current_subforum() else { return; };

    let mut order: Vec<usize> = (0..sf.posts.len()).collect();
    order.sort_by(|&a, &b| sf.posts[b].timestamp.cmp(&sf.posts[a].timestamp));

    let items: Vec<ListItem> = order.iter().map(|&i| {
        let p = &sf.posts[i];
        let selected = i == app.selected_post;
        let title_style = if selected {
            style!(fg: t.accent2, bg: t.selection_bg, bold)
        } else {
            style!(fg: t.fg, bold)
        };
        let meta = format!("  by {} • {} • {} comments",
            p.author, format_age(&p.timestamp), p.comments.len());
        ListItem::new(vec![
            Line::from(Span::styled(format!(" {}", p.title), title_style)),
            Line::from(s(meta, t.fg_dim)),
            Line::from(""),
        ])
    }).collect();

    let title = Line::from(vec![b(format!(" f/{} ", sf.name), t.accent2), s("n:new post ", t.fg_dim)]);
    let block = Panel::new(t, title).state(state).block();
    if sf.posts.is_empty() {
        frame.render_widget(
            Paragraph::new("\n  no posts — press n to create one")
                .style(Style::default().fg(t.fg_dim)).block(block),
            area,
        );
    } else {
        frame.render_widget(List::new(items).block(block), area);
    }
}

fn render_thread(app: &App, frame: &mut Frame, area: Rect) {
    let t = &app.theme;
    let state = PanelState::focused(app.forum_focus == ForumFocus::Thread, app.entered);
    let chunks = Layout::vertical([Constraint::Min(0), Constraint::Length(3)]).split(area);
    let block = Panel::new(t, Line::from(b(" thread ", t.accent2)))
        .state(state).bg(t.bg).block();

    let Some(post) = app.current_post() else {
        frame.render_widget(
            Paragraph::new("\n  select a post").style(Style::default().fg(t.fg_dim)).block(block),
            chunks[0],
        );
        render_thread_input(app, frame, chunks[1]);
        return;
    };

    let inner_w = chunks[0].width.saturating_sub(4) as usize;
    let mut lines: Vec<Line> = Vec::new();
    lines.push(Line::from(Span::styled(
        format!(" {}", post.title),
        style!(fg: t.accent2, bold, underlined),
    )));
    lines.push(Line::from(s(
        format!(" posted by {}  {}", post.author, post.timestamp.format("%Y-%m-%d %H:%M")),
        t.fg_dim,
    )));
    lines.push(Line::from(s("─".repeat(inner_w), t.border)));
    lines.push(Line::from(""));
    for line in wrap_text(&post.content, inner_w) {
        lines.push(Line::from(s(format!(" {}", line), t.fg)));
    }
    lines.push(Line::from(""));
    lines.push(Line::from(s("─".repeat(inner_w), t.border)));
    lines.push(Line::from(b(format!(" {} comments", post.comments.len()), t.fg_dim)));
    lines.push(Line::from(""));

    for node in flatten_comments(&post.comments, &app.collapsed_comments) {
        render_comment_lines(app, &mut lines, &post.comments, node, inner_w);
    }

    let total = lines.len();
    let visible = chunks[0].height.saturating_sub(2) as usize;
    let scroll = (app.thread_scroll * 2).min(total.saturating_sub(visible)) as u16;
    frame.render_widget(
        Paragraph::new(Text::from(lines)).block(block).scroll((scroll, 0)),
        chunks[0],
    );

    render_thread_input(app, frame, chunks[1]);
}

fn render_thread_input(app: &App, frame: &mut Frame, area: Rect) {
    let t = &app.theme;
    let composing = app.input_mode == InputMode::Composing;
    let (label, text) = match app.compose_stage {
        ComposeStage::PostTitle if composing =>
            (" post title — enter:next  esc:cancel ", app.compose_title.clone()),
        ComposeStage::PostBody if composing =>
            (" post body — enter:submit  esc:cancel ", app.input_buffer.clone()),
        ComposeStage::Comment if composing => (
            if app.reply_to_comment_id.is_some() { " reply — enter:post  esc:cancel " }
            else { " comment — enter:post  esc:cancel " },
            app.input_buffer.clone(),
        ),
        ComposeStage::SubforumName if composing =>
            (" subforum name — enter:create  esc:cancel ", app.compose_title.clone()),
        _ if app.forum_focus == ForumFocus::Thread =>
            (" j/k:select  r:reply  c:comment  n:new post ", String::new()),
        _ => (" l/enter:open thread ", String::new()),
    };
    render_input(frame, area, t, label, &text, composing);
}

fn render_comment_lines(
    app: &App, lines: &mut Vec<Line<'static>>,
    comments: &[Comment], node: CommentNode, inner_w: usize,
) {
    let t = &app.theme;
    let c = &comments[node.idx];
    let selected = app.selected_comment == Some(node.idx);
    let collapsed = app.is_comment_collapsed(&c.id);
    let indent = "  ".repeat(node.depth);
    let uname: String = c.author.chars().take(8).collect();
    let time = c.timestamp.format("%H:%M").to_string();
    let marker = if collapsed { "▶" } else if c.reply_to.is_some() { "↩" } else { "▸" };

    let suffix = if collapsed {
        let (d, total) = App::count_descendants(comments, &c.id);
        format!(" ({}c/{}t)", d, total)
    } else {
        String::new()
    };
    let head_style = if selected {
        style!(fg: t.accent, bg: t.selection_bg, bold)
    } else {
        style!(fg: t.accent, bold)
    };
    let row_style = if selected { Style::default().bg(t.selection_bg) } else { Style::default() };

    lines.push(Line::from(vec![
        s(format!("{}{} ", indent, marker), t.fg_dim),
        Span::styled(format!("{:<8}", uname), head_style),
        s(format!("  {}", time), t.fg_dim),
        s(suffix, t.warn),
    ]).style(row_style));

    if !collapsed {
        let body_indent = format!("{}  ", indent);
        for l in wrap_text(&c.content, inner_w.saturating_sub(body_indent.len())) {
            lines.push(Line::from(s(format!("{}{}", body_indent, l), t.fg)));
        }
    }
    lines.push(Line::from(""));
}

// ── popups ───────────────────────────────────────────────────────────────

fn render_reply_chain(app: &App, frame: &mut Frame, area: Rect, ids: &[String]) {
    let t = &app.theme;
    let pw = (area.width * 2 / 3).max(50);
    let ph = area.height.saturating_sub(4);
    let inner = render_popup(
        frame, area,
        Line::from(b(" reply chain — j/k:scroll  esc:close ", t.accent)),
        t.accent, t.surface, pw, ph,
    );

    let Some(room) = app.current_room() else { return; };
    let lines: Vec<Line> = ids.iter()
        .filter_map(|id| room.messages.iter().find(|m| &m.id == id))
        .map(|m| message_row(t, m))
        .collect();

    let visible = inner.height as usize;
    let scroll = app.popup_scroll.min(lines.len().saturating_sub(visible)) as u16;
    frame.render_widget(Paragraph::new(Text::from(lines)).scroll((scroll, 0)), inner);
}

fn render_settings(app: &App, frame: &mut Frame, area: Rect) {
    let t = &app.theme;
    let inner = render_popup(
        frame, area,
        Line::from(b(" settings — ^O/esc:close ", t.warn)),
        t.warn, t.surface, 60, 18,
    );

    let chunks = Layout::vertical([
        Constraint::Length(3), Constraint::Length(3),
        Constraint::Length(3), Constraint::Length(3),
        Constraint::Min(0),
    ]).split(inner);

    let username = if app.settings_editing && app.settings_field == SettingsField::Username {
        app.input_buffer.clone()
    } else { app.username.clone() };
    settings_field(app, frame, chunks[0], "username", &username, SettingsField::Username, t.warn);

    let key_val = if app.settings_editing && app.settings_field == SettingsField::UsersKey {
        "press any key…".to_string()
    } else {
        format!("'{}'  (opens users panel from chat)", app.users_view_key)
    };
    settings_field(app, frame, chunks[1], "users panel key", &key_val, SettingsField::UsersKey, t.warn);

    let theme_val = format!("{}  ← h/l to cycle", app.theme.name());
    settings_field(app, frame, chunks[2], "theme", &theme_val, SettingsField::Theme, t.accent);

    let del_val = if app.settings_field == SettingsField::DeleteAccount { "press enter to confirm" } else { "" };
    settings_field(app, frame, chunks[3], "delete account", del_val, SettingsField::DeleteAccount, t.bad);
}

fn settings_field(
    app: &App, frame: &mut Frame, area: Rect,
    label: &str, value: &str, field: SettingsField, accent: Color,
) {
    let t = &app.theme;
    let focused = app.settings_field == field;
    let editing = focused && app.settings_editing;
    let (color, btype) = match (focused, editing) {
        (true, true) => (accent, ratatui::widgets::BorderType::Double),
        (true, false) => (accent, ratatui::widgets::BorderType::Plain),
        _ => (t.border, ratatui::widgets::BorderType::Plain),
    };
    let title_color = if focused { accent } else { t.fg_dim };
    let block = Block::default()
        .title(s(format!(" {} ", label), title_color))
        .borders(Borders::ALL).border_type(btype)
        .border_style(Style::default().fg(color))
        .style(Style::default().bg(t.bg));
    frame.render_widget(
        Paragraph::new(value.to_string()).style(Style::default().fg(t.fg)).block(block),
        area,
    );
}

fn render_user_profile(app: &App, frame: &mut Frame, area: Rect, name: &str) {
    let t = &app.theme;
    let inner = render_popup(
        frame, area,
        Line::from(b(" user profile — esc:close ", t.accent2)),
        t.accent2, t.surface, 50, 12,
    );

    let user = app.users.iter().find(|u| u.name == name);
    let mut lines = vec![Line::from("")];
    lines.push(kv_line(t, "  name:    ", name, t.warn));
    if let Some(u) = user {
        let (status, color) = if u.online { ("● online", t.good) } else { ("○ offline", t.fg_dim) };
        lines.push(kv_line(t, "  status:  ", status, color));
        let short: String = u.id.chars().take(16).collect();
        lines.push(kv_line(t, "  id:      ", &short, t.fg));

        let msgs: usize = app.rooms.iter().map(|r| r.messages.iter().filter(|m| m.author == name).count()).sum();
        let posts: usize = app.subforums.iter().map(|s| s.posts.iter().filter(|p| p.author == name).count()).sum();
        let comments: usize = app.subforums.iter()
            .flat_map(|sf| sf.posts.iter())
            .map(|p| p.comments.iter().filter(|c| c.author == name).count())
            .sum();
        lines.push(Line::from(""));
        lines.push(Line::from(s("  activity:", t.fg_dim)));
        lines.push(Line::from(s(
            format!("    {} msgs, {} posts, {} comments", msgs, posts, comments),
            t.fg,
        )));
    } else {
        lines.push(Line::from(s("  (not found)", t.fg_dim)));
    }
    frame.render_widget(Paragraph::new(Text::from(lines)), inner);
}

fn kv_line(t: &Theme, label: &str, value: &str, color: Color) -> Line<'static> {
    Line::from(vec![s(label.to_string(), t.fg_dim), b(value.to_string(), color)])
}

fn render_search(app: &App, frame: &mut Frame, area: Rect) {
    let t = &app.theme;
    let kind = match app.search_kind {
        SearchKind::Rooms => "rooms",
        SearchKind::Subforums => "subforums",
        SearchKind::Users => "users",
        SearchKind::Messages => "messages",
        SearchKind::Posts => "posts",
    };
    let inner = render_popup(
        frame, area,
        Line::from(b(format!(" search [{}] — esc:close  enter:select ", kind), t.warn)),
        t.warn, t.surface, area.width / 2, 16,
    );

    let chunks = Layout::vertical([
        Constraint::Length(1), Constraint::Length(1), Constraint::Min(0),
    ]).split(inner);

    frame.render_widget(
        Paragraph::new(Line::from(vec![
            s(" › ", t.warn),
            b(&app.search_query[..], t.fg),
            s("▌", t.warn),
        ])),
        chunks[0],
    );
    frame.render_widget(
        Paragraph::new(s("─".repeat(inner.width as usize), t.border)),
        chunks[1],
    );

    let results = crate::events::search_results(app);
    if results.is_empty() {
        frame.render_widget(Paragraph::new(s(" no matches", t.fg_dim)), chunks[2]);
        return;
    }
    let items: Vec<ListItem> = results.iter().enumerate().map(|(i, (_, label))| {
        let style = if i == app.search_selected {
            style!(fg: t.warn, bg: t.selection_bg, bold)
        } else {
            Style::default().fg(t.fg)
        };
        ListItem::new(Line::from(Span::styled(format!(" {} ", label), style)))
    }).collect();
    frame.render_widget(List::new(items), chunks[2]);
}

fn render_confirm_delete(app: &App, frame: &mut Frame, area: Rect) {
    let t = &app.theme;
    let inner = render_popup(
        frame, area,
        Line::from(b(" delete account ", t.bad)),
        t.bad, t.surface, 44, 5,
    );
    frame.render_widget(
        Paragraph::new(b("  press y to confirm, any key cancels", t.bad)),
        inner,
    );
}

// ── helpers ──────────────────────────────────────────────────────────────

fn message_row(t: &Theme, m: &crate::models::Message) -> Line<'static> {
    let time = m.timestamp.format("%H:%M").to_string();
    let uname: String = m.author.chars().take(8).collect();
    let reply = if m.reply_to.is_some() { "↩ " } else { "  " };
    let preview: String = m.content.lines().next().unwrap_or("").chars().take(200).collect();
    Line::from(vec![
        s(format!("{} ", time), t.fg_dim),
        s(reply, t.accent2),
        b(format!("{:<8} ", uname), t.accent),
        s(preview, t.fg),
    ])
}

fn render_input(frame: &mut Frame, area: Rect, t: &Theme, label: &str, text: &str, active: bool) {
    let color = if active { t.accent } else { t.border };
    let block = Block::default()
        .title(s(label.to_string(), t.fg_dim))
        .borders(Borders::ALL)
        .border_style(Style::default().fg(color))
        .style(Style::default().bg(t.surface));
    frame.render_widget(
        Paragraph::new(text.to_string()).style(Style::default().fg(t.fg)).block(block),
        area,
    );
}

fn wrap_text(text: &str, width: usize) -> Vec<String> {
    if width == 0 { return vec![text.into()]; }
    let mut out = Vec::new();
    for line in text.lines() {
        if line.is_empty() { out.push(String::new()); continue; }
        let mut cur = String::new();
        for word in line.split_whitespace() {
            if cur.is_empty() { cur = word.into(); }
            else if cur.len() + 1 + word.len() <= width { cur.push(' '); cur.push_str(word); }
            else { out.push(std::mem::take(&mut cur)); cur = word.into(); }
        }
        if !cur.is_empty() { out.push(cur); }
    }
    out
}

fn format_age(ts: &chrono::DateTime<chrono::Utc>) -> String {
    let dur = chrono::Utc::now().signed_duration_since(*ts);
    if dur.num_days() > 0 { format!("{}d ago", dur.num_days()) }
    else if dur.num_hours() > 0 { format!("{}h ago", dur.num_hours()) }
    else if dur.num_minutes() > 0 { format!("{}m ago", dur.num_minutes()) }
    else { "just now".into() }
}

// ── comment tree ─────────────────────────────────────────────────────────

#[derive(Clone, Copy)]
struct CommentNode { idx: usize, depth: usize }

fn flatten_comments(comments: &[Comment], collapsed: &[String]) -> Vec<CommentNode> {
    use std::collections::HashMap;
    let mut children: HashMap<&str, Vec<usize>> = HashMap::new();
    let mut roots: Vec<usize> = Vec::new();
    let id_set: std::collections::HashSet<&str> = comments.iter().map(|c| c.id.as_str()).collect();
    for (i, c) in comments.iter().enumerate() {
        match c.reply_to.as_deref() {
            Some(p) if id_set.contains(p) => children.entry(p).or_default().push(i),
            _ => roots.push(i),
        }
    }

    fn walk(
        idx: usize, depth: usize,
        comments: &[Comment],
        children: &std::collections::HashMap<&str, Vec<usize>>,
        collapsed: &[String],
        out: &mut Vec<CommentNode>,
    ) {
        out.push(CommentNode { idx, depth });
        if collapsed.iter().any(|c| c == &comments[idx].id) { return; }
        if let Some(kids) = children.get(comments[idx].id.as_str()) {
            for &k in kids { walk(k, depth + 1, comments, children, collapsed, out); }
        }
    }

    let mut out = Vec::with_capacity(comments.len());
    for root in roots { walk(root, 0, comments, &children, collapsed, &mut out); }
    out
}
