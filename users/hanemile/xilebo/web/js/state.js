// Minimal reactive state — no library, just callbacks.

const listeners = new Set();

export const state = {
  token: localStorage.getItem('xb_token'),
  userId: localStorage.getItem('xb_user_id'),
  handle: localStorage.getItem('xb_handle'),
  rooms: new Map(),       // id -> room
  messages: new Map(),    // roomId -> [msg, ...]
  members: new Map(),     // roomId -> [member, ...]
  activeRoomId: null,
  showCreateDialog: false,
};

export function setState(partial) {
  Object.assign(state, partial);
  listeners.forEach(fn => fn(state));
}

export function subscribe(fn) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

export function setAuth(userId, handle, token) {
  localStorage.setItem('xb_token', token);
  localStorage.setItem('xb_user_id', userId);
  localStorage.setItem('xb_handle', handle);
  setState({ token, userId, handle });
}

export function clearAuth() {
  localStorage.removeItem('xb_token');
  localStorage.removeItem('xb_user_id');
  localStorage.removeItem('xb_handle');
  setState({ token: null, userId: null, handle: null, rooms: new Map(), messages: new Map(), members: new Map(), activeRoomId: null });
}

export function setRooms(list) {
  const map = new Map();
  list.forEach(r => map.set(r.id, r));
  setState({ rooms: map });
}

export function addRoom(room) {
  const map = new Map(state.rooms);
  map.set(room.id, room);
  setState({ rooms: map });
}

export function setMessages(roomId, msgs) {
  const map = new Map(state.messages);
  map.set(roomId, msgs);
  setState({ messages: map });
}

export function addMessage(roomId, msg) {
  const map = new Map(state.messages);
  const list = map.get(roomId) || [];
  if (!list.some(m => m.id === msg.id)) {
    map.set(roomId, [...list, msg]);
    setState({ messages: map });
  }
}

export function setMembers(roomId, list) {
  const map = new Map(state.members);
  map.set(roomId, list);
  setState({ members: map });
}
