// js/symbols.js — Non-source symbol browser panel.
// Presents all indexed symbols as interactive cards with filtering,
// grouping by kind/type/file, and one-click plugin dispatch.
// Part of github.com/hanemile/kscope

// ── Symbol browser panel ──────────────────────────────────────────────────────

// Called by initSlotView when view === 'symbols'
async function initSymbolsSlot(slot, st) {
  const body = qs('.sym-browser-body', slot);
  if (!body) return;

  // Wire toolbar controls
  const filterKind = qs('.sb-filter-kind', slot);
  const filterType = qs('.sb-filter-type', slot);
  const filterQ    = qs('.sb-filter-q',    slot);
  const groupBy    = qs('.sb-group-by',    slot);
  const sortBy     = qs('.sb-sort-by',     slot);

  let state = {
    kind: st.kind || '', type: st.type || '', q: st.q || '',
    group: st.group || 'kind', sort: st.sort || 'name',
    page: 1, perPage: 200,
    data: null,
  };

  const load = async () => {
    const params = new URLSearchParams({
      version:  A.activeVer,
      kind:     state.kind,
      type:     state.type,
      q:        state.q,
      page:     state.page,
      per_page: state.perPage,
    });
    try {
      state.data = await api('/symbols?' + params);
      renderSymbolBrowser(slot, state);
    } catch (e) {
      body.innerHTML = `<div style="padding:12px;color:var(--red);font-size:10px">${esc(e.message)}</div>`;
    }
  };

  filterKind?.addEventListener('change', () => { state.kind = filterKind.value; state.page = 1; load(); });
  filterType?.addEventListener('change', () => { state.type = filterType.value; state.page = 1; load(); });
  filterQ?.addEventListener('input',    () => { state.q    = filterQ.value;    state.page = 1; load(); });
  groupBy?.addEventListener('change',   () => { state.group = groupBy.value;                   renderSymbolBrowser(slot, state); });
  sortBy?.addEventListener('change',    () => { state.sort  = sortBy.value;                    renderSymbolBrowser(slot, state); });

  // Reload when index finishes
  Bus.on('index:done', load);

  await load();
}

function renderSymbolBrowser(slot, state) {
  const body  = qs('.sym-browser-body', slot);
  const count = qs('.sb-count',         slot);
  if (!body || !state.data) return;

  const syms = state.data.symbols || [];
  if (count) count.textContent = `${syms.length} / ${state.data.total}`;

  // Sort
  const sorted = [...syms].sort((a, b) => {
    if (state.sort === 'name')   return a.name.localeCompare(b.name);
    if (state.sort === 'xrefs')  return (b.xref_count || 0) - (a.xref_count || 0);
    if (state.sort === 'file')   return a.file.localeCompare(b.file);
    return 0;
  });

  // Group
  const groups = groupSymbols(sorted, state.group);

  body.innerHTML = '';
  for (const [gKey, gSyms] of Object.entries(groups)) {
    const section = document.createElement('div');
    section.className = 'sym-group';

    const hdr = document.createElement('div');
    hdr.className = 'sym-group-hdr';
    hdr.innerHTML = `<span class="sym-group-label">${esc(gKey)}</span><span class="sym-group-count">${gSyms.length}</span>`;
    hdr.addEventListener('click', () => section.classList.toggle('collapsed'));
    section.appendChild(hdr);

    const grid = document.createElement('div');
    grid.className = 'sym-card-grid';
    gSyms.forEach(s => grid.appendChild(buildSymbolCard(s)));
    section.appendChild(grid);
    body.appendChild(section);
  }

  if (sorted.length === 0) {
    body.innerHTML = '<div style="padding:16px;color:var(--text4);font-size:10px">No symbols found. Indexing may still be in progress.</div>';
  }
}

function groupSymbols(syms, by) {
  const g = {};
  syms.forEach(s => {
    const key = by === 'kind' ? s.kind :
                by === 'type' ? s.type :
                by === 'file' ? s.file.split('/').slice(0, 3).join('/') :
                'All';
    (g[key] = g[key] || []).push(s);
  });
  // Sort group keys
  return Object.fromEntries(Object.entries(g).sort((a, b) => a[0].localeCompare(b[0])));
}

