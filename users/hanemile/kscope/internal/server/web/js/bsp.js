// js/bsp.js — Binary Space Partitioning panel system.
// Every workspace is a BSP tree of splits and leaves.
// Splits can be dragged to resize. Leaves hold a panel (view + state).
// Panels can be split (H or V), closed (merges sibling up), or swapped.
// Part of github.com/hanemile/kscope
'use strict';

// ── BSP node types ────────────────────────────────────────────────────────────
// node = { type:'split', dir:'h'|'v', ratio:0.5, a:node, b:node }
//       | { type:'leaf',  panel:{view,st}, id:number }

let _leafId = 0;
function leafNode(view, st)  { return { type:'leaf',  id: ++_leafId, panel:{view:view||'home', st:st||{}} }; }
function splitNode(dir, a, b, ratio) { return { type:'split', dir, a, b, ratio: ratio||0.5 }; }

// ── Workspace root ────────────────────────────────────────────────────────────
// Each workspace in A.wsList has a { bsp: node } tree.

function wsEnsureBSP(ws) {
  if (!ws.bsp) ws.bsp = leafNode('home', {});
}

// ── DOM rendering ─────────────────────────────────────────────────────────────
const BSP_ROOT = () => $('pgrid');

function bspRender() {
  if (!A.wsList || A.wsList.length === 0) return;
  const w = ws(); wsEnsureBSP(w);
  const root = BSP_ROOT(); if (!root) return;
  root.innerHTML = '';
  root.style.cssText = 'position:relative;width:100%;height:100%;overflow:hidden;background:var(--border)';
  bspRenderNode(w.bsp, root, 0, 0, root.offsetWidth || 800, root.offsetHeight || 600, w);
}

function bspRenderNode(node, container, x, y, w, h, ws) {
  if (node.type === 'leaf') {
    const el = document.createElement('div');
    el.dataset.leafId = node.id;
    el.style.cssText = `position:absolute;left:${x}px;top:${y}px;width:${w}px;height:${h}px;overflow:hidden;background:var(--bg);display:flex;flex-direction:column`;
    el.innerHTML = bspPanelHTML(node);
    container.appendChild(el);
    bspInitLeaf(el, node, ws);
    return;
  }
  // Split node
  if (node.dir === 'h') {
    // Horizontal split: a on left, b on right
    const aw = Math.round(w * node.ratio);
    const bw = w - aw - 2; // 2px divider
    bspRenderNode(node.a, container, x,       y, aw, h, ws);
    bspDivider(container, x + aw, y, 2, h, 'h', node, ws);
    bspRenderNode(node.b, container, x+aw+2, y, bw, h, ws);
  } else {
    // Vertical split: a on top, b on bottom
    const ah = Math.round(h * node.ratio);
    const bh = h - ah - 2;
    bspRenderNode(node.a, container, x, y,       w, ah, ws);
    bspDivider(container, x, y + ah, w, 2, 'v', node, ws);
    bspRenderNode(node.b, container, x, y+ah+2, w, bh, ws);
  }
}

function bspDivider(container, x, y, w, h, dir, node, ws) {
  const el = document.createElement('div');
  el.style.cssText = `position:absolute;left:${x}px;top:${y}px;width:${w}px;height:${h}px;`
    + `background:var(--border);cursor:${dir==='h'?'col-resize':'row-resize'};z-index:10;`
    + `transition:background .1s`;
  el.addEventListener('mouseenter', () => el.style.background = 'var(--blue)');
  el.addEventListener('mouseleave', () => el.style.background = 'var(--border)');

  let dragStart = null, startRatio = null;
  el.addEventListener('mousedown', e => {
    e.preventDefault();
    dragStart  = dir === 'h' ? e.clientX : e.clientY;
    startRatio = node.ratio;
    const root = BSP_ROOT();
    const totalPx = dir === 'h' ? root.offsetWidth : root.offsetHeight;

    const onMove = e => {
      const delta = (dir === 'h' ? e.clientX : e.clientY) - dragStart;
      const newRatio = Math.max(0.1, Math.min(0.9, startRatio + delta / totalPx));
      node.ratio = newRatio;
      bspRender(); // full re-render on drag (fast enough at this scale)
    };
    const onUp = () => {
      document.removeEventListener('mousemove', onMove);
      document.removeEventListener('mouseup',   onUp);
    };
    document.addEventListener('mousemove', onMove);
    document.addEventListener('mouseup',   onUp);
  });
  container.appendChild(el);
}

