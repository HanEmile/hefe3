// js/core.js — Core application logic: state, event bus, SSE client,
// workspace/layout management, file tree, search, and context menu.
// Part of github.com/hanemile/kscope
'use strict';

// ── Utilities ─────────────────────────────────────────────────────────────────
const $  = id => document.getElementById(id);
const qs = (sel, el = document) => el.querySelector(sel);
const qa = (sel, el = document) => [...el.querySelectorAll(sel)];
const esc = s => String(s)
  .replace(/&/g, '&amp;')
  .replace(/</g, '&lt;')
  .replace(/>/g, '&gt;')
  .replace(/"/g, '&quot;');

async function api(path, opts) {
  const r = await fetch('/api' + path, opts);
  if (!r.ok) throw new Error(await r.text());
  return r.json();
}
async function apiPost(path, body) {
  return api(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

// ── Internal event bus ────────────────────────────────────────────────────────
const Bus = (() => {
  const subs = {};
  return {
    on(ev, fn)   { (subs[ev] = subs[ev] || []).push(fn); },
    off(ev, fn)  { if (subs[ev]) subs[ev] = subs[ev].filter(f => f !== fn); },
    emit(ev, d)  { (subs[ev] || []).forEach(fn => { try { fn(d); } catch(e) { console.error(ev, e); } }); },
  };
})();

// ── Application state ─────────────────────────────────────────────────────────
const A = {
  versions:  [],   // [{label, root, git_tag, indexed, symbols, files, xrefs}]
  activeVer: '',
  diffMode:  false,
  diffVerB:  '',
  jobs:      [],
  progress:  {},   // label → IndexProgressPayload
  wsList:    [],
  wsIdx:     0,
};
const ws    = () => A.wsList[A.wsIdx];
const curVS = () => A.versions.find(v => v.label === A.activeVer) || A.versions[0];

// ── SSE client ────────────────────────────────────────────────────────────────
// ── Loading overlay ───────────────────────────────────────────────────────────
// Module-level so both the SSE handler and bootstrap can call it directly.

function updateLoadingOverlay() {
  const ov    = document.getElementById('loading');
  if (!ov || ov.classList.contains('fade')) return;

  const title  = document.getElementById('load-title');
  const detail = document.getElementById('load-detail');
  const jobs   = document.getElementById('load-jobs');

  const entries = Object.entries(A.progress);

  if (entries.length === 0) {
    if (title) title.textContent = 'KScope starting…';
    return;
  }

  const allDone = entries.every(([, p]) => p.phase === 'done' || p.phase === 'error');
  if (title) title.textContent = allDone ? 'Index complete' : 'Indexing kernel source…';

  if (jobs) {
    jobs.innerHTML = entries.map(([label, p]) => {
      const pct  = Math.min(100, Math.round(p.pct || 0));
      const syms = p.symbols ? p.symbols.toLocaleString() + ' sym' : '';
      const xrefs = p.xrefs  ? ', ' + p.xrefs.toLocaleString() + ' xrefs' : '';
      const files = (p.files && p.total)
        ? p.files.toLocaleString() + ' / ' + p.total.toLocaleString() + ' files'
        : '';
      const elapsed = p.elapsed_s ? ' (' + p.elapsed_s.toFixed(1) + 's)' : '';

      let stat;
      switch (p.phase) {
        case 'done':  stat = '✓ ' + syms + xrefs + elapsed; break;
        case 'error': stat = '✗ ' + (p.message || 'error');  break;
        case 'dedup': stat = 'dedup…' + elapsed;              break;
        case 'walk':  stat = 'walking…';                       break;
        default:      stat = pct + '% — ' + files + elapsed;
      }

      const barColor = p.phase === 'done'  ? 'var(--green)'
                     : p.phase === 'error' ? 'var(--red)'
                     : p.phase === 'dedup' ? 'var(--yellow)'
                     : 'var(--blue)';

      return '<div class="load-job-row">'
        + '<span class="load-job-label">' + esc(label) + '</span>'
        + '<div class="load-job-bar">'
        +   '<div class="load-job-fill" style="width:' + pct + '%;background:' + barColor + '"></div>'
        + '</div>'
        + '<span class="load-job-pct">' + esc(stat) + '</span>'
        + '</div>';
    }).join('');
  }

  if (detail) {
    const totalFiles = entries.reduce((s, [,p]) => s + (p.total || 0), 0);
    const doneFiles  = entries.reduce((s, [,p]) => s + (p.files || 0), 0);
    if (totalFiles > 0 && !entries.every(([,p]) => p.phase === 'done')) {
      detail.textContent = 'Parsed ' + doneFiles.toLocaleString()
        + ' of ' + totalFiles.toLocaleString() + ' source files. '
        + 'Search and navigation available now.';
    } else {
      detail.textContent = 'Search and navigation ready.';
    }
  }

  if (allDone) hideLoadingOverlay();
}

function hideLoadingOverlay() {
  const ov = document.getElementById('loading');
  if (!ov || ov.classList.contains('fade')) return;
  ov.classList.add('fade');
  setTimeout(() => { if (ov.parentNode) ov.parentNode.removeChild(ov); }, 280);
}
// Expose explicitly on window so inline onclick attributes can reach it
window.hideLoadingOverlay = hideLoadingOverlay;
window.openIndexDirModal  = openIndexDirModal;
window.indexSubDir        = indexSubDir;

function connectSSE() {
  const es = new EventSource('/api/events');

  es.addEventListener('index.progress', ev => {
    const msg = JSON.parse(ev.data);
    // Msg envelope: payload is a JSON string
    let p;
    try { p = typeof msg.payload === 'string' ? JSON.parse(msg.payload) : msg.payload || msg; }
    catch(_) { p = msg; }
    A.progress[p.label] = p;
    const ver = A.versions.find(v => v.label === p.label);
    if (ver && p.phase === 'done') { ver.indexed = true; renderVerSelect(); }
    renderSbFoot();
    updatePbar();
    updateStatusBar();
    updateLoadingOverlay();
    if (p.phase === 'done' || p.phase === 'error') Bus.emit('index:done', p);
    else Bus.emit('index:progress', p);
  });

  ['job.queued','job.running','job.done','job.error','job.cancelled'].forEach(k => {
    es.addEventListener(k, ev => {
      const msg = JSON.parse(ev.data);
      let p;
      try { p = typeof msg.payload === 'string' ? JSON.parse(msg.payload) : msg.payload || msg; }
      catch(_) { p = msg; }
      const idx = A.jobs.findIndex(j => j.job_id === p.job_id);
      if (idx >= 0) A.jobs[idx] = p; else A.jobs.unshift(p);
      if (A.jobs.length > 200) A.jobs.length = 200;
      Bus.emit('jobs:update', A.jobs);
    });
  });

  es.addEventListener('jobs', ev => {
    A.jobs = JSON.parse(ev.data);
    Bus.emit('jobs:update', A.jobs);
  });

  es.onerror = () => setTimeout(connectSSE, 3000);
}

// ── Progress bar ──────────────────────────────────────────────────────────────
function updatePbar() {
  const bar = $('progress-fill');
  if (!bar) return;
  const active = Object.values(A.progress).filter(p => p.phase === 'parse' || p.phase === 'walk' || p.phase === 'dedup');
  if (!active.length) {
    const allDone = Object.values(A.progress).every(p => p.phase === 'done' || p.phase === 'error');
    if (allDone) {
      bar.classList.remove('pulse');
      bar.style.width = '100%';
      setTimeout(() => { bar.style.width = '0%'; }, 800);
    }
    return;
  }
  const avg = active.reduce((s, p) => s + (p.pct || 0), 0) / active.length;
  if (avg < 1) { bar.classList.add('pulse'); }
  else { bar.classList.remove('pulse'); bar.style.width = avg + '%'; }
}

// ── Status bar ────────────────────────────────────────────────────────────────
function updateStatusBar() {
  const bar = $('status-bar');
  if (!bar) return;
  bar.innerHTML = A.versions.map(v => {
    const p = A.progress[v.label] || { phase: v.indexed ? 'done' : 'indexing', pct: v.indexed ? 100 : 0 };
    const stat = p.phase === 'done'
      ? (v.symbols || 0).toLocaleString() + ' sym'
      : p.phase === 'parse' ? Math.round(p.pct || 0) + '%' : p.phase;
    return `<div class="isb-dot ${p.phase === 'done' ? 'done' : p.phase === 'error' ? 'error' : 'indexing'}"></div>
            <span>${esc(v.label)}: ${stat}</span>`;
  }).join('<span style="color:var(--border2);margin:0 4px">|</span>');
}

// ── Version select ─────────────────────────────────────────────────────────────
function renderVerSelect() {
  const sel = $('ver-sel');
  if (!sel) return;
  const cur = sel.value || A.activeVer;
  sel.innerHTML = A.versions.map(v =>
    `<option value="${esc(v.label)}" ${v.label === cur ? 'selected' : ''}>${esc(v.label)}${v.indexed ? '' : ' ⋯'}</option>`
  ).join('');
  if (cur) sel.value = cur;
}

$('ver-sel')?.addEventListener('change', e => {
  A.activeVer = e.target.value;
  pStk = ['.'];
  loadTree('.');
  renderSbFoot();
  wsRenderBar();
});

// ── Git modal ──────────────────────────────────────────────────────────────────
$('btn-git')?.addEventListener('click', () => Bus.emit('modal:git'));

Bus.on('modal:git', async () => {
  let tags = [], branches = [], head = '';
  try {
    const d = await api('/git/tags');
    tags = d.tags || []; branches = d.branches || []; head = d.head || '';
  } catch (_) { /* git may not be available */ }

  showModal(`
    <h3>Switch Git Version</h3>
    <label>Current HEAD</label>
    <div style="font:11px var(--fm);color:var(--text2);padding:3px 0">${esc(head || 'unknown')}</div>
    <label>Tag</label>
    <select id="m-tag" style="width:100%;height:26px;margin-bottom:4px">
      <option value="">— select tag —</option>
      ${tags.map(t => `<option value="${esc(t)}">${esc(t)}</option>`).join('')}
    </select>
    <label>Branch</label>
    <select id="m-branch" style="width:100%;height:26px;margin-bottom:4px">
      <option value="">— select branch —</option>
      ${branches.map(b => `<option value="${esc(b)}">${esc(b)}</option>`).join('')}
    </select>
    <label>Or type manually</label>
    <input id="m-ref" type="text" placeholder="v6.9 / main / abc1234">
    <label>Label (optional — defaults to tag name)</label>
    <input id="m-label" type="text" placeholder="auto">
    <div class="modal-btns">
      <button class="btn-cancel" onclick="closeModal()">Cancel</button>
      <button class="btn-ok" onclick="doGitCheckout()">Checkout &amp; Index</button>
    </div>
  `);
});

async function doGitCheckout() {
  const tag   = $('m-tag')?.value || $('m-branch')?.value || $('m-ref')?.value?.trim();
  const label = $('m-label')?.value?.trim() || '';
  if (!tag) return;
  closeModal();
  try {
    const d = await apiPost('/git/checkout', { tag, label });
    A.activeVer = d.label;
    if (!A.versions.find(v => v.label === d.label)) {
      A.versions.push({ label: d.label, root: '', git_tag: tag, indexed: false });
    }
    renderVerSelect();
  } catch (e) {
    alert('Checkout failed: ' + e.message);
  }
}

// ── Diff mode ──────────────────────────────────────────────────────────────────
$('btn-diff')?.addEventListener('click', () => {
  A.diffMode = !A.diffMode;
  $('btn-diff').classList.toggle('on', A.diffMode);
  $('btn-diff').textContent = A.diffMode ? '⇆ Diff on' : '⇆ Diff';
  if (A.diffMode && A.versions.length > 1) {
    A.diffVerB = A.versions.find(v => v.label !== A.activeVer)?.label || '';
  }
});

// ── Modal ──────────────────────────────────────────────────────────────────────
function showModal(html) {
  $('modal-content').innerHTML = html;
  $('modal-bg').classList.add('open');
}
function closeModal() { $('modal-bg').classList.remove('open'); }
$('modal-bg')?.addEventListener('click', e => { if (e.target === $('modal-bg')) closeModal(); });

// ── Context menu ───────────────────────────────────────────────────────────────
const CTX = $('ctx-menu');
let ctxTarget = null;

function showCtxMenu(x, y, items) {
  CTX.innerHTML = items.map(it => {
    if (it === '-') return '<div class="ctx-sep"></div>';
    if (it.title) return `<div class="ctx-title">${esc(it.title)}</div>`;
    return `<div class="ctx-item" data-action="${esc(it.action || '')}">`
      + (it.icon ? `<span>${it.icon}</span>` : '')
      + `<span>${esc(it.label)}</span>`
      + (it.hint ? `<span style="margin-left:auto;color:var(--text4);font-size:9px">${esc(it.hint)}</span>` : '')
      + '</div>';
  }).join('');
  CTX.style.display = 'block';
  const maxX = window.innerWidth  - 180;
  const maxY = window.innerHeight - CTX.offsetHeight - 10;
  CTX.style.left = Math.min(x, maxX) + 'px';
  CTX.style.top  = Math.min(y, maxY) + 'px';
}
function hideCtxMenu() { CTX.style.display = 'none'; }

CTX.addEventListener('click', e => {
  const item = e.target.closest('.ctx-item');
  if (!item) return;
  hideCtxMenu();
  Bus.emit('ctx:action', { action: item.dataset.action, target: ctxTarget });
});
document.addEventListener('click', () => hideCtxMenu());
document.addEventListener('keydown', e => {
  if (e.key === 'Escape') { hideCtxMenu(); closeModal(); }
});

// Symbol right-click anywhere in the document
document.addEventListener('contextmenu', e => {
  const link = e.target.closest('.sym-link');
  if (!link) return;
  e.preventDefault();
  const sym  = link.dataset.sym;
  const file = link.dataset.file || '';
  const line = Number(link.dataset.line) || 0;
  ctxTarget = { sym, file, line };
  showCtxMenu(e.clientX, e.clientY, [
    { title: sym },
    '-',
    { label: 'XRef: find usages',      action: 'xref',            icon: '🔍' },
    { label: 'XRef graph',             action: 'xrefgraph',       icon: '◎'  },
    { label: 'Open definition',        action: 'opendef',         icon: '→'  },
    { label: 'Open in diff view',      action: 'opendiff',        icon: '⇆'  },
    '-',
    { label: 'Send to plugin…',        action: 'send_to_plugin',  icon: '⌁'  },
    { label: 'Add to node editor',     action: 'addnode',         icon: '◆'  },
    { label: 'Send to search',         action: 'tosearch',        icon: '⌕'  },
    { label: 'Copy name',              action: 'copyname',        icon: '📋' },
  ]);
});

// Also handle left-click on sym-link per settings (default: open context menu)
document.addEventListener('click', e => {
  const link = e.target.closest('.sym-link');
  if (!link || e.button !== 0) return;
  e.stopPropagation();
  const sym  = link.dataset.sym;
  const file = link.dataset.file || '';
  const line = Number(link.dataset.line) || 0;
  ctxTarget = { sym, file, line };
  showCtxMenu(e.clientX, e.clientY, [
    { title: sym },
    '-',
    { label: 'XRef: find usages',  action: 'xref',           icon: '🔍' },
    { label: 'XRef graph',         action: 'xrefgraph',      icon: '◎'  },
    { label: 'Open definition',    action: 'opendef',        icon: '→'  },
    { label: 'Open in diff',       action: 'opendiff',       icon: '⇆'  },
    '-',
    { label: 'Send to plugin…',    action: 'send_to_plugin', icon: '⌁'  },
    { label: 'Add to node editor', action: 'addnode',        icon: '◆'  },
    { label: 'Copy name',          action: 'copyname',       icon: '📋' },
  ]);
});

Bus.on('ctx:action', ({ action, target }) => {
  if (!target) return;
  const { sym, file, line } = target;
  switch (action) {
    case 'xref':            openPanelWithState('xref',    { sym });               break;
    case 'xrefgraph':       openPanelWithState('graph',   { mode: 'xref', sym }); break;
    case 'opendef':         if (file) openFile(file);                              break;
    case 'opendiff':        if (file) openFile(file, true);                        break;
    case 'addnode':         Bus.emit('nodeed:addnode', { label: sym, kind: target.kind || 'symbol' }); break;
    case 'tosearch':        $('srch').value = sym; $('srch').dispatchEvent(new Event('input')); break;
    case 'copyname':        navigator.clipboard?.writeText(sym);                   break;
    case 'symbol_detail':   openSymbolDetail(sym);                                 break;
    case 'send_to_plugin':
      if (sym) showPluginPopup(
        { sym, kind: target.kind || 'unknown', type: target.type || '', file: file || '', line: line || 0 },
        0, 0, true
      );
      break;
  }
});

// ── Workspace management ──────────────────────────────────────────────────────
function wsCreate(name = 'Workspace') {
  const w = { id: Date.now(), name, layout: 'l1', panels: [{ view: 'home', st: {} }], bsp: null };
  A.wsList.push(w);
  return w;
}

function wsSwitch(i) {
  A.wsIdx = i;
  wsRenderBar();
  renderGrid();
}

function wsRenderBar() {
  const container = $('ws-tabs');
  if (!container) return;
  container.innerHTML = '';
  A.wsList.forEach((w, i) => {
    const t = document.createElement('div');
    t.className = 'ws-tab' + (i === A.wsIdx ? ' active' : '');
    t.innerHTML = `<span class="ws-tab-name">${esc(w.name)}</span><span class="ws-x" data-i="${i}">✕</span>`;
    t.addEventListener('click', e => {
      if (e.target.closest('.ws-x')) {
        if (A.wsList.length > 1) {
          A.wsList.splice(i, 1);
          if (A.wsIdx >= A.wsList.length) A.wsIdx = A.wsList.length - 1;
          wsRenderBar(); renderGrid();
        }
      } else wsSwitch(i);
    });
    container.appendChild(t);
  });
}

$('ws-new')?.addEventListener('click', () => {
  wsCreate('WS ' + (A.wsList.length + 1));
  A.wsIdx = A.wsList.length - 1;
  wsRenderBar(); renderGrid();
});

// ── Layout ────────────────────────────────────────────────────────────────────
const LAYOUTS = [
  { id: 'l1',  count: 1, gridClass: '',    icon: '<rect x="1" y="1" width="8" height="8" rx="1"/>' },
  { id: 'l2c', count: 2, gridClass: 'l2c', icon: '<rect x="1" y="1" width="3.5" height="8" rx="1"/><rect x="5.5" y="1" width="3.5" height="8" rx="1"/>' },
  { id: 'l2r', count: 2, gridClass: 'l2r', icon: '<rect x="1" y="1" width="8" height="3.5" rx="1"/><rect x="1" y="5.5" width="8" height="3.5" rx="1"/>' },
  { id: 'l4',  count: 4, gridClass: 'l4',  icon: '<rect x="1" y="1" width="3.5" height="3.5" rx="1"/><rect x="5.5" y="1" width="3.5" height="3.5" rx="1"/><rect x="1" y="5.5" width="3.5" height="3.5" rx="1"/><rect x="5.5" y="5.5" width="3.5" height="3.5" rx="1"/>' },
];

function renderLayoutBtns() {
  const c = $('layout-btns');
  if (!c) return;
  c.innerHTML = '';
  LAYOUTS.forEach(l => {
    const b = document.createElement('button');
    b.className = 'lp' + (ws().layout === l.id ? ' on' : '');
    b.dataset.l = l.id;
    b.title = l.id;
    b.innerHTML = `<svg width="10" height="10" viewBox="0 0 10 10" fill="currentColor">${l.icon}</svg>`;
    b.addEventListener('click', () => setLayout(l.id));
    c.appendChild(b);
  });
}

function setLayout(id) {
  const w = ws();
  w.layout = id;
  const def = LAYOUTS.find(l => l.id === id) || LAYOUTS[0];
  while (w.panels.length < def.count) w.panels.push({ view: 'home', st: {} });
  w.panels = w.panels.slice(0, def.count);
  renderLayoutBtns();
  renderGrid();
}

// ── Panel grid ────────────────────────────────────────────────────────────────
const VIEWS = [
  { id: 'home',       label: 'Home',       icon: '⌂'  },
  { id: 'source',     label: 'Source',     icon: '⟨⟩' },
  { id: 'symbols',    label: 'Symbols',    icon: '⊞'  },
  { id: 'diff',       label: 'Diff',       icon: '⇆'  },
  { id: 'dup',        label: 'Dups',       icon: '⑂'  },
  { id: 'graph',      label: 'Graph',      icon: '◎'  },
  { id: 'callgraph',  label: 'CallGraph',  icon: '↕'  },
  { id: 'xref',       label: 'XRef',       icon: '↔'  },
  { id: 'structview', label: 'Struct',     icon: '▦'  },
  { id: 'nodeed',     label: 'Nodes',      icon: '◆'  },
  { id: 'jobs',       label: 'Jobs',       icon: '⚙'  },
  { id: 'plugins',    label: 'Plugins',    icon: '⌁'  },
];

function renderGrid() {
  // Delegates to bsp.js when loaded (bsp.js script comes after core.js in index.html)
  if (typeof bspRender === 'function') { bspRender(); return; }
  // Legacy grid fallback (used only if bsp.js is absent)
  const w    = ws();
  const grid = $('pgrid');
  if (!grid) return;
  const def  = LAYOUTS.find(l => l.id === w.layout) || LAYOUTS[0];
  grid.className = def.gridClass;
  grid.innerHTML = '';
  w.panels.forEach((p, i) => {
    const slot = buildSlot(p, i);
    grid.appendChild(slot);
    initSlotView(slot, p.view, p.st);
  });
  renderLayoutBtns();
}

function buildSlot(p, idx) {
  const slot = document.createElement('div');
  slot.className = 'pslot'; slot.dataset.slot = idx;

  const hd   = document.createElement('div'); hd.className = 'phd';
  const tabs = document.createElement('div'); tabs.className = 'pvtabs';
  VIEWS.forEach(v => {
    const b = document.createElement('button');
    b.className = 'pvt' + (v.id === p.view ? ' active' : '');
    b.dataset.v = v.id;
    b.textContent = v.icon + ' ' + v.label;
    b.addEventListener('click', () => switchSlotView(idx, v.id));
    tabs.appendChild(b);
  });
  hd.appendChild(tabs);

  const acts = document.createElement('div'); acts.className = 'phd-acts';
  const exp  = document.createElement('button');
  exp.className = 'ic-btn'; exp.title = 'Export panel data'; exp.textContent = '↓';
  exp.addEventListener('click', () => exportPanel(idx));
  acts.appendChild(exp);
  if (ws().panels.length > 1) {
    const cx = document.createElement('button');
    cx.className = 'phd-x'; cx.title = 'Close panel'; cx.textContent = '✕';
    cx.addEventListener('click', () => closePanel(idx));
    acts.appendChild(cx);
  }
  hd.appendChild(acts);
  slot.appendChild(hd);

  VIEWS.forEach(v => {
    const d = document.createElement('div');
    d.className = 'pv' + (v.id === p.view ? ' on' : '');
    d.dataset.v = v.id;
    d.innerHTML = viewTemplate(v.id);
    slot.appendChild(d);
  });
  return slot;
}

function switchSlotView(idx, view) {
  const w = ws(); w.panels[idx].view = view;
  const slot = $('pgrid').querySelector(`[data-slot="${idx}"]`);
  if (!slot) return;
  qa('.pvt',  slot).forEach(b => b.classList.toggle('active', b.dataset.v === view));
  qa('.pv',   slot).forEach(d => {
    d.classList.toggle('on', d.dataset.v === view);
    if (d.dataset.v === view) initSlotView(slot, view, w.panels[idx].st);
  });
}

function closePanel(idx) {
  ws().panels.splice(idx, 1);
  const cnt = ws().panels.length;
  ws().layout = cnt === 1 ? 'l1' : cnt === 2 ? 'l2c' : 'l4';
  renderGrid();
}

function exportPanel(idx) {
  const p    = ws().panels[idx];
  const data = JSON.stringify({ panel: p, versions: A.versions, jobs: A.jobs }, null, 2);
  const blob = new Blob([data], { type: 'application/json' });
  const url  = URL.createObjectURL(blob);
  const a    = document.createElement('a');
  a.href = url; a.download = `kscope-panel-${idx}-${Date.now()}.json`; a.click();
  URL.revokeObjectURL(url);
}

function openPanelWithState(view, st) {
  const w = ws();
  let idx = w.panels.findIndex(p => p.view === view);
  if (idx < 0) idx = 0;
  w.panels[idx].view = view;
  w.panels[idx].st   = st;
  renderGrid();
}

function openFile(path, forceDiff = false) {
  if (!path) return;
  const w = ws();
  if (A.diffMode || forceDiff) {
    let idx = w.panels.findIndex(p => p.view === 'diff');
    if (idx < 0) idx = 0;
    w.panels[idx].view = 'diff';
    w.panels[idx].st   = { path, verA: A.activeVer, verB: A.diffVerB };
  } else {
    let idx = w.panels.findIndex(p => p.view === 'source');
    if (idx < 0) idx = 0;
    w.panels[idx].view = 'source';
    w.panels[idx].st   = { path, ver: A.activeVer };
  }
  renderGrid();
}

// ── View templates ────────────────────────────────────────────────────────────
function viewTemplate(v) {
  switch (v) {
    case 'home': return `
      <div class="hv-body">
        <div class="hv-logo">KScope</div>
        <div class="hv-sub">Linux kernel source browser — kernel/UAPI annotation, duplicate detection, cross-reference graph, node editor, git version switching.</div>
        <div class="hv-grid">
          ${[
            ['🟢 Kernel','Outside include/uapi/ — may change','source'],
            ['🟡 UAPI','Stable ABI boundary — must not break userspace','source'],
            ['🔴 Dups','Same struct name in kernel + uapi — ABI hazard','dup'],
            ['🔍 XRef','Trace symbol usages across the tree','xref'],
            ['◎ Graph','Symbol relationship graph','graph'],
            ['◆ Nodes','Visual node editor for analysis','nodeed'],
            ['⇆ Diff','Side-by-side git tag comparison','diff'],
            ['⚙ Jobs','Background indexing job queue','jobs'],
          ].map(([h,p,view]) => `<div class="hv-card" onclick="openPanelWithState('${view}',{})"><h4>${h}</h4><p>${p}</p></div>`).join('')}
        </div>
      </div>`;
    case 'source': return `
      <div class="sv-body">
        <div class="sv-scroll"><div style="padding:14px;color:var(--text4);font-size:10px">Open a file from the explorer →</div></div>
        <div class="sv-syms">
          <div class="svs-hd"><span>Symbols</span><span class="sym-count" style="color:var(--text4);margin-left:4px"></span></div>
        </div>
      </div>`;
    case 'diff': return `
      <div class="dv-body">
        <div class="dv-side">
          <div class="dv-hd"><span class="dv-ver">A</span><select class="dv-ver-sel ver-sel" style="height:18px;font-size:10px"></select></div>
          <div class="dv-scroll"></div>
        </div>
        <div class="dv-side">
          <div class="dv-hd"><span class="dv-ver">B</span><select class="dv-ver-sel ver-sel" style="height:18px;font-size:10px"></select></div>
          <div class="dv-scroll"></div>
        </div>
      </div>`;
    case 'dup': return `
      <div class="dup-body">
        <div class="dup-toolbar">
          <select class="dup-filter-sel" style="height:22px;font-size:10px">
            <option value="">All risks</option>
            <option>high</option><option>medium</option><option>low</option>
          </select>
          <input class="dup-search" type="text" placeholder="Filter name / path…" style="flex:1;height:22px">
        </div>
        <div class="dup-grid"></div>
      </div>`;
    case 'graph': return `
      <div class="gv-body">
        <canvas></canvas>
        <div class="g-hud">
          <button class="g-btn" onclick="gReset()">⟳</button>
          <button class="g-btn" onclick="gZoom(1.2)">+</button>
          <button class="g-btn" onclick="gZoom(.8)">−</button>
          <button class="g-btn" id="g-dup-btn" onclick="gToggleDup()">Dups</button>
          <button class="g-btn" onclick="gExport()">↓ SVG</button>
        </div>
        <div class="g-legend">
          ${[['--green','kernel'],['--yellow','uapi'],['--red','dup'],['--blue','file'],['--purple','exported'],['--cyan','syscall'],['--text3','function']].map(([c,l]) => `<div class="gl-r"><div class="gl-d" style="background:var(${c})"></div>${l}</div>`).join('')}
        </div>
        <div class="g-tip"></div>
        <div class="g-info">Loading graph…</div>
      </div>`;
    case 'xref': return `
      <div class="xv-body">
        <div class="xv-toolbar">
          <input class="xv-input" type="text" placeholder="Symbol name…">
          <button class="xv-go" onclick="xvSearch(this)">XRef</button>
          <button class="xv-go" style="background:var(--purple)" onclick="xvGraph(this)">Graph</button>
        </div>
        <div class="xv-list"></div>
      </div>`;
    case 'nodeed': return `
      <div class="ne-body">
        <canvas></canvas>
        <div class="ne-tb-row">
          <button class="ne-tb" onclick="neAddNode()">+ Node</button>
          <button class="ne-tb" onclick="neLoadSymbols()">Load symbols</button>
          <button class="ne-tb" onclick="neLoadXRef()">Load XRef</button>
          <button class="ne-tb" onclick="neClear()">Clear</button>
          <button class="ne-tb" onclick="neExportJSON()">↓ JSON</button>
          <button class="ne-tb" onclick="neImportJSON()">↑ Import</button>
        </div>
        <div class="ne-help">Drag=move · Port click=connect · DblClick=create/open · RClick=delete · Scroll=zoom</div>
      </div>`;
    case 'symbols': return `
      <div style="display:flex;flex-direction:column;height:100%;overflow:hidden">
        <div class="sym-browser-toolbar">
          <select class="sb-filter-kind" style="height:22px;font-size:10px">
            <option value="">All kinds</option>
            <option value="kernel">kernel</option>
            <option value="uapi">uapi</option>
            <option value="exported">exported</option>
            <option value="syscall">syscall</option>
            <option value="duplicated">duplicated</option>
          </select>
          <select class="sb-filter-type" style="height:22px;font-size:10px">
            <option value="">All types</option>
            <option value="struct">struct</option>
            <option value="enum">enum</option>
            <option value="func">func</option>
            <option value="macro">macro</option>
            <option value="typedef">typedef</option>
          </select>
          <input class="sb-filter-q" type="text" placeholder="Filter name…" style="flex:1;height:22px">
          <select class="sb-group-by" style="height:22px;font-size:10px">
            <option value="kind">Group: kind</option>
            <option value="type">Group: type</option>
            <option value="file">Group: dir</option>
            <option value="none">No grouping</option>
          </select>
          <select class="sb-sort-by" style="height:22px;font-size:10px">
            <option value="name">Sort: name</option>
            <option value="xrefs">Sort: usage</option>
            <option value="file">Sort: file</option>
          </select>
          <span class="sb-count"></span>
        </div>
        <div class="sym-browser-body"></div>
      </div>`;
    case 'jobs': return `
      <div class="jobs-body">
        <div class="jobs-toolbar">
          <button class="g-btn" onclick="cancelAllJobs()">Cancel all</button>
          <button class="g-btn" onclick="clearDoneJobs()">Clear done</button>
        </div>
        <div class="jobs-list"></div>
      </div>`;
    case 'plugins': return `<div class="plug-body"><div style="display:flex;align-items:center;gap:5px;padding:5px;border-bottom:1px solid var(--border)"><span style="font-size:10px;color:var(--text3);flex:1">Loaded plugins</span><button class="pc-reload-btn" onclick="reloadPlugins()">⟳ Reload</button></div><div class="plug-grid"></div></div>`;
    case 'callgraph': return `
      <div class="cg-body">
        <div class="cg-toolbar">
          <input class="cg-sym" type="text" placeholder="Function name…">
          <input class="cg-depth" type="number" value="2" min="1" max="5" title="Depth" style="width:40px">
          <button class="cg-go">Show</button>
          <button class="cg-graph">◎ Graph</button>
        </div>
        <div class="cg-list"></div>
      </div>`;
    case 'structview': return `
      <div class="sv2-body">
        <div class="sv2-toolbar">
          <input class="sv2-sym" type="text" placeholder="Struct name (e.g. iovec)…">
          <button class="sv2-go">Visualize</button>
        </div>
        <canvas class="sv2-canvas"></canvas>
      </div>`;
    default: return '';
  }
}

// ── initSlotView ──────────────────────────────────────────────────────────────
function initSlotView(slot, view, st) {
  switch (view) {
    case 'source':  if (st.path) loadSrc(slot, st.path, st.ver); break;
    case 'diff':    loadDiffSlot(slot, st); break;
    case 'dup':     loadDupsSlot(slot); break;
    case 'graph':   initGraphSlot(slot, st); break;
    case 'xref': {
      if (st.sym) {
        const inp = qs('.xv-input', slot);
        if (inp) { inp.value = st.sym; xvSearch(qs('.xv-go', slot)); }
      }
      break;
    }
    case 'symbols':    initSymbolsSlot(slot, st); break;
    case 'callgraph':  initCallgraphSlot(slot, st); break;
    case 'structview': initStructviewSlot(slot, st); break;
    case 'jobs':    initJobsSlot(slot); break;
    case 'plugins': initPluginsSlot(slot); break;
    case 'nodeed':  initNodeEditor(slot); break;
  }
}

// ── File tree ──────────────────────────────────────────────────────────────────
let pStk = ['.'];

async function loadTree(path) {
  const data = await api('/tree?path=' + encodeURIComponent(path) + '&version=' + encodeURIComponent(A.activeVer));
  const tree = $('ftree'); if (!tree) return;
  tree.innerHTML = '';
  $('sb-bc').textContent = path === '.' ? '/' : '/' + path;
  if (path !== '.') tree.appendChild(mkGoUp());
  const dirs  = (data.entries || []).filter(e =>  e.is_dir);
  const files = (data.entries || []).filter(e => !e.is_dir);
  dirs.forEach(e => {
    const it = mkDir(e);
    it.addEventListener('click', () => { pStk.push(e.path); loadTree(e.path); });
    tree.appendChild(it);
  });
  if (dirs.length && files.length) { const d = document.createElement('div'); d.className = 'ti-div'; tree.appendChild(d); }
  files.forEach(e => {
    const it = mkFile(e);
    it.addEventListener('click', () => { if (/\.[ch]$/.test(e.name)) openFile(e.path); });
    tree.appendChild(it);
  });
}

function mkGoUp() {
  const d = document.createElement('div'); d.className = 'ti go-up';
  d.innerHTML = svgUp() + '<span class="ti-ic"></span><span class="ti-name">..</span>';
  d.addEventListener('click', () => { pStk.pop(); loadTree(pStk[pStk.length - 1] || '.'); });
  return d;
}
function mkDir(e) {
  const d = document.createElement('div'); d.className = 'ti dir';
  d.innerHTML = svgCh() + `<span class="ti-ic">${svgFolder()}</span><span class="ti-name">${esc(e.name)}</span>`;
  return d;
}
function mkFile(e) {
  const ext = e.name.split('.').pop().toLowerCase();
  const d   = document.createElement('div'); d.className = 'ti';
  d.innerHTML = `<span style="width:10px;flex-shrink:0"></span><span class="ti-ic">${svgFile(ext)}</span><span class="ti-name">${esc(e.name)}</span>${mkBadge(ext)}`;
  return d;
}
function mkBadge(e) {
  const m = { c: 'tb-c', h: 'tb-h', md: 'tb-md', go: 'tb-go' };
  return m[e] ? `<span class="ti-badge ${m[e]}">${esc(e)}</span>` : '';
}
function svgCh() {
  return '<svg class="ti-ch" viewBox="0 0 10 10" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><polyline points="3,2 7,5 3,8"/></svg>';
}
function svgUp() {
  return '<svg class="ti-ch" viewBox="0 0 10 10" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><polyline points="2,7 5,3 8,7"/></svg>';
}
function svgFolder() {
  return '<svg width="12" height="11" viewBox="0 0 14 12"><path d="M1 1h4l1.5 2H13v8H1V1z" fill="var(--yellow)" opacity=".2" stroke="var(--yellow)" stroke-width=".7" stroke-linejoin="round"/></svg>';
}
function svgFile(ext) {
  const c = { c: 'var(--blue)', h: 'var(--yellow)', md: 'var(--purple)', go: 'var(--green)', mod: 'var(--green)', sum: 'var(--text4)' }[ext] || 'var(--text4)';
  return `<svg width="10" height="12" viewBox="0 0 10 12"><path d="M1 0h5.5L9 3.5V11H1V0z" fill="${c}" opacity=".15" stroke="${c}" stroke-width=".6" stroke-linejoin="round"/><path d="M5.5 0v3.5H9" stroke="${c}" stroke-width=".6" opacity=".4"/></svg>`;
}

$('nav-up')?.addEventListener('click', () => { if (pStk.length > 1) { pStk.pop(); loadTree(pStk[pStk.length - 1]); } });
$('sb-refresh')?.addEventListener('click', () => loadTree(pStk[pStk.length - 1] || '.'));

// ── Sidebar footer ────────────────────────────────────────────────────────────
function renderSbFoot() {
  const foot = $('sb-foot'); if (!foot) return;
  foot.innerHTML = '';
  A.versions.forEach(v => {
    const p   = A.progress[v.label] || { phase: v.indexed ? 'done' : 'indexing', pct: v.indexed ? 100 : 0, symbols: v.symbols || 0 };
    const pct = Math.round(p.pct || 0);
    const stat = p.phase === 'done'
      ? `✓ ${(p.symbols || v.symbols || 0).toLocaleString()}`
      : p.phase === 'parse' || p.phase === 'dedup' ? `${pct}%` : p.phase;
    const c = document.createElement('div'); c.className = 'vc';
    c.innerHTML = `<div class="vc-dot ${p.phase === 'done' ? 'done' : p.phase === 'error' ? 'error' : 'indexing'}"></div><span class="vc-label">${esc(v.label)}</span><span class="vc-stat">${stat}</span>`;
    if (p.phase === 'parse' || p.phase === 'dedup') {
      const pb = document.createElement('div'); pb.className = 'vc-pb';
      pb.innerHTML = `<div class="vc-fill" style="width:${pct}%"></div>`;
      c.appendChild(pb);
    }
    foot.appendChild(c);
  });

  // Lazy-indexable subdirectory chips
  const divider = document.createElement('div');
  divider.style.cssText = 'height:1px;background:var(--border);margin:3px 0';
  foot.appendChild(divider);

  const label = document.createElement('div');
  label.style.cssText = 'font:700 9px var(--fu);letter-spacing:.06em;text-transform:uppercase;color:var(--text4);padding:2px 4px;display:flex;align-items:center;justify-content:space-between';
  label.innerHTML = '<span>Skipped dirs</span>'
    + '<button onclick="openIndexDirModal()" title="Index a skipped directory" '
    + 'style="font-size:10px;color:var(--text4);background:none;border:none;cursor:pointer;padding:0 2px" '
    + 'onmouseover="this.style.color=\'var(--blue)\'" onmouseout="this.style.color=\'var(--text4)\'">+ index</button>';
  foot.appendChild(label);

  LAZY_DIRS.forEach(d => {
    const state = indexedSubDirs[d.dir];
    const chip  = document.createElement('div');
    chip.className = 'vc';
    chip.style.cursor = state === 'running' ? 'default' : 'pointer';
    chip.title = d.title + (state ? ' — ' + state : ' — click to index');

    const dotColor = state === 'done'    ? 'var(--green)'
                   : state === 'running' ? 'var(--yellow)'
                   : state === 'error'   ? 'var(--red)'
                   : 'var(--border2)';
    const dotAnim  = state === 'running' ? 'animation:blink 1s ease-in-out infinite' : '';
    const stat     = state === 'done'    ? '✓ indexed'
                   : state === 'running' ? 'indexing…'
                   : state === 'error'   ? '✗ error'
                   : 'not indexed';

    chip.innerHTML = `<div class="vc-dot" style="background:${dotColor};${dotAnim}"></div>`
      + `<span class="vc-label" style="color:${state?'var(--text)':'var(--text4)'}">${esc(d.icon)} ${esc(d.dir)}</span>`
      + `<span class="vc-stat" style="color:${state==='error'?'var(--red)':'var(--text3)'}">${stat}</span>`;

    if (state !== 'running') {
      chip.addEventListener('click', () => openIndexDirModal());
    }
    foot.appendChild(chip);
  });
}

// ── Search ─────────────────────────────────────────────────────────────────────
const srchIn   = $('srch');
const srchDrop = $('srch-drop');
let srchTimer  = null;

srchIn?.addEventListener('input', () => {
  clearTimeout(srchTimer);
  const q = srchIn.value.trim();
  if (q.length < 2) { srchDrop.style.display = 'none'; return; }
  srchTimer = setTimeout(() => doSearch(q), 130);
});
srchIn?.addEventListener('keydown', e => { if (e.key === 'Escape') { srchDrop.style.display = 'none'; srchIn.blur(); } });
document.addEventListener('keydown', e => {
  if ((e.ctrlKey || e.metaKey) && e.key === 'k') { e.preventDefault(); srchIn?.focus(); srchIn?.select(); }
});
document.addEventListener('click', e => { if (!e.target.closest('#srch-wrap')) srchDrop.style.display = 'none'; });

async function doSearch(q) {
  const d = await api('/search?q=' + encodeURIComponent(q) + '&limit=40&version=' + encodeURIComponent(A.activeVer));
  srchDrop.innerHTML = '';
  (d.results || []).forEach(s => {
    const r = document.createElement('div'); r.className = 'sd-row';
    r.innerHTML = `<span class="kb kb-${s.kind_str}">${esc(s.type)}</span><span class="sd-name">${esc(s.name)}</span><span class="sd-file">${esc(s.file)}:${s.line}</span>`;
    r.addEventListener('click', () => { srchDrop.style.display = 'none'; srchIn.value = ''; openFile(s.file); });
    r.addEventListener('contextmenu', e => {
      e.preventDefault(); srchDrop.style.display = 'none';
      ctxTarget = { sym: s.name, file: s.file, line: s.line, kind: s.kind_str, type: s.type };
      showCtxMenu(e.clientX, e.clientY, [{ title: s.name }, '-',
        { label: 'XRef',             action: 'xref',           icon: '🔍' },
        { label: 'Graph',            action: 'xrefgraph',      icon: '◎' },
        { label: 'Open',             action: 'opendef',        icon: '→' },
        { label: 'Send to plugin…',  action: 'send_to_plugin', icon: '⌁' },
        { label: 'Copy',             action: 'copyname',       icon: '📋' },
      ]);
    });
    srchDrop.appendChild(r);
  });
  if (!(d.results || []).length) srchDrop.innerHTML = '<div class="sd-row" style="color:var(--text4)">No results</div>';
  srchDrop.style.display = 'block';
}

// ── Jobs view ─────────────────────────────────────────────────────────────────
function initJobsSlot(slot) {
  const list = qs('.jobs-list', slot); if (!list) return;
  const render = jobs => {
    list.innerHTML = (jobs || []).map(j => `
      <div class="job-row">
        <span class="job-kind">${esc(j.kind)}</span>
        <span class="job-label">${esc(j.label || j.job_id)}</span>
        <div class="job-prog"><div class="job-prog-fill" style="width:${Math.round(j.progress || 0)}%"></div></div>
        <span class="job-state js-${j.state}">${j.state}</span>
        ${j.state === 'running' || j.state === 'queued' ? `<button class="job-cancel" onclick="cancelJob('${j.job_id}')">✕</button>` : ''}
      </div>`).join('') || '<div style="padding:8px;color:var(--text4);font-size:10px">No jobs yet</div>';
  };
  api('/jobs').then(d => { A.jobs = d.jobs || []; render(A.jobs); });
  Bus.on('jobs:update', render);
}
async function cancelJob(id) { await api('/jobs/' + id + '/cancel', { method: 'POST' }); }
async function cancelAllJobs() { for (const j of A.jobs) if (j.state === 'running' || j.state === 'queued') await cancelJob(j.job_id); }
function clearDoneJobs() { A.jobs = A.jobs.filter(j => j.state !== 'done' && j.state !== 'cancelled'); Bus.emit('jobs:update', A.jobs); }

// ── Plugins view ───────────────────────────────────────────────────────────────
async function initPluginsSlot(slot) {
  const grid = qs('.plug-grid', slot); if (!grid) return;
  const render = async () => {
    grid.innerHTML = '';
    let d;
    try { d = await api('/plugins'); } catch(e) { grid.innerHTML = '<div style="color:var(--red);padding:8px;font-size:10px">'+esc(e.message)+'</div>'; return; }
    (d.plugins || []).forEach(p => {
      const c = document.createElement('div'); c.className = 'pc';
      const acts = (p.actions || []).map(a => {
        const id    = typeof a === 'object' ? (a.id    || '') : String(a);
        const label = typeof a === 'object' ? (a.label || a.id || String(a)) : String(a);
        const icon  = typeof a === 'object' ? (a.icon  || '') : '';
        const desc  = typeof a === 'object' ? (a.description || '') : '';
        return `<button class="pc-act" title="${esc(desc)}" onclick="execPlug('${esc(p.name)}', '${esc(id)}')">${icon ? icon + ' ' : ''}${esc(label)}</button>`;
      }).join('');
      const author = p.author ? '<span style="margin-left:auto;font-size:9px;color:var(--text3)">'+esc(p.author)+'</span>' : '';
      c.innerHTML = '<div class="pc-name">'+esc(p.name)+'<span class="pc-ver">v'+esc(p.version||'?')+'</span>'+author+'</div>'
        + '<div class="pc-desc">'+esc(p.description||'')+'</div>'
        + '<div class="pc-acts">'+acts+'</div>';
      grid.appendChild(c);
    });
    if (!(d.plugins||[]).length) grid.innerHTML = '<div style="color:var(--text4);font-size:10px;padding:8px">No plugins loaded. Place .so files in the plugin directory and use Reload.</div>';
  };
  render();
}
async function reloadPlugins() {
  try {
    const d = await apiPost('/plugins/reload', {});
    document.querySelectorAll('.plug-grid').forEach(g => { const s = g.closest('[data-slot]'); if(s) initPluginsSlot(s); });
  } catch(e) { alert('Reload failed: '+e.message); }
}
async function execPlug(n, a) { const r = await apiPost('/plugin/' + encodeURIComponent(n), {action:a}); alert(JSON.stringify(r,null,2)); }

// ── On-demand subdirectory indexing ──────────────────────────────────────────

// Well-known large subdirs that are skipped by default.
// Shown as clickable chips in the sidebar footer.
const LAZY_DIRS = [
  { dir: 'drivers',   label: 'drivers',   icon: '⚙', title: 'Device drivers (~40% of the kernel tree)' },
  { dir: 'arch',      label: 'arch',      icon: '⛏', title: 'Architecture-specific code' },
  { dir: 'sound',     label: 'sound',     icon: '♪', title: 'ALSA / sound subsystem' },
  { dir: 'net',       label: 'net',       icon: '⇄', title: 'Networking stack' },
];

// Track which sub-dirs have been queued/indexed this session
const indexedSubDirs = {};

async function indexSubDir(subDir) {
  const vs = curVS();
  if (!vs) { alert('No version selected'); return; }
  if (indexedSubDirs[subDir] === 'running') { alert(subDir + ' is already being indexed'); return; }

  indexedSubDirs[subDir] = 'running';
  renderSbFoot(); // update chip state immediately

  try {
    const d = await apiPost('/index-dir', { sub_dir: subDir, version: vs.label });
    Bus.emit('alert', {
      level: 'info',
      title: 'Indexing ' + subDir,
      message: 'Job ' + d.job_id + ' queued — symbols will appear as indexing progresses.',
    });
  } catch (e) {
    indexedSubDirs[subDir] = 'error';
    renderSbFoot();
    alert('Failed to queue ' + subDir + ': ' + e.message);
  }
}

function openIndexDirModal() {
  const vs = curVS();
  const rows = LAZY_DIRS.map(d => {
    const state = indexedSubDirs[d.dir];
    const stateLabel = state === 'running' ? ' (indexing…)' : state === 'done' ? ' ✓' : '';
    const disabled   = state === 'running' ? 'disabled' : '';
    return `<div style="display:flex;align-items:center;gap:8px;padding:6px 0;border-bottom:1px solid var(--border)">
      <span style="font-size:14px">${d.icon}</span>
      <div style="flex:1">
        <div style="font:600 11px var(--fm);color:var(--text)">${esc(d.dir)}${stateLabel}</div>
        <div style="font-size:9px;color:var(--text3)">${esc(d.title)}</div>
      </div>
      <button class="btn-ok" ${disabled} onclick="indexSubDir('${esc(d.dir)}');closeModal()"
        style="height:24px;padding:0 10px;font-size:10px">Index</button>
    </div>`;
  }).join('');

  const customRow = `<div style="margin-top:10px">
    <label style="display:block;font-size:9px;color:var(--text3);text-transform:uppercase;letter-spacing:.05em;margin-bottom:4px">Custom subdirectory</label>
    <div style="display:flex;gap:5px">
      <input id="custom-subdir" type="text" placeholder="e.g. sound/soc" style="flex:1;height:26px">
      <button class="btn-ok" onclick="indexSubDir(document.getElementById('custom-subdir').value.trim());closeModal()"
        style="height:26px;padding:0 10px;font-size:10px">Index</button>
    </div>
  </div>`;

  showModal(`
    <h3>Index subdirectory</h3>
    <p style="font-size:10px;color:var(--text3);margin-bottom:10px">
      These directories are skipped during the initial index to keep startup fast.
      Click <b>Index</b> to scan them on demand — results merge into the active version.
    </p>
    ${rows}
    ${customRow}
    <div class="modal-btns" style="margin-top:12px">
      <button class="btn-cancel" onclick="closeModal()">Close</button>
    </div>
  `);
}

// Update indexedSubDirs when a sub-dir job completes
Bus.on('jobs:update', (jobs) => {
  LAZY_DIRS.forEach(d => {
    const job = jobs.find(j => j.label === 'index-dir:' + d.dir);
    if (!job) return;
    if (job.state === 'done')  { indexedSubDirs[d.dir] = 'done';    renderSbFoot(); }
    if (job.state === 'error') { indexedSubDirs[d.dir] = 'error';   renderSbFoot(); }
    if (job.state === 'running' || job.state === 'queued') {
      indexedSubDirs[d.dir] = 'running'; renderSbFoot();
    }
  });
});


// ── Bootstrap ──────────────────────────────────────────────────────────────────
(async () => {
  // Retry /api/status until the server is ready (handles race at startup)
  let st;
  for (let attempt = 0; attempt < 20; attempt++) {
    try {
      st = await api('/status');
      break;
    } catch (e) {
      const msg = document.getElementById('load-title');
      if (msg) msg.textContent = 'Connecting to server' + '.'.repeat(attempt % 4 + 1);
      await new Promise(r => setTimeout(r, 500));
    }
  }
  if (!st) {
    const msg = document.getElementById('load-title');
    if (msg) msg.textContent = 'Cannot reach server — is KScope running?';
    return;
  }

  A.versions  = st.versions || [];
  A.activeVer = A.versions[0]?.label || '';

  // Pre-populate progress from /api/status so overlay shows correct state immediately,
  // even before any SSE events arrive.
  A.versions.forEach(v => {
    A.progress[v.label] = {
      label:   v.label,
      phase:   v.indexed ? 'done' : 'walk',
      pct:     v.indexed ? 100 : 0,
      symbols: v.symbols || 0,
      files:   v.files   || 0,
      total:   v.files   || 0,
    };
  });

  renderVerSelect();
  renderSbFoot();
  updateStatusBar();
  updateLoadingOverlay();   // hide immediately if everything is already indexed
  connectSSE();

  wsCreate('Main');
  wsRenderBar();
  renderGrid();
  await loadTree('.');
  initPluginPopup(); // must come after DOM ready

  Bus.on('index:done', () => { loadTree(pStk[pStk.length - 1] || '.'); renderSbFoot(); updateStatusBar(); });

  // Wire Bus to update the loading overlay when index progress arrives
  Bus.on('index:progress', updateLoadingOverlay);
  Bus.on('index:done',     updateLoadingOverlay);

  // Hard fallback — always hide after 8s regardless of indexing state
  setTimeout(hideLoadingOverlay, 8000);
})();

// Expose functions used in onclick attributes
window.cancelAllJobs = cancelAllJobs;
window.cancelJob = cancelJob;
window.clearDoneJobs = clearDoneJobs;
window.closeModal = closeModal;
window.doGitCheckout = doGitCheckout;
window.execPlug = execPlug;
window.reloadPlugins = reloadPlugins;
