// js/nodeed.js — Canvas-based node editor.
// Nodes represent symbols/files/functions with typed I/O ports.
// Edges are bezier curves. State is importable/exportable as JSON.
// Part of github.com/hanemile/kscope

const NE = {
  canvas: null, ctx: null,
  nodes: [], edges: [],
  zoom: 1, ox: 0, oy: 0,
  drag: null,
  connect: null,
  pan: false, panStart: null, panOrigin: null,
  hoverNode: null,
};

const NW = 160, NH = 56, PR = 5;

// Dynamic node type registry — extend at runtime
const NODE_TYPES = {
  symbol:   { color: '#00ff7f', ports: { in: [],                             out: [{ id: 'out',    label: 'ref' }] } },
  file:     { color: '#00aaff', ports: { in: [],                             out: [{ id: 'src',    label: 'symbols' }] } },
  function: { color: '#0077cc', ports: { in: [{ id: 'in', label: 'calls' }], out: [{ id: 'out',   label: 'ret' }] } },
  xref:     { color: '#00e5ff', ports: { in: [{ id: 'sym', label: 'sym' }],  out: [{ id: 'usages',label: 'usages' }] } },
  filter:   { color: '#ffe000', ports: { in: [{ id: 'in', label: 'in' }],    out: [{ id: 'out',   label: 'out' }] } },
  custom:   { color: '#555555', ports: { in: [{ id: 'in', label: 'in' }],    out: [{ id: 'out',   label: 'out' }] } },
};

function neNodeColor(kind) {
  return (NODE_TYPES[kind] || NODE_TYPES.custom).color;
}
function neNodePorts(kind) {
  const def = NODE_TYPES[kind] || NODE_TYPES.custom;
  return { in: def.ports.in.map(p => ({ ...p })), out: def.ports.out.map(p => ({ ...p })) };
}

// ── Init ──────────────────────────────────────────────────────────────────────
function initNodeEditor(slot) {
  const canvas = qs('canvas', slot);
  if (!canvas || NE.canvas === canvas) return;
  if (NE.canvas && NE.canvas._neStop) NE.canvas._neStop();

  NE.canvas = canvas;
  canvas.width  = canvas.offsetWidth  || 600;
  canvas.height = canvas.offsetHeight || 400;
  new ResizeObserver(() => {
    canvas.width  = canvas.offsetWidth;
    canvas.height = canvas.offsetHeight;
    neRedraw();
  }).observe(canvas);

  NE.ctx = canvas.getContext('2d');
  neBindEvents(canvas);
  Bus.on('nodeed:addnode', ({ label, kind, file, line }) => {
    neAddNode(label, undefined, undefined, kind || 'symbol', { file, line });
  });
  neRedraw();
}

// ── Add / clear ────────────────────────────────────────────────────────────────
function neAddNode(label, x, y, kind, meta) {
  if (!NE.canvas) return;
  kind = kind || 'custom'; meta = meta || {};
  NE.nodes.push({
    id:     Date.now() + '_' + Math.random().toString(36).slice(2),
    label:  label || ('Node ' + (NE.nodes.length + 1)),
    kind,
    x:      x !== undefined ? x : 80 + Math.random() * (NE.canvas.width  - 240),
    y:      y !== undefined ? y : 60 + Math.random() * (NE.canvas.height - 160),
    ports:  neNodePorts(kind),
    meta,
  });
  neRedraw();
}

function neClear() {
  NE.nodes = []; NE.edges = []; NE.connect = null; neRedraw();
}

async function neLoadSymbols() {
  const d = await api('/search?q=&limit=16&version=' + encodeURIComponent(A.activeVer));
  (d.results || []).slice(0, 12).forEach((s, i) => {
    neAddNode(s.name, 80 + (i % 4) * 200, 70 + Math.floor(i / 4) * 130, s.type || 'symbol', { file: s.file, line: s.line, kind_str: s.kind_str });
  });
}

