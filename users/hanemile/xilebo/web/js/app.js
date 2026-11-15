// xilebo web client — pure vanilla JS, zero dependencies.

import * as api from './api.js';
import * as ws from './ws.js';
import { state, subscribe, setAuth, clearAuth, setRooms, addRoom, setMessages, addMessage, setMembers, setState } from './state.js';
import { h, esc, nameFromHandle, initial, formatTime, renderText } from './render.js';

const $ = (sel, el = document) => el.querySelector(sel);
const root = () => $('#app');

// ── Routing ──────────────────────────────────────────────

let currentView = null;

function navigate(hash) { location.hash = hash; }

function route() {
  const hash = location.hash.slice(1) || '/';
  if (!state.token) {
    if (hash === '/register') renderRegister();
    else renderLogin();
    return;
  }
  const roomMatch = hash.match(/^\/rooms\/(.+)$/);
  if (roomMatch) {
    setState({ activeRoomId: roomMatch[1] });
    renderApp();
  } else {
    setState({ activeRoomId: null });
    renderApp();
  }
}

window.addEventListener('hashchange', route);

// ── Auth views ───────────────────────────────────────────

function renderLogin() {
  currentView = 'login';
  root().innerHTML = '';
  root().append(h('div', { class: 'auth-container' },
    h('div', { class: 'auth-box' },
      h('h1', null, 'Welcome back'),
      h('p', { class: 'subtitle' }, 'Sign in to your xilebo account'),
      h('div', { class: 'error-msg', id: 'err' }),
      h('form', { id: 'login-form' },
        h('div', { class: 'field' },
          h('label', { for: 'handle' }, 'Handle'),
          h('input', { id: 'handle', type: 'text', placeholder: '@user@server', required: '', autocomplete: 'username' })),
        h('div', { class: 'field' },
          h('label', { for: 'pw' }, 'Password'),
          h('input', { id: 'pw', type: 'password', placeholder: 'Password', required: '', autocomplete: 'current-password' })),
        h('button', { class: 'btn', type: 'submit' }, 'Log In')),
      h('div', { class: 'auth-switch' }, 'No account? ', h('a', { onclick: () => navigate('/register') }, 'Register'))
    )
  ));
  $('#login-form').onsubmit = async (e) => {
    e.preventDefault();
    const btn = $('button', e.target); btn.disabled = true;
    $('#err').textContent = '';
    try {
      const u = await api.login($('#handle').value, $('#pw').value);
      api.setToken(u.token);
      setAuth(u.user_id, u.handle, u.token);
      startSession();
      navigate('/');
    } catch (err) { $('#err').textContent = err.message; btn.disabled = false; }
  };
}

function renderRegister() {
  currentView = 'register';
  root().innerHTML = '';
  root().append(h('div', { class: 'auth-container' },
    h('div', { class: 'auth-box' },
      h('h1', null, 'Create account'),
      h('p', { class: 'subtitle' }, 'Join xilebo'),
      h('div', { class: 'error-msg', id: 'err' }),
      h('form', { id: 'reg-form' },
        h('div', { class: 'field' },
          h('label', { for: 'user' }, 'Username'),
          h('input', { id: 'user', type: 'text', placeholder: 'alice', required: '', pattern: '[a-z0-9._-]{1,64}', autocomplete: 'username' })),
        h('div', { class: 'field' },
          h('label', { for: 'pw' }, 'Password'),
          h('input', { id: 'pw', type: 'password', placeholder: 'Password', required: '', minlength: '8', autocomplete: 'new-password' })),
        h('button', { class: 'btn', type: 'submit' }, 'Create Account')),
      h('div', { class: 'auth-switch' }, 'Have an account? ', h('a', { onclick: () => navigate('/login') }, 'Log in'))
    )
  ));
  $('#reg-form').onsubmit = async (e) => {
    e.preventDefault();
    const btn = $('button', e.target); btn.disabled = true;
    $('#err').textContent = '';
    try {
      const u = await api.register($('#user').value, $('#pw').value);
      api.setToken(u.token);
      setAuth(u.user_id, u.handle, u.token);
      startSession();
      navigate('/');
    } catch (err) { $('#err').textContent = err.message; btn.disabled = false; }
  };
}

// ── Main app layout ──────────────────────────────────────

function renderApp() {
  if (currentView === 'app' && root().querySelector('.layout')) {
    // Partial updates
    renderRoomList();
    renderMainView();
    renderPanel();
    return;
  }
  currentView = 'app';
  root().innerHTML = '';

  const layout = h('div', { class: 'layout' },
    renderServers(),
    renderSidebar(),
    h('div', { class: 'main', id: 'main' }),
    h('div', { class: 'panel', id: 'panel' })
  );
  root().append(layout);
  renderRoomList();
  renderMainView();
  renderPanel();
}

function renderServers() {
  return h('div', { class: 'servers' },
    h('div', { class: 'srv-icon active', title: 'Home' }, 'H'),
    h('div', { style: { width: '32px', borderTop: '2px solid var(--divider)', margin: '4px 0' } })
  );
}