function buildSymbolCard(s) {
  const card = document.createElement('div');
  card.className = 'sym-card';
  if (s.has_dup) card.classList.add('has-dup');

  const xrefBar = s.xref_count > 0
    ? `<div class="sym-card-xref-bar" title="${s.xref_count} usages" style="width:${Math.min(100, s.xref_count * 2)}%"></div>`
    : '';

  card.innerHTML = `
    <div class="sym-card-head">
      <span class="kb kb-${esc(s.kind)}">${esc(s.type || s.kind)}</span>
      ${s.has_dup ? '<span class="sym-dup-pip" title="Kernel/UAPI duplicate">⑂</span>' : ''}
    </div>
    <div class="sym-card-name" title="${esc(s.name)}">${esc(s.name)}</div>
    <div class="sym-card-file" title="${esc(s.file)}">${esc(shortPath(s.file))}:${s.line}</div>
    <div class="sym-card-footer">
      <span class="sym-card-xref-count">${s.xref_count > 0 ? s.xref_count + ' refs' : ''}</span>
      <div class="sym-xref-bar-wrap">${xrefBar}</div>
    </div>`;

  // Left click → show plugin popup
  card.addEventListener('click', e => {
    e.stopPropagation();
    showPluginPopup({ sym: s.name, kind: s.kind, type: s.type, file: s.file, line: s.line }, e.clientX, e.clientY);
  });

  // Right click → context menu (matches source view behaviour)
  card.addEventListener('contextmenu', e => {
    e.preventDefault();
    ctxTarget = { sym: s.name, file: s.file, line: s.line };
    showCtxMenu(e.clientX, e.clientY, [
      { title: s.name },
      '-',
      { label: 'Open definition',    action: 'opendef',   icon: '→'  },
      { label: 'XRef: find usages',  action: 'xref',      icon: '🔍' },
      { label: 'XRef graph',         action: 'xrefgraph', icon: '◎'  },
      { label: 'Add to node editor', action: 'addnode',   icon: '◆'  },
      { label: 'Send to search',     action: 'tosearch',  icon: '⌕'  },
      { label: 'Copy name',          action: 'copyname',  icon: '📋' },
      '-',
      { label: 'Send to plugin…',    action: 'send_to_plugin', icon: '⌁' },
    ]);
  });

  return card;
}

function shortPath(p) {
  const parts = p.split('/');
  if (parts.length <= 3) return p;
  return '…/' + parts.slice(-2).join('/');
}

// ── Symbol detail popup (rich view) ──────────────────────────────────────────

async function openSymbolDetail(symName) {
  const d = await api('/symbol/' + encodeURIComponent(symName) + '/view?version=' + encodeURIComponent(A.activeVer));

  const lines = [
    `<div class="sd-title">${esc(symName)}</div>`,
    `<div class="sd-meta">`,
  ];

  (d.definitions || []).forEach(def => {
    lines.push(`<div class="sd-def">
      <span class="kb kb-${esc(def.kind_str)}">${esc(def.type)}</span>
      <span class="sd-def-file" onclick="openFile('${esc(def.file)}')" style="cursor:pointer;color:var(--blue)">${esc(def.file)}:${def.line}</span>
      ${def.uapi_peer ? `<span class="sym-dup-pip" title="Dup: ${esc(def.uapi_peer)}">⑂</span>` : ''}
    </div>`);
  });

  lines.push(`</div>`);

  if (d.xref_count > 0) {
    const topFiles = Object.entries(d.xref_files || {})
      .sort((a, b) => b[1] - a[1]).slice(0, 5);
    lines.push(`<div class="sd-section-title">Used in ${d.xref_count} places</div>`);
    lines.push(`<div class="sd-xref-files">`);
    topFiles.forEach(([f, n]) => {
      lines.push(`<div class="sd-xref-file" onclick="openFile('${esc(f)}')" style="cursor:pointer">${esc(shortPath(f))} <span style="color:var(--text3)">(${n})</span></div>`);
    });
    lines.push('</div>');
  }

  if (d.dup_peer) {
    lines.push(`<div class="sd-dup-warn">⑂ Kernel/UAPI duplicate: <span onclick="openFile('${esc(d.dup_peer)}')" style="cursor:pointer;color:var(--red)">${esc(d.dup_peer)}</span></div>`);
  }

  // Plugin actions
  if ((d.actions || []).length > 0) {
    lines.push(`<div class="sd-section-title">Plugin actions</div><div class="sd-actions">`);
    (d.actions || []).forEach(a => {
      lines.push(`<button class="sd-action-btn" onclick="runPluginAction('${esc(a.plugin)}','${esc(a.action_id)}','${esc(symName)}','${esc(d.version||A.activeVer)}')">
        ${a.icon ? `<span>${a.icon}</span>` : ''}${esc(a.label)}
      </button>`);
    });
    lines.push('</div>');
  }

  lines.push(`<div class="sd-footer"><button class="btn-ok" onclick="showPluginPopup({sym:'${esc(symName)}'},0,0,true);closeModal()">⌁ Send to plugin…</button></div>`);

  showModal(`<div class="sym-detail">${lines.join('')}</div>`);
}

async function runPluginAction(plugin, action, sym, version) {
  closeModal();
  const result = await apiPost('/plugin-dispatch', { plugin, action, symbol: sym, version: version || A.activeVer });
  handlePluginResult(result, sym);
}

// Expose functions used in onclick attributes
window.runPluginAction = runPluginAction;
