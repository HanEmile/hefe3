// js/popup.js — Universal "Send to plugin" popup.
// Triggered from any panel (source sym-link, symbol card, graph node,
// context menu, node editor). Dynamically fetches available actions for
// the symbol's kind from /api/plugin-actions, presents them, collects
// any extra params, dispatches via /api/plugin-dispatch, and renders the result.
// Part of github.com/hanemile/kscope

// ── Plugin popup state ────────────────────────────────────────────────────────

const PP = {
  el:        null,   // popup DOM element
  actions:   [],     // loaded actions
  ctx:       null,   // current symbol context
  inline:    false,  // inline mode (no floating popup, use modal)
};

function initPluginPopup() {
  const el = document.createElement('div');
  el.id = 'plugin-popup';
  el.style.cssText = [
    'position:fixed', 'z-index:600', 'display:none',
    'background:var(--bg2)', 'border:1px solid var(--border2)',
    'border-radius:5px', 'box-shadow:0 8px 32px #000000b0',
    'min-width:280px', 'max-width:420px', 'max-height:80vh',
    'overflow-y:auto', 'font-size:11px',
  ].join(';');
  document.body.appendChild(el);
  PP.el = el;

  // Close on outside click
  document.addEventListener('click', e => {
    if (PP.el && !PP.el.contains(e.target)) hidePluginPopup();
  });
  document.addEventListener('keydown', e => { if (e.key === 'Escape') hidePluginPopup(); });
}

function hidePluginPopup() {
  if (PP.el) PP.el.style.display = 'none';
}

// ── Main entry point: call from any panel ─────────────────────────────────────

// ctx: { sym, kind, type, file, line, version }
// x,y: screen coordinates (0,0 → center modal instead)
async function showPluginPopup(ctx, x, y, forceModal) {
  PP.ctx = ctx;
  hidePluginPopup();

  // Fetch available actions dynamically
  const kind    = ctx.kind    || 'unknown';
  const type    = ctx.type    || '';
  const version = ctx.version || A.activeVer;
  let actions;
  try {
    const d = await api('/plugin-actions?kind=' + encodeURIComponent(kind) + '&type=' + encodeURIComponent(type));
    actions = d.actions || [];
  } catch (_) { actions = []; }
  PP.actions = actions;

  // Build content
  const html = buildPopupHTML(ctx, actions);

  if (forceModal || (!x && !y)) {
    showModal(html);
    return;
  }

  PP.el.innerHTML = html;
  PP.el.style.display = 'block';
  // Position
  const vw = window.innerWidth, vh = window.innerHeight;
  const pw = 300, ph = Math.min(actions.length * 44 + 120, 500);
  PP.el.style.left = Math.min(x + 4, vw - pw - 10) + 'px';
  PP.el.style.top  = Math.min(y + 4, vh - ph - 10) + 'px';
}

function buildPopupHTML(ctx, actions) {
  const groupedActions = groupActionsByPlugin(actions);

  let html = `
    <div style="padding:10px 12px 6px;border-bottom:1px solid var(--border);display:flex;align-items:center;gap:6px">
      <span class="kb kb-${esc(ctx.kind || 'unknown')}">${esc(ctx.type || ctx.kind || '?')}</span>
      <span style="font:600 11px var(--fm);color:var(--text);flex:1;overflow:hidden;text-overflow:ellipsis">${esc(ctx.sym)}</span>
      <button onclick="hidePluginPopup();closeModal()" style="color:var(--text4);font-size:12px">✕</button>
    </div>`;

  if (ctx.file) {
    html += `<div style="padding:3px 12px 5px;font-size:9px;color:var(--text3);font-family:var(--fm);border-bottom:1px solid var(--border)">${esc(shortPath(ctx.file))}${ctx.line ? ':' + ctx.line : ''}</div>`;
  }

  if (actions.length === 0) {
    html += `<div style="padding:12px;color:var(--text4)">No plugin actions available for this symbol kind.</div>`;
  } else {
    for (const [pluginName, pluginActions] of Object.entries(groupedActions)) {
      html += `<div style="padding:4px 12px 2px;font-size:9px;font-weight:700;letter-spacing:.06em;text-transform:uppercase;color:var(--text3)">${esc(pluginName)}</div>`;
      pluginActions.forEach(a => {
        const hasExtraParams = (a.params || []).filter(p => p.kind !== 'symbol' && p.kind !== 'version').length > 0;
        html += `
          <div class="pp-action" onclick="${hasExtraParams ? `showParamForm('${esc(a.plugin)}','${esc(a.action_id)}')` : `dispatchPopupAction('${esc(a.plugin)}','${esc(a.action_id)}')`}"
               title="${esc(a.description || '')}">
            <span class="pp-icon">${a.icon || '⌁'}</span>
            <div class="pp-action-text">
              <div class="pp-action-label">${esc(a.label)}</div>
              ${a.description ? `<div class="pp-action-desc">${esc(a.description)}</div>` : ''}
            </div>
            ${a.result_view ? `<span class="pp-result-hint">${esc(a.result_view)}</span>` : ''}
          </div>`;
      });
    }
  }

  html += `
    <div style="padding:6px 12px;border-top:1px solid var(--border);display:flex;gap:5px">
      <button class="pp-quick" onclick="dispatchPopupAction('xref-trace','trace_usages')">🔍 XRef</button>
      <button class="pp-quick" onclick="dispatchPopupAction('xref-trace','xref_graph')">◎ Graph</button>
      <button class="pp-quick" onclick="openFile('${esc(ctx.file || '')}')">→ Open</button>
      <button class="pp-quick" onclick="navigator.clipboard?.writeText('${esc(ctx.sym)}')">📋 Copy</button>
    </div>`;

  return html;
}