// ── Panel HTML skeleton (Blender-style) ───────────────────────────────────────
// Top bar: [▾ ViewDropdown] [title/path]  [⇌ ⇅ ↓ ✕]
// Content: the view body (flex:1)
// Bottom bar: context-sensitive controls injected by each view init
function bspPanelHTML(node) {
  const p    = node.panel;
  const view = VIEWS.find(v => v.id === p.view) || VIEWS[0];

  // Build dropdown options — the <select> is the primary view-switcher
  const opts = VIEWS.map(v =>
    `<option value="${v.id}"${v.id === p.view ? ' selected' : ''}>${v.icon} ${v.label}</option>`
  ).join('');

  return `
  <div class="bsp-topbar" data-leaf="${node.id}">
    <div class="bsp-view-pick">
      <select class="bsp-view-sel" onchange="bspSwitchView(${node.id}, this.value)"
              title="Switch panel view">${opts}</select>
    </div>
    <div class="bsp-title" id="bsp-title-${node.id}"></div>
    <div class="bsp-topbar-acts">
      <button class="bsp-act" title="Split horizontally" onclick="bspSplit(${node.id},'h')">⇌</button>
      <button class="bsp-act" title="Split vertically"   onclick="bspSplit(${node.id},'v')">⇅</button>
      <button class="bsp-act" title="Export panel"       onclick="bspExportPanel(${node.id})">↓</button>
      <button class="bsp-act bsp-act-close" title="Close panel" onclick="bspClose(${node.id})">✕</button>
    </div>
  </div>
  <div class="pv-content" style="flex:1;overflow:hidden;display:flex;flex-direction:column">
    ${viewTemplate(p.view)}
  </div>
  <div class="bsp-botbar" id="bsp-bot-${node.id}"></div>`;
}

// ── Leaf init ─────────────────────────────────────────────────────────────────
function bspInitLeaf(el, node, ws) {
  const content = qs('.pv-content', el);
  if (!content) return;
  initSlotView_bsp(content, node.panel.view, node.panel.st, node.id);
}

// initSlotView for BSP — uses leaf id instead of slot index
function initSlotView_bsp(container, view, st, leafId) {
  // Create a fake "slot" proxy that matches what panel functions expect
  const slot = container;
  slot.dataset.slot = leafId;
  initSlotView(slot, view, st);
}

// ── BSP mutations ─────────────────────────────────────────────────────────────

// Find a leaf node and its parent in the tree
function bspFind(root, id, parent, side) {
  if (!root) return null;
  if (root.type === 'leaf' && root.id === id) return { node: root, parent, side };
  if (root.type === 'split') {
    return bspFind(root.a, id, root, 'a') || bspFind(root.b, id, root, 'b');
  }
  return null;
}

function bspLeaf(id) {
  return bspFind(ws().bsp, id);
}

// Switch the view in a leaf
function bspSwitchView(leafId, view) {
  const res = bspLeaf(leafId); if (!res) return;
  res.node.panel.view = view;
  res.node.panel.st   = {};
  bspRender();
}

// Set the title text in a leaf's top bar (called by view inits)
function bspSetTitle(leafId, html) {
  const el = document.getElementById('bsp-title-' + leafId);
  if (el) el.innerHTML = html;
}

// Set the bottom bar content (called by view inits)
function bspSetBot(leafId, html) {
  const el = document.getElementById('bsp-bot-' + leafId);
  if (el) el.innerHTML = html;
}

// Get the leaf id for any element inside a BSP leaf panel.
// Walks up the DOM to find the element with data-leaf-id.
function bspLeafIdFromSlot(el) {
  if (!el) return null;
  // direct hit
  if (el.dataset && el.dataset.slot) return Number(el.dataset.slot);
  if (el.dataset && el.dataset.leafId) return Number(el.dataset.leafId);
  // walk up
  let cur = el.parentElement;
  while (cur) {
    if (cur.dataset && cur.dataset.leafId) return Number(cur.dataset.leafId);
    if (cur.dataset && cur.dataset.slot)   return Number(cur.dataset.slot);
    cur = cur.parentElement;
  }
  return null;
}

