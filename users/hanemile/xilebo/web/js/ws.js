// WebSocket client with auto-reconnect.

let socket = null;
let shouldReconnect = false;
let reconnectDelay = 1000;
let reconnectTimer = null;
const handlers = new Set();

export function connect(token) {
  shouldReconnect = true;
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const url = `${proto}//${location.host}/_xb/client/v1/ws?token=${encodeURIComponent(token)}`;
  doConnect(url);
}

export function disconnect() {
  shouldReconnect = false;
  clearTimeout(reconnectTimer);
  if (socket) { socket.close(); socket = null; }
}

export function onMessage(fn) {
  handlers.add(fn);
  return () => handlers.delete(fn);
}

function doConnect(url) {
  try { socket = new WebSocket(url); } catch { scheduleReconnect(url); return; }
  socket.onopen = () => { reconnectDelay = 1000; };
  socket.onmessage = (e) => {
    try { const msg = JSON.parse(e.data); handlers.forEach(fn => fn(msg)); } catch {}
  };
  socket.onclose = () => { socket = null; if (shouldReconnect) scheduleReconnect(url); };
  socket.onerror = () => {};
}

function scheduleReconnect(url) {
  if (reconnectTimer) return;
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    reconnectDelay = Math.min(reconnectDelay * 2, 30000);
    doConnect(url);
  }, reconnectDelay);
}