function renderSidebar() {
  const name = nameFromHandle(state.handle);
  const sidebar = h('div', { class: 'sidebar' },
    h('div', { class: 'sidebar-head' }, 'xilebo'),
    h('div', { class: 'sidebar-body', id: 'room-list' }),
    h('div', { class: 'sidebar-foot' },
      h('div', { class: 'name' },
        h('div', { class: 'display' }, name),
        h('div', { class: 'handle' }, state.handle || '')),
      h('button', { title: 'Log out', onclick: doLogout, style: { color: 'var(--text-muted)', fontSize: '1.1rem' } }, '\u23FB')
    )
  );
  return sidebar;
}

function renderRoomList() {
  const el = $('#room-list');
  if (!el) return;
  el.innerHTML = '';

  const chatRooms = [];
  const videoRooms = [];
  for (const [id, room] of state.rooms) {
    (room.room_type === 'video' ? videoRooms : chatRooms).push({ id, ...room });
  }

  el.append(
    h('div', { class: 'section-hdr' },
      h('span', null, 'Channels'),
      h('button', { onclick: () => showCreateDialog(), title: 'Create channel' }, '+')
    )
  );

  if (chatRooms.length === 0) {
    el.append(h('div', { style: { padding: '6px 10px', color: 'var(--text-muted)', fontSize: '.85rem' } }, 'No channels yet'));
  }
  for (const r of chatRooms) {
    const item = h('div', { class: 'room-item' + (r.id === state.activeRoomId ? ' active' : ''), onclick: () => navigate('/rooms/' + r.id) },
      h('span', { class: 'room-icon' }, '#'),
      h('span', { class: 'room-name' }, r.name || 'Unnamed')
    );
    el.append(item);
  }

  if (videoRooms.length > 0) {
    el.append(h('div', { class: 'section-hdr' }, h('span', null, 'Voice')));
    for (const r of videoRooms) {
      el.append(h('div', { class: 'room-item' + (r.id === state.activeRoomId ? ' active' : ''), onclick: () => navigate('/rooms/' + r.id) },
        h('span', { class: 'room-icon' }, '\u{1F50A}'),
        h('span', { class: 'room-name' }, r.name || 'Unnamed')
      ));
    }
  }
}

// ── Main view (messages) ─────────────────────────────────

function renderMainView() {
  const el = $('#main');
  if (!el) return;
  el.innerHTML = '';

  const room = state.rooms.get(state.activeRoomId);
  if (!room) {
    el.append(h('div', { class: 'empty' }, h('div', { class: 'icon' }, '#'), h('div', null, 'Select a channel')));
    return;
  }

  el.append(
    h('div', { class: 'main-head' },
      h('span', null, '#'),
      h('span', null, room.name || 'Unnamed'),
      room.topic ? h('span', { class: 'topic' }, room.topic) : null
    ),
    renderMessageList(room.id),
    renderComposer(room.id)
  );

  // Load messages if we haven't yet
  loadMessages(room.id);
  scrollToBottom();
}

function renderMessageList(roomId) {
  const msgs = state.messages.get(roomId) || [];
  const container = h('div', { class: 'messages', id: 'msg-list' });
  container.append(h('div', { class: 'spacer' }));
  for (const m of msgs) {
    container.append(h('div', { class: 'msg' },
      h('div', { class: 'avatar' }, initial(m.sender)),
      h('div', { class: 'body' },
        h('div', { class: 'meta' },
          h('span', { class: 'sender' }, nameFromHandle(m.sender)),
          h('span', { class: 'time' }, formatTime(m.server_ts))),
        Object.assign(h('div', { class: 'text' }), { innerHTML: renderText(m.content) })
      )
    ));
  }
  return container;
}

function renderComposer(roomId) {
  const box = h('div', { class: 'composer' },
    h('div', { class: 'composer-box' },
      h('textarea', { id: 'msg-input', rows: '1', placeholder: 'Message #' + (state.rooms.get(roomId)?.name || 'channel') }),
      h('button', { class: 'send', id: 'send-btn', disabled: '' }, 'Send')
    )
  );

  setTimeout(() => {
    const input = $('#msg-input');
    const btn = $('#send-btn');
    if (!input || !btn) return;

    input.oninput = () => {
      input.style.height = 'auto';
      input.style.height = Math.min(input.scrollHeight, 200) + 'px';
      btn.disabled = !input.value.trim();
    };
    input.onkeydown = (e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); doSend(roomId); } };
    btn.onclick = () => doSend(roomId);
    input.focus();
  }, 0);

  return box;
}

async function doSend(roomId) {
  const input = $('#msg-input');
  const btn = $('#send-btn');
  const content = input?.value.trim();
  if (!content || !input) return;
  input.value = ''; input.style.height = 'auto'; btn.disabled = true;
  try {
    const msg = await api.sendMsg(roomId, content);
    addMessage(roomId, msg);
    scrollToBottom();
  } catch { input.value = content; btn.disabled = false; }
}