// Split a leaf (direction: 'h' = left/right, 'v' = top/bottom)
function bspSplit(leafId, dir) {
  const res = bspLeaf(leafId); if (!res) return;
  const w = ws();
  const newLeaf = leafNode('home', {});
  const newSplit = splitNode(dir, res.node, newLeaf, 0.5);
  if (!res.parent) {
    w.bsp = newSplit;
  } else {
    res.parent[res.side] = newSplit;
  }
  bspRender();
}

// Close a leaf — replace parent split with sibling
function bspClose(leafId) {
  const w = ws(); wsEnsureBSP(w);
  const res = bspLeaf(leafId);
  if (!res) return;
  if (!res.parent) {
    // Only one panel left; reset to home
    w.bsp = leafNode('home', {});
    bspRender(); return;
  }
  // Replace parent with sibling
  const sibling = res.side === 'a' ? res.parent.b : res.parent.a;
  const grandparent = bspFind(w.bsp, res.parent.id || -1);
  // Walk tree to find parent's parent
  const pp = bspFindParentOf(w.bsp, res.parent);
  if (!pp) {
    w.bsp = sibling;
  } else {
    pp.node[pp.side] = sibling;
  }
  bspRender();
}

function bspFindParentOf(root, target, parent, side) {
  if (!root || root.type === 'leaf') return null;
  if (root.a === target) return { node: root, side: 'a' };
  if (root.b === target) return { node: root, side: 'b' };
  return bspFindParentOf(root.a, target) || bspFindParentOf(root.b, target);
}

function bspExportPanel(leafId) {
  const res = bspLeaf(leafId); if (!res) return;
  const data = JSON.stringify({ panel: res.node.panel, versions: A.versions }, null, 2);
  const a    = Object.assign(document.createElement('a'), {
    href: URL.createObjectURL(new Blob([data], { type: 'application/json' })),
    download: 'kscope-panel-' + leafId + '-' + Date.now() + '.json',
  });
  a.click(); URL.revokeObjectURL(a.href);
}

// ── Public helpers for other modules ─────────────────────────────────────────

// Open a view in the first leaf of a given view type, or a new split
function bspOpenView(view, st) {
  const w = ws(); wsEnsureBSP(w);
  const found = bspFindLeafByView(w.bsp, view);
  if (found) {
    found.panel.view = view;
    found.panel.st   = st || {};
    bspRender(); return;
  }
  // Split the first leaf
  const first = bspFirstLeaf(w.bsp);
  if (first) {
    const res = bspLeaf(first.id);
    if (res) {
      const newLeaf = leafNode(view, st || {});
      const newSplit = splitNode('h', res.node, newLeaf, 0.5);
      if (!res.parent) w.bsp = newSplit;
      else res.parent[res.side] = newSplit;
    }
  }
  bspRender();
}

function bspFindLeafByView(node, view) {
  if (!node) return null;
  if (node.type === 'leaf' && node.panel.view === view) return node;
  if (node.type === 'split') return bspFindLeafByView(node.a, view) || bspFindLeafByView(node.b, view);
  return null;
}

function bspFirstLeaf(node) {
  if (!node) return null;
  if (node.type === 'leaf') return node;
  return bspFirstLeaf(node.a);
}

// Override openPanelWithState to use BSP
function openPanelWithState(view, st) {
  bspOpenView(view, st);
}

// Override openFile to use BSP
function openFile(path, forceDiff) {
  if (!path) return;
  if (A.diffMode || forceDiff) {
    bspOpenView('diff', { path, verA: A.activeVer, verB: A.diffVerB });
  } else {
    bspOpenView('source', { path, ver: A.activeVer });
  }
}

// Re-render BSP on workspace switch / resize
function renderGrid() { bspRender(); }


// Expose BSP functions used in onclick attributes
window.bspSwitchView  = bspSwitchView;
window.bspSetTitle    = bspSetTitle;
window.bspSetBot      = bspSetBot;
window.bspLeafIdFromSlot = bspLeafIdFromSlot;
window.bspSplit       = bspSplit;
window.bspClose       = bspClose;
window.bspExportPanel = bspExportPanel;
window.bspRender      = bspRender;

window.addEventListener('resize', () => { if (ws()) bspRender(); });

// Expose functions used in onclick attributes
window.openFile = openFile;
window.openPanelWithState = openPanelWithState;