async function neLoadXRef() {
  const name = prompt('Symbol name for XRef graph:'); if (!name) return;
  const d    = await api('/xrefgraph/' + encodeURIComponent(name) + '?version=' + encodeURIComponent(A.activeVer) + '&depth=1');
  neClear();
  (d.nodes || []).forEach((n, i) => {
    const x = 80 + (i % 5) * 190;
    const y = 60 + Math.floor(i / 5) * 130;
    const k = n.kind === 'file' ? 'file' : n.kind === 'function' ? 'function' : 'symbol';
    neAddNode(n.label, x, y, k, {});
  });
  (d.edges || []).forEach(e => {
    const sn = NE.nodes[e.source]; const tn = NE.nodes[e.target];
    if (sn && tn) {
      NE.edges.push({ from: { nodeId: sn.id, portId: 'out' }, to: { nodeId: tn.id, portId: 'in' }, label: e.label || '' });
    }
  });
  neRedraw();
}

// ── Export / Import ────────────────────────────────────────────────────────────
function neExportJSON() {
  const data = JSON.stringify({ nodes: NE.nodes, edges: NE.edges }, null, 2);
  const blob = new Blob([data], { type: 'application/json' });
  const url  = URL.createObjectURL(blob);
  const a    = document.createElement('a');
  a.href = url; a.download = 'kscope-nodes-' + Date.now() + '.json'; a.click();
  URL.revokeObjectURL(url);
}

function neImportJSON() {
  const inp = document.createElement('input'); inp.type = 'file'; inp.accept = '.json';
  inp.addEventListener('change', () => {
    const f = inp.files[0]; if (!f) return;
    const r = new FileReader();
    r.onload = ev => {
      try {
        const d = JSON.parse(ev.target.result);
        if (d.nodes) NE.nodes = d.nodes;
        if (d.edges) NE.edges = d.edges;
        neRedraw();
      } catch (e) { alert('Import failed: ' + e.message); }
    };
    r.readAsText(f);
  });
  inp.click();
}

// ── Port helpers ───────────────────────────────────────────────────────────────
function nePortPos(node, port, side) {
  const list = side === 'out' ? node.ports.out : node.ports.in;
  const idx  = list.findIndex(p => p.id === port.id);
  return {
    x: node.x + (side === 'out' ? NW : 0),
    y: node.y + NH * (idx + 1) / (list.length + 1),
  };
}

function neHitPort(wx, wy) {
  for (const n of NE.nodes) {
    for (const p of n.ports.out) {
      const pos = nePortPos(n, p, 'out');
      if (Math.hypot(wx - pos.x, wy - pos.y) < PR + 5) return { nodeId: n.id, portId: p.id, side: 'out' };
    }
    for (const p of n.ports.in) {
      const pos = nePortPos(n, p, 'in');
      if (Math.hypot(wx - pos.x, wy - pos.y) < PR + 5) return { nodeId: n.id, portId: p.id, side: 'in' };
    }
  }
  return null;
}

function neHitNode(wx, wy) {
  for (let i = NE.nodes.length - 1; i >= 0; i--) {
    const n = NE.nodes[i];
    if (wx >= n.x && wx <= n.x + NW && wy >= n.y && wy <= n.y + NH) return n;
  }
  return null;
}