function scrollToBottom() {
  requestAnimationFrame(() => {
    const el = $('#msg-list');
    if (el) el.scrollTop = el.scrollHeight;
  });
}

// ── Members panel ────────────────────────────────────────

function renderPanel() {
  const el = $('#panel');
  if (!el) return;
  el.innerHTML = '';

  if (!state.activeRoomId) {
    el.append(h('div', { class: 'panel-head' }, 'Members'), h('div', { class: 'panel-body' }, h('div', { class: 'empty', style: { fontSize: '.85rem' } }, 'Select a room')));
    return;
  }

  const memberList = state.members.get(state.activeRoomId) || [];
  loadMembers(state.activeRoomId);

  const admins = memberList.filter(m => m.PowerLevel >= 50);
  const regulars = memberList.filter(m => m.PowerLevel < 50);

  const body = h('div', { class: 'panel-body' });
  if (admins.length) {
    body.append(h('div', { class: 'member-section' }, `Admin \u2014 ${admins.length}`));
    admins.forEach(m => body.append(renderMember(m)));
  }
  if (regulars.length) {
    body.append(h('div', { class: 'member-section' }, `Members \u2014 ${regulars.length}`));
    regulars.forEach(m => body.append(renderMember(m)));
  }

  el.append(h('div', { class: 'panel-head' }, `Members \u2014 ${memberList.length}`), body);
}

function renderMember(m) {
  const name = nameFromHandle(m.UserID);
  return h('div', { class: 'member' },
    h('div', { class: 'm-avatar' }, initial(m.UserID)),
    h('div', { class: 'm-name' }, name)
  );
}

// ── Create room dialog ───────────────────────────────────

function showCreateDialog() {
  const overlay = h('div', { class: 'overlay', id: 'dialog-overlay', onclick: (e) => { if (e.target.id === 'dialog-overlay') overlay.remove(); } },
    h('div', { class: 'dialog' },
      h('h2', null, 'Create Channel'),
      h('form', { id: 'create-form' },
        h('div', { class: 'field' },
          h('label', { for: 'rname' }, 'Name'),
          h('input', { id: 'rname', type: 'text', placeholder: 'general', required: '' })),
        h('div', { class: 'field' },
          h('label', { for: 'rtopic' }, 'Topic'),
          h('input', { id: 'rtopic', type: 'text', placeholder: 'What\'s this about?' })),
        h('div', { class: 'error-msg', id: 'create-err' }),
        h('div', { class: 'actions' },
          h('button', { class: 'cancel', type: 'button', onclick: () => overlay.remove() }, 'Cancel'),
          h('button', { class: 'submit', type: 'submit' }, 'Create'))
      )
    )
  );
  document.body.append(overlay);

  $('#create-form').onsubmit = async (e) => {
    e.preventDefault();
    const name = $('#rname').value.trim();
    const topic = $('#rtopic').value.trim();
    if (!name) return;
    try {
      const room = await api.createRoom(name, topic || undefined);
      addRoom(room);
      overlay.remove();
      navigate('/rooms/' + room.id);
    } catch (err) { $('#create-err').textContent = err.message; }
  };
}

// ── Session management ───────────────────────────────────

async function startSession() {
  ws.connect(state.token);
  ws.onMessage((msg) => {
    if ((msg.type === 'm.room.message' || msg.type === 'm.room.encrypted') && msg.room_id && msg.content) {
      addMessage(msg.room_id, {
        id: msg.content.id || crypto.randomUUID(),
        room_id: msg.room_id,
        sender: msg.sender || '',
        event_type: msg.type,
        content: msg.content.body || '',
        origin_ts: msg.content.origin_ts || Date.now(),
        server_ts: msg.content.server_ts || Date.now(),
      });
      if (msg.room_id === state.activeRoomId) {
        renderMainView();
        scrollToBottom();
      }
    }
  });

  try {
    const rooms = await api.listRooms();
    setRooms(rooms);
  } catch {}
}

function doLogout() {
  ws.disconnect();
  api.setToken(null);
  clearAuth();
  navigate('/login');
}

const loadedMessages = new Set();
async function loadMessages(roomId) {
  if (loadedMessages.has(roomId)) return;
  loadedMessages.add(roomId);
  try {
    const msgs = await api.getMsgs(roomId);
    setMessages(roomId, msgs.reverse());
    renderMainView();
    scrollToBottom();
  } catch { loadedMessages.delete(roomId); }
}

const loadedMembers = new Set();
async function loadMembers(roomId) {
  if (loadedMembers.has(roomId)) return;
  loadedMembers.add(roomId);
  try {
    const members = await api.getMembers(roomId);
    setMembers(roomId, members);
    renderPanel();
  } catch { loadedMembers.delete(roomId); }
}

// ── Re-render on state changes ───────────────────────────

subscribe(() => {
  if (!state.token) { route(); return; }
  if (currentView !== 'app') { route(); return; }
  // Only re-render parts that changed
  renderRoomList();
});

// ── Init ─────────────────────────────────────────────────

if (state.token) {
  api.setToken(state.token);
  startSession();
}
route();