function groupActionsByPlugin(actions) {
  const g = {};
  actions.forEach(a => { (g[a.plugin] = g[a.plugin] || []).push(a); });
  return g;
}

// ── Param form for actions that need extra input ───────────────────────────────

function showParamForm(plugin, actionId) {
  const action = PP.actions.find(a => a.plugin === plugin && a.action_id === actionId);
  if (!action) return;

  const extraParams = (action.params || []).filter(p => p.kind !== 'symbol' && p.kind !== 'version');

  let fields = extraParams.map(p => `
    <label style="display:block;font-size:9px;color:var(--text3);margin-top:8px;text-transform:uppercase;letter-spacing:.05em">${esc(p.label)}</label>
    <input id="pf-${esc(p.name)}" type="text" value="${esc(p.default || '')}"
           style="width:100%;height:24px;margin-top:2px" ${p.required ? 'required' : ''}>`
  ).join('');

  showModal(`
    <h3>${esc(action.icon || '⌁')} ${esc(action.label)}</h3>
    <div style="font-size:10px;color:var(--text3);margin-bottom:8px">${esc(action.description || '')}</div>
    <div style="font-size:10px;color:var(--text2);margin-bottom:8px">
      Symbol: <span style="font-family:var(--fm);color:var(--blue)">${esc(PP.ctx.sym)}</span>
    </div>
    ${fields}
    <div class="modal-btns">
      <button class="btn-cancel" onclick="closeModal()">Cancel</button>
      <button class="btn-ok" onclick="submitParamForm('${esc(plugin)}','${esc(actionId)}')">Run</button>
    </div>
  `);
}

function submitParamForm(plugin, actionId) {
  const action = PP.actions.find(a => a.plugin === plugin && a.action_id === actionId);
  const params = {};
  if (action) {
    (action.params || []).forEach(p => {
      if (p.kind === 'symbol')  { params[p.name] = PP.ctx.sym;            return; }
      if (p.kind === 'version') { params[p.name] = PP.ctx.version || A.activeVer; return; }
      const el = document.getElementById('pf-' + p.name);
      if (el) params[p.name] = el.value;
    });
  }
  closeModal();
  dispatchPopupAction(plugin, actionId, params);
}

// ── Dispatch and result rendering ─────────────────────────────────────────────

async function dispatchPopupAction(plugin, actionId, extraParams) {
  hidePluginPopup(); closeModal();
  const ctx = PP.ctx || {};
  const body = {
    plugin,
    action:  actionId,
    symbol:  ctx.sym  || '',
    file:    ctx.file || '',
    line:    ctx.line || 0,
    version: ctx.version || A.activeVer,
    params:  extraParams ? JSON.stringify(extraParams) : undefined,
  };
  try {
    const result = await apiPost('/plugin-dispatch', body);
    handlePluginResult(result, ctx.sym);
  } catch (e) {
    Bus.emit('alert', { level: 'error', title: 'Plugin error', message: e.message });
  }
}

// ── Result rendering ──────────────────────────────────────────────────────────

function handlePluginResult(result, symName) {
  if (result.error) {
    showModal(`<h3>Plugin error</h3><div style="color:var(--red);font-size:11px;padding:8px 0">${esc(result.error)}</div><div class="modal-btns"><button class="btn-cancel" onclick="closeModal()">Close</button></div>`);
    return;
  }

  const view = result.result_view || 'json';
  const data = result.data;

  switch (view) {
    case 'graph':
      // Fetch endpoint and open in graph panel
      if (data && data.endpoint) {
        const sym = data.symbol || symName;
        openPanelWithState('graph', { mode: 'xref', sym });
      }
      break;

    case 'table':
      // Fetch endpoint and open in xref panel or show inline
      if (data && data.endpoint) {
        const ep = data.resolved_endpoint || data.endpoint;
        fetchAndShowTable(ep, symName);
      } else {
        showResultModal('table', data, symName);
      }
      break;

    case 'struct':
      // Open struct layout visualizer
      Bus.emit('structview:load', { sym: symName });
      break;

    case 'source':
      // Navigate to symbol definition
      if (data && data.action === 'navigate' && data.endpoint) {
        api(data.endpoint.replace('/api', '')).then(d => {
          const syms = d.symbols || [];
          if (syms.length > 0) openFile(syms[0].file);
        });
      }
      break;

    default: // 'json'
      showResultModal('json', data, symName);
  }
}