// ── Draw ───────────────────────────────────────────────────────────────────────
function neRedraw() {
  const ctx = NE.ctx; if (!ctx) return;
  const canvas = NE.canvas;
  ctx.clearRect(0, 0, canvas.width, canvas.height);
  ctx.save();
  ctx.translate(NE.ox, NE.oy);
  ctx.scale(NE.zoom, NE.zoom);

  // Background dot grid
  const gs = 24;
  const x0 = Math.floor(-NE.ox / NE.zoom / gs) * gs;
  const y0 = Math.floor(-NE.oy / NE.zoom / gs) * gs;
  const x1 = x0 + canvas.width  / NE.zoom + gs * 2;
  const y1 = y0 + canvas.height / NE.zoom + gs * 2;
  ctx.fillStyle = '#111';
  for (let x = x0; x < x1; x += gs) {
    for (let y = y0; y < y1; y += gs) {
      ctx.beginPath(); ctx.arc(x, y, 0.7, 0, Math.PI * 2); ctx.fill();
    }
  }

  // Edges
  NE.edges.forEach(e => {
    const sn = NE.nodes.find(n => n.id === e.from.nodeId);
    const tn = NE.nodes.find(n => n.id === e.to.nodeId);
    if (!sn || !tn) return;
    const sp = nePortPos(sn, sn.ports.out.find(p => p.id === e.from.portId) || sn.ports.out[0], 'out');
    const tp = nePortPos(tn, tn.ports.in.find(p  => p.id === e.to.portId)   || tn.ports.in[0],  'in');
    const cx = (sp.x + tp.x) / 2;
    ctx.beginPath();
    ctx.moveTo(sp.x, sp.y);
    ctx.bezierCurveTo(cx, sp.y, cx, tp.y, tp.x, tp.y);
    ctx.strokeStyle = '#3a3a3a'; ctx.lineWidth = 1.2; ctx.stroke();
    if (e.label) {
      ctx.fillStyle = '#555'; ctx.font = '8px sans-serif';
      ctx.fillText(e.label, (sp.x + tp.x) / 2 - 10, (sp.y + tp.y) / 2 - 3);
    }
  });

  // In-progress connection
  if (NE.connect) {
    const sn = NE.nodes.find(n => n.id === NE.connect.nodeId);
    if (sn) {
      const sp = nePortPos(sn, sn.ports.out.find(p => p.id === NE.connect.portId) || sn.ports.out[0], 'out');
      const cx = (sp.x + NE.connect.mx) / 2;
      ctx.beginPath(); ctx.moveTo(sp.x, sp.y);
      ctx.bezierCurveTo(cx, sp.y, cx, NE.connect.my, NE.connect.mx, NE.connect.my);
      ctx.strokeStyle = 'var(--blue, #00aaff)'; ctx.lineWidth = 1.2;
      ctx.setLineDash([4, 4]); ctx.stroke(); ctx.setLineDash([]);
    }
  }

  // Nodes
  NE.nodes.forEach(node => {
    const col  = neNodeColor(node.kind);
    const isHov = NE.hoverNode === node;
    if (isHov) { ctx.shadowColor = col; ctx.shadowBlur = 10; }

    // Body
    ctx.beginPath();
    neRoundRect(ctx, node.x, node.y, NW, NH, 4);
    ctx.fillStyle = '#0e0e0e'; ctx.fill();
    ctx.strokeStyle = isHov ? col : '#2a2a2a';
    ctx.lineWidth = isHov ? 1.5 : 1; ctx.stroke();
    ctx.shadowBlur = 0;

    // Title bar
    ctx.beginPath();
    neRoundRect(ctx, node.x, node.y, NW, 18, { tl: 4, tr: 4, bl: 0, br: 0 });
    ctx.fillStyle = col + '22'; ctx.fill();

    // Title
    ctx.fillStyle = '#f0f0f0'; ctx.font = 'bold 10px monospace';
    ctx.textBaseline = 'middle';
    ctx.fillText(node.label.slice(0, 22), node.x + 6, node.y + 9);

    // Kind tag
    if (node.kind && node.kind !== 'custom') {
      ctx.font = '700 8px sans-serif'; ctx.fillStyle = col + 'cc';
      const tw = ctx.measureText(node.kind).width;
      ctx.fillText(node.kind.toUpperCase(), node.x + NW - tw - 4, node.y + 9);
    }

    // Input ports
    node.ports.in.forEach(p => {
      const pos = nePortPos(node, p, 'in');
      ctx.beginPath(); ctx.arc(pos.x, pos.y, PR, 0, Math.PI * 2);
      ctx.fillStyle = '#1a1a1a'; ctx.fill();
      ctx.strokeStyle = '#3a3a3a'; ctx.lineWidth = 1; ctx.stroke();
      ctx.fillStyle = '#555'; ctx.font = '8px sans-serif';
      ctx.textAlign = 'left'; ctx.textBaseline = 'middle';
      ctx.fillText(p.label, pos.x + PR + 2, pos.y);
    });

    // Output ports
    node.ports.out.forEach(p => {
      const pos = nePortPos(node, p, 'out');
      ctx.beginPath(); ctx.arc(pos.x, pos.y, PR, 0, Math.PI * 2);
      ctx.fillStyle = col + '44'; ctx.fill();
      ctx.strokeStyle = col; ctx.lineWidth = 1; ctx.stroke();
      ctx.fillStyle = '#888'; ctx.font = '8px sans-serif';
      ctx.textAlign = 'right'; ctx.textBaseline = 'middle';
      ctx.fillText(p.label, pos.x - PR - 2, pos.y);
    });

    ctx.textAlign = 'left'; ctx.textBaseline = 'alphabetic';
  });

  ctx.restore();
}

