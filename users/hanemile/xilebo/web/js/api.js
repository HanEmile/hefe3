// API client — zero dependencies, plain fetch.

const API = '/_xb/client/v1';
let token = localStorage.getItem('xb_token');

export function setToken(t) { token = t; if (t) localStorage.setItem('xb_token', t); else localStorage.removeItem('xb_token'); }
export function getToken() { return token; }

async function req(method, path, body) {
  const headers = { 'Content-Type': 'application/json' };
  if (token) headers['Authorization'] = `Bearer ${token}`;
  const res = await fetch(API + path, { method, headers, body: body ? JSON.stringify(body) : undefined });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ message: res.statusText }));
    throw new Error(err.message || err.error || 'Request failed');
  }
  return res.json();
}

export const register   = (username, password) => req('POST', '/register', { username, password });
export const login      = (handle, password)   => req('POST', '/login', { handle, password });
export const listRooms  = ()                   => req('GET', '/rooms');
export const getRoom    = (id)                 => req('GET', `/rooms/${id}`);
export const createRoom = (name, topic)        => req('POST', '/rooms', { name, topic });
export const joinRoom   = (id)                 => req('POST', `/rooms/${id}/join`);
export const leaveRoom  = (id)                 => req('POST', `/rooms/${id}/leave`);
export const getMembers = (id)                 => req('GET', `/rooms/${id}/members`);
export const sendMsg    = (roomId, content)    => req('POST', `/rooms/${roomId}/send`, { content });
export const getMsgs    = (roomId, limit = 50) => req('GET', `/rooms/${roomId}/messages?limit=${limit}`);
export const sync       = (since)              => req('GET', `/sync${since ? '?since=' + since : ''}`);
export const getProfile = (handle)             => req('GET', `/profile/${encodeURIComponent(handle)}`);
export const updateProfile = (p)               => req('PUT', '/profile', p);
export const uploadKeys = (p)                  => req('POST', '/keys/upload', p);
export const claimKeys  = (userId)             => req('POST', '/keys/claim', { user_id: userId });
export const iceServers = ()                   => req('GET', '/calls/ice-servers');
