// js/callgraph.js — Function call graph panel.
// Shows callers/callees of a function up to configurable depth.
// Reuses the graph.js renderer via openPanelWithState.
// Part of github.com/hanemile/kscope
'use strict';

// Called by initSlotView when view === 'callgraph'
async function initCallgraphSlot(slot, st) {
  const body    = qs('.cg-body',   slot);
  const symIn   = qs('.cg-sym',    slot);
  const depthIn = qs('.cg-depth',  slot);
  const goBtn   = qs('.cg-go',     slot);
  const list    = qs('.cg-list',   slot);
  if (!body) return;

  // Pre-fill from state
  if (st && st.sym && symIn)   symIn.value   = st.sym;
  if (st && st.depth && depthIn) depthIn.value = st.depth;

  const load = async () => {
    const sym   = symIn?.value.trim();
    const depth = parseInt(depthIn?.value || '2', 10);
    if (!sym) return;

    list.innerHTML = '<div style="padding:8px;color:var(--text4);font-size:10px">Loading…</div>';
    try {
      const d = await api('/callgraph/' + encodeURIComponent(sym)
                        + '?version=' + encodeURIComponent(A.activeVer)
                        + '&depth=' + depth);

      // Build a readable call list grouped by direction
      const callers = (d.edges || []).filter(e => {
        const tn = (d.nodes || [])[e.target];
        return tn && tn.label === sym;
      });
      const callees = (d.edges || []).filter(e => {
        const sn = (d.nodes || [])[e.source];
        return sn && sn.label === sym;
      });

      list.innerHTML = '';

      if (callers.length) {
        const h = document.createElement('div');
        h.style.cssText = 'padding:4px 8px;font:700 9px var(--fu);color:var(--text3);text-transform:uppercase;letter-spacing:.06em;border-bottom:1px solid var(--border)';
        h.textContent = 'Called by (' + callers.length + ')';
        list.appendChild(h);
        callers.forEach(e => {
          const sn = (d.nodes || [])[e.source];
          if (!sn) return;
          const row = cgRow(sn.label, sn.kind, e.file, e.line, '↑ caller');
          list.appendChild(row);
        });
      }

      if (callees.length) {
        const h = document.createElement('div');
        h.style.cssText = 'padding:4px 8px;font:700 9px var(--fu);color:var(--text3);text-transform:uppercase;letter-spacing:.06em;border-bottom:1px solid var(--border);margin-top:4px';
        h.textContent = 'Calls (' + callees.length + ')';
        list.appendChild(h);
        callees.forEach(e => {
          const tn = (d.nodes || [])[e.target];
          if (!tn) return;
          const row = cgRow(tn.label, tn.kind, e.file, e.line, '↓ callee');
          list.appendChild(row);
        });
      }

      if (!callers.length && !callees.length) {
        list.innerHTML = '<div style="padding:8px;color:var(--text4);font-size:10px">No call edges found. Try a lower depth or check indexing is complete.</div>';
      }

      // Update slot state so it survives panel switches
      const pi = Number(slot.dataset.slot);
      if (!isNaN(pi) && ws().panels[pi]) {
        ws().panels[pi].st = { sym, depth };
      }
    } catch (e) {
      list.innerHTML = `<div style="padding:8px;color:var(--red);font-size:10px">${esc(e.message)}</div>`;
    }
  };

  goBtn?.addEventListener('click', load);
  symIn?.addEventListener('keydown', e => { if (e.key === 'Enter') load(); });

  // Open graph view button
  qs('.cg-graph', slot)?.addEventListener('click', () => {
    const sym   = symIn?.value.trim();
    const depth = parseInt(depthIn?.value || '2', 10);
    if (sym) openPanelWithState('graph', { mode: 'callgraph', sym, depth });
  });

  if (st && st.sym) load();
}

function cgRow(name, kind, file, line, direction) {
  const row = document.createElement('div');
  row.className = 'xr-row';
  row.innerHTML = `
    <span class="xr-fn" style="color:${kind==='function'?'var(--blue)':'var(--text2)'}">${esc(name)}</span>
    <span class="xr-ctx" style="color:var(--text4)">${esc(direction)}</span>
    <span class="xr-loc">${file ? esc(file.split('/').slice(-2).join('/')) + ':' + line : ''}</span>`;
  row.addEventListener('click', () => { if (file) openFile(file); });
  row.addEventListener('contextmenu', e => {
    e.preventDefault();
    ctxTarget = { sym: name, file: file || '', line: line || 0, kind };
    showCtxMenu(e.clientX, e.clientY, [
      { title: name }, '-',
      { label: 'Call graph',        action: 'callgraph',      icon: '↕'  },
      { label: 'XRef usages',       action: 'xref',           icon: '🔍' },
      { label: 'Open file',         action: 'opendef',        icon: '→'  },
      { label: 'Send to plugin…',   action: 'send_to_plugin', icon: '⌁'  },
    ]);
  });
  return row;
}

// Wire callgraph action from context menu
Bus.on('ctx:action', ({ action, target }) => {
  if (action !== 'callgraph' || !target || !target.sym) return;
  openPanelWithState('callgraph', { sym: target.sym, depth: 2 });
});