// Polyfill for roundRect
function neRoundRect(ctx, x, y, w, h, r) {
  if (typeof r === 'number') r = { tl: r, tr: r, bl: r, br: r };
  ctx.beginPath();
  ctx.moveTo(x + r.tl, y);
  ctx.lineTo(x + w - r.tr, y);  ctx.arcTo(x + w, y,     x + w, y + r.tr, r.tr);
  ctx.lineTo(x + w, y + h - r.br); ctx.arcTo(x + w, y + h, x + w - r.br, y + h, r.br);
  ctx.lineTo(x + r.bl, y + h);  ctx.arcTo(x,     y + h, x, y + h - r.bl, r.bl);
  ctx.lineTo(x, y + r.tl);      ctx.arcTo(x,     y,     x + r.tl, y, r.tl);
  ctx.closePath();
}

// ── Events ─────────────────────────────────────────────────────────────────────
function neBindEvents(canvas) {
  const toW = (ex, ey) => ({ x: (ex - NE.ox) / NE.zoom, y: (ey - NE.oy) / NE.zoom });

  canvas.addEventListener('mousedown', e => {
    if (e.button === 2) return;
    const r   = canvas.getBoundingClientRect();
    const w   = toW(e.clientX - r.left, e.clientY - r.top);
    const port = neHitPort(w.x, w.y);
    if (port && port.side === 'out') { NE.connect = { ...port, mx: w.x, my: w.y }; return; }
    const node = neHitNode(w.x, w.y);
    if (node) { NE.drag = { node, offX: w.x - node.x, offY: w.y - node.y }; return; }
    NE.pan = true; NE.panStart = { x: e.clientX, y: e.clientY }; NE.panOrigin = { x: NE.ox, y: NE.oy };
  });

  canvas.addEventListener('mousemove', e => {
    const r = canvas.getBoundingClientRect();
    const w = toW(e.clientX - r.left, e.clientY - r.top);
    if (NE.drag)    { NE.drag.node.x = w.x - NE.drag.offX; NE.drag.node.y = w.y - NE.drag.offY; neRedraw(); return; }
    if (NE.connect) { NE.connect.mx = w.x; NE.connect.my = w.y; neRedraw(); return; }
    if (NE.pan)     { NE.ox = NE.panOrigin.x + (e.clientX - NE.panStart.x); NE.oy = NE.panOrigin.y + (e.clientY - NE.panStart.y); neRedraw(); return; }
    NE.hoverNode = neHitNode(w.x, w.y);
    neRedraw();
  });

  canvas.addEventListener('mouseup', e => {
    const r = canvas.getBoundingClientRect();
    const w = toW(e.clientX - r.left, e.clientY - r.top);
    if (NE.connect) {
      const port = neHitPort(w.x, w.y);
      if (port && port.side === 'in' && port.nodeId !== NE.connect.nodeId) {
        NE.edges.push({ from: { nodeId: NE.connect.nodeId, portId: NE.connect.portId }, to: { nodeId: port.nodeId, portId: port.portId }, label: '' });
      }
      NE.connect = null; neRedraw();
    }
    NE.drag = null; NE.pan = false;
  });

  canvas.addEventListener('dblclick', e => {
    const r    = canvas.getBoundingClientRect();
    const w    = toW(e.clientX - r.left, e.clientY - r.top);
    const node = neHitNode(w.x, w.y);
    if (node) {
      if (node.meta && node.meta.file) openFile(node.meta.file);
      else { const l = prompt('Rename node:', node.label); if (l) { node.label = l; neRedraw(); } }
      return;
    }
    const label = prompt('New node label:');
    if (label) neAddNode(label, w.x, w.y, 'custom');
  });

  canvas.addEventListener('contextmenu', e => {
    e.preventDefault();
    const r    = canvas.getBoundingClientRect();
    const w    = toW(e.clientX - r.left, e.clientY - r.top);
    const node = neHitNode(w.x, w.y);
    if (node) {
      ctxTarget = { neNode: node, sym: node.label, kind: node.kind, file: node.meta?.file || '', line: node.meta?.line || 0 };
      showCtxMenu(e.clientX, e.clientY, [
        { title: node.label },
        '-',
        { label: 'Open file',        action: 'ne-open',        icon: '→'  },
        { label: 'Symbol detail',    action: 'symbol_detail',  icon: 'ℹ'  },
        { label: 'XRef usages',      action: 'ne-xref',        icon: '🔍' },
        { label: 'Send to plugin…',  action: 'send_to_plugin', icon: '⌁'  },
        '-',
        { label: 'Rename',           action: 'ne-rename',      icon: '✎'  },
        { label: 'Delete',           action: 'ne-delete',      icon: '✕'  },
        '-',
        { label: 'Add input port',   action: 'ne-addin'                   },
        { label: 'Add output port',  action: 'ne-addout'                  },
      ]);
    } else {
      ctxTarget = { nePos: { x: w.x, y: w.y } };
      showCtxMenu(e.clientX, e.clientY, [
        { label: 'Add node here', action: 'ne-addhere', icon: '+' },
      ]);
    }
  });

  canvas.addEventListener('wheel', e => {
    e.preventDefault();
    NE.zoom = Math.max(0.1, Math.min(5, NE.zoom * (e.deltaY < 0 ? 1.12 : 0.88)));
    neRedraw();
  }, { passive: false });
}