async function fetchAndShowTable(endpoint, symName) {
  try {
    const d = await fetch(endpoint).then(r => r.json());
    // Try to find a list: results, xrefs, duplicates, symbols
    const list = d.results || d.xrefs || d.duplicates || d.symbols || d;
    if (Array.isArray(list) && list.length > 0) {
      showResultModal('table', list, symName);
    } else {
      showResultModal('json', d, symName);
    }
  } catch (e) {
    showResultModal('json', { error: e.message }, symName);
  }
}

function showResultModal(view, data, symName) {
  let content = '';
  if (view === 'table' && Array.isArray(data)) {
    // Auto-detect columns from first row
    const cols = Object.keys(data[0] || {}).filter(k => k !== 'kind' || true).slice(0, 6);
    content = `
      <div style="overflow-x:auto;max-height:400px;overflow-y:auto">
        <table style="border-collapse:collapse;width:100%;font-size:10px;font-family:var(--fm)">
          <thead>
            <tr>${cols.map(c => `<th style="padding:3px 8px;text-align:left;color:var(--text3);border-bottom:1px solid var(--border);white-space:nowrap">${esc(c)}</th>`).join('')}</tr>
          </thead>
          <tbody>
            ${data.slice(0, 200).map(row => `
              <tr style="cursor:pointer" onclick="${row.file ? `openFile('${esc(row.file)}');closeModal()` : ''}">
                ${cols.map(c => {
                  const v = row[c];
                  const s = v === null || v === undefined ? '' : String(v);
                  if (c === 'kind_str' || c === 'kind') return `<td style="padding:3px 8px"><span class="kb kb-${esc(s)}">${esc(s)}</span></td>`;
                  if (c === 'name') return `<td style="padding:3px 8px;color:var(--text);font-weight:600">${esc(s)}</td>`;
                  if (c === 'file') return `<td style="padding:3px 8px;color:var(--text3);max-width:180px;overflow:hidden;text-overflow:ellipsis">${esc(shortPath(s))}</td>`;
                  return `<td style="padding:3px 8px;color:var(--text2)">${esc(s.slice(0, 60))}</td>`;
                }).join('')}
              </tr>`).join('')}
          </tbody>
        </table>
        ${data.length > 200 ? `<div style="padding:6px;color:var(--text3)">Showing 200 of ${data.length}</div>` : ''}
      </div>`;
  } else {
    // JSON view with syntax highlighting
    const json = JSON.stringify(data, null, 2);
    content = `<pre style="font:10px var(--fm);color:var(--text2);overflow:auto;max-height:400px;background:var(--bg1);padding:8px;border-radius:3px;white-space:pre-wrap;word-break:break-all">${esc(json)}</pre>`;
  }

  showModal(`
    <div style="display:flex;align-items:center;gap:8px;margin-bottom:10px">
      <h3 style="flex:1">${esc(symName || 'Result')}</h3>
      <button class="btn-cancel" onclick="copyResultToClipboard()">📋 Copy</button>
    </div>
    <div id="result-content">${content}</div>
    <div style="display:flex;gap:5px;margin-top:10px">
      <button class="btn-cancel" onclick="closeModal()">Close</button>
      <button class="btn-ok" onclick="exportResultJSON()">↓ Export</button>
    </div>
  `);
  // Store data for export
  window._lastResultData = data;
}

function copyResultToClipboard() {
  navigator.clipboard?.writeText(JSON.stringify(window._lastResultData, null, 2));
}

function exportResultJSON() {
  const blob = new Blob([JSON.stringify(window._lastResultData, null, 2)], { type: 'application/json' });
  const url  = URL.createObjectURL(blob);
  const a    = document.createElement('a');
  a.href = url; a.download = 'kscope-result-' + Date.now() + '.json'; a.click();
  URL.revokeObjectURL(url);
}

// ── Wire popup into every existing entry point ─────────────────────────────────

// Extend ctx:action to open plugin popup from context menu
Bus.on('ctx:action', ({ action, target }) => {
  if (action !== 'send_to_plugin') return;
  if (!target || !target.sym) return;
  showPluginPopup(
    { sym: target.sym, kind: target.kind || 'unknown', type: target.type || '', file: target.file || '', line: target.line || 0 },
    0, 0, true
  );
});

// Expose functions used in onclick attributes
window.copyResultToClipboard = copyResultToClipboard;
window.dispatchPopupAction = dispatchPopupAction;
window.exportResultJSON = exportResultJSON;
window.hidePluginPopup = hidePluginPopup;
window.showPluginPopup = showPluginPopup;
window.submitParamForm = submitParamForm;