Bus.on('ctx:action', ({ action, target }) => {
  if (!target) return;
  const node = target.neNode;
  switch (action) {
    case 'ne-open':       if (node && node.meta && node.meta.file) openFile(node.meta.file); break;
    case 'ne-xref':       if (node) openPanelWithState('xref', { sym: node.label }); break;
    case 'symbol_detail': if (target.sym) openSymbolDetail(target.sym); break;
    case 'send_to_plugin':
      if (target.sym) showPluginPopup(
        { sym: target.sym, kind: target.kind || 'unknown', type: '', file: target.file || '', line: target.line || 0 },
        0, 0, true
      );
      break;
    case 'ne-rename':  if (node) { const l = prompt('Rename:', node.label); if (l) { node.label = l; neRedraw(); } } break;
    case 'ne-delete':  if (node) {
      NE.nodes = NE.nodes.filter(n => n.id !== node.id);
      NE.edges = NE.edges.filter(e => e.from.nodeId !== node.id && e.to.nodeId !== node.id);
      neRedraw(); break;
    }
    case 'ne-addin':  if (node) { const l = prompt('Port label:'); if (l) { node.ports.in.push({ id: 'in' + Date.now(), label: l }); neRedraw(); } } break;
    case 'ne-addout': if (node) { const l = prompt('Port label:'); if (l) { node.ports.out.push({ id: 'out' + Date.now(), label: l }); neRedraw(); } } break;
    case 'ne-addhere': { const pos = target.nePos; if (!pos) break; const l = prompt('Node label:'); if (l) neAddNode(l, pos.x, pos.y, 'custom'); break; }
  }
});

// Expose functions used in onclick attributes
window.neAddNode = neAddNode;
window.neClear = neClear;
window.neExportJSON = neExportJSON;
window.neImportJSON = neImportJSON;
window.neLoadSymbols = neLoadSymbols;
window.neLoadXRef = neLoadXRef;
