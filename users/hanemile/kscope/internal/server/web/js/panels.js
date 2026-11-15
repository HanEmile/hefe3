// js/panels.js — Source viewer, diff, duplicates, and XRef panel logic.
// Includes acme-style symbol link injection into rendered source HTML.
// Part of github.com/hanemile/kscope

// ── Source panel ──────────────────────────────────────────────────────────────
async function loadSrc(slot, path, ver) {
  const scroll = qs('.sv-scroll', slot);
  const symsDiv = qs('.sv-syms', slot);
  if (!scroll) return;
  scroll.innerHTML = '<div style="padding:12px;color:var(--text4);font-size:10px">Loading…</div>';
  if (symsDiv) {
    const cnt = qs('.sym-count', symsDiv);
    if (cnt) cnt.textContent = '';
    qa('.svs-item', symsDiv).forEach(el => el.remove());
  }

  try {
    const d = await api('/file?path=' + encodeURIComponent(path) + '&version=' + encodeURIComponent(ver || A.activeVer));

    // Build symbol name set for link injection
    const symNames = new Set((d.symbols || []).map(s => s.name));
    scroll.innerHTML = injectSymLinks(d.html || '', symNames, path);

    // Populate symbol sidebar — always, from API response
    if (symsDiv) {
      const cnt = qs('.sym-count', symsDiv);
      if (cnt) cnt.textContent = `(${(d.symbols || []).length})`;
      (d.symbols || []).forEach(s => {
        const row = document.createElement('div'); row.className = 'svs-item';
        const xrefBadge = s.xref_count ? `<span class="svs-xref">${s.xref_count}↔</span>` : '';
        row.innerHTML = `<span class="kb kb-${s.kind_str}">${esc(s.type)}</span><span class="svs-name">${esc(s.name)}</span>${xrefBadge}`;
        row.title = `${s.kind_str} · line ${s.line}${s.xref_count ? ` · ${s.xref_count} xrefs` : ''}`;
        row.addEventListener('click', () => jumpToLine(scroll, s.line));
        row.addEventListener('dblclick', () => openSymbolDetail(s.name));
        row.addEventListener('contextmenu', e => {
          e.preventDefault();
          ctxTarget = { sym: s.name, file: path, line: s.line, kind: s.kind_str, type: s.type };
          showCtxMenu(e.clientX, e.clientY, [
            { title: s.name },
            '-',
            { label: 'Jump to line',       action: 'noop',           icon: '↓'  },
            { label: 'Symbol detail',      action: 'symbol_detail',  icon: 'ℹ'  },
            { label: 'XRef usages',        action: 'xref',           icon: '🔍' },
            { label: 'XRef graph',         action: 'xrefgraph',      icon: '◎'  },
            '-',
            { label: 'Send to plugin…',    action: 'send_to_plugin', icon: '⌁'  },
            { label: 'Add to node editor', action: 'addnode',        icon: '◆'  },
            { label: 'Copy name',          action: 'copyname',       icon: '📋' },
          ]);
        });
        symsDiv.appendChild(row);
      });
    }

    // Overlay annotations (copy_from_user, sizeof, etc.) on rendered lines
    overlayAnnotations(scroll, path, ver || A.activeVer);

    // Mark active file in tree
    qa('#ftree .ti:not(.dir):not(.go-up)').forEach(el => {
      const n = qs('.ti-name', el);
      el.classList.toggle('sel', n && n.textContent === path.split('/').pop());
    });

    // Update panel top-bar title and bottom bar
    const leafId = bspLeafIdFromSlot(scroll.closest('[data-leaf-id]') || slot);
    if (leafId != null && typeof bspSetTitle === 'function') {
      const shortPath = path.split('/').slice(-2).join('/');
      const kindBadge = d.symbols && d.symbols.length
        ? `<span class="bsp-tag kb-kernel" style="background:var(--bg3);color:var(--text3)">${d.symbols.length} sym</span>` : '';
      bspSetTitle(leafId, `<b>${esc(shortPath)}</b>${kindBadge}`);

      const bot = document.getElementById('bsp-bot-' + leafId);
      if (bot) {
        const verLabel = esc(ver || A.activeVer || '');
        bot.innerHTML = `
          <span class="bsp-bot-label">${esc(path)}</span>
          <div class="bsp-bot-sep"></div>
          <button class="bsp-bot-btn" onclick="openPanelWithState('xref',{sym:''})">↔ XRef</button>
          <button class="bsp-bot-btn" onclick="openPanelWithState('callgraph',{sym:''})">↕ CallGraph</button>
          <button class="bsp-bot-btn" onclick="openPanelWithState('diff',{path:'${esc(path)}',verA:'${verLabel}',verB:''})">⇆ Diff</button>`;
        bot.classList.add('has-content');
      }
    }
  } catch (e) {
    scroll.innerHTML = `<div style="padding:12px;color:var(--red);font-size:10px">${esc(e.message)}</div>`;
  }
}

// Inject clickable .sym-link spans into rendered source HTML.
// Works by parsing the table into a DocumentFragment and walking text nodes
// inside each .src-cd cell, wrapping known symbol names.
function injectSymLinks(html, symNames, filePath) {
  if (!symNames || symNames.size === 0) return html;
  const tmpl = document.createElement('template');
  tmpl.innerHTML = html;
  const fragment = tmpl.content;
  fragment.querySelectorAll('.src-cd').forEach(cell => wrapSymbolsInCell(cell, symNames, filePath));
  // Serialize back
  const div = document.createElement('div');
  div.appendChild(fragment.cloneNode(true));
  return div.innerHTML;
}

function wrapSymbolsInCell(cell, symNames, filePath) {
  const re = /\b([A-Za-z_]\w{2,})\b/g;
  const walk = node => {
    if (node.nodeType === 3) { // TEXT_NODE
      const text = node.textContent;
      let m, last = 0, frag = null;
      re.lastIndex = 0;
      while ((m = re.exec(text)) !== null) {
        if (!symNames.has(m[1])) continue;
        if (!frag) frag = document.createDocumentFragment();
        frag.appendChild(document.createTextNode(text.slice(last, m.index)));
        const span = document.createElement('span');
        span.className = 'sym-link';
        span.dataset.sym  = m[1];
        span.dataset.file = filePath || '';
        span.textContent  = m[1];
        frag.appendChild(span);
        last = m.index + m[0].length;
      }
      if (frag) {
        frag.appendChild(document.createTextNode(text.slice(last)));
        node.parentNode.replaceChild(frag, node);
      }
    } else if (node.nodeType === 1 && node.tagName !== 'SPAN') {
      [...node.childNodes].forEach(walk);
    }
  };
  [...cell.childNodes].forEach(walk);
}

function symbolCtxItems(name) {
  return [
    { title: name },
    '-',
    { label: 'Symbol detail',         action: 'symbol_detail',  icon: 'ℹ'  },
    { label: 'XRef: find usages',     action: 'xref',           icon: '🔍' },
    { label: 'XRef graph',            action: 'xrefgraph',      icon: '◎'  },
    { label: 'Open definition',       action: 'opendef',        icon: '→'  },
    { label: 'Open in diff',          action: 'opendiff',       icon: '⇆'  },
    '-',
    { label: 'Send to plugin…',       action: 'send_to_plugin', icon: '⌁'  },
    { label: 'Add to node editor',    action: 'addnode',        icon: '◆'  },
    { label: 'Send to search',        action: 'tosearch',       icon: '⌕'  },
    { label: 'Copy name',             action: 'copyname',       icon: '📋' },
  ];
}

function jumpToLine(scrollEl, line) {
  const el = scrollEl.querySelector('#L' + line);
  if (el) {
    el.scrollIntoView({ block: 'center' });
    el.style.outline = '1px solid var(--blue)';
    setTimeout(() => { el.style.outline = ''; }, 2000);
  }
}

// ── Diff panel ────────────────────────────────────────────────────────────────
async function loadDiffSlot(slot, st) {
  // Populate version selectors
  qa('.dv-ver-sel', slot).forEach((sel, i) => {
    sel.innerHTML = '<option value="">— select —</option>' + A.versions.map(v =>
      `<option value="${esc(v.label)}" ${v.label === (i === 0 ? st.verA : st.verB) ? 'selected' : ''}>${esc(v.label)}</option>`
    ).join('');
    sel.addEventListener('change', () => {
      const sels = qa('.dv-ver-sel', slot);
      const newSt = { ...st, verA: sels[0]?.value, verB: sels[1]?.value };
      const pi = Number(slot.dataset.slot);
      ws().panels[pi].st = newSt;
      loadDiffContent(slot, newSt);
    });
  });
  if (st.path) await loadDiffContent(slot, st);
}

async function loadDiffContent(slot, st) {
  const sides = qa('.dv-side', slot);
  if (!st.path || !st.verA || !st.verB) {
    sides.forEach(s => {
      const sc = qs('.dv-scroll', s);
      if (sc) sc.innerHTML = '<div style="padding:12px;color:var(--text3);font-size:10px">Select two versions and open a file.</div>';
    });
    return;
  }

  const d = await api(`/diff?path=${encodeURIComponent(st.path)}&a=${encodeURIComponent(st.verA)}&b=${encodeURIComponent(st.verB)}`);
  const renderSide = (sideEl, sd) => {
    const hd  = qs('.dv-ver',    sideEl);
    const scr = qs('.dv-scroll', sideEl);
    if (hd) hd.textContent = sd.version || '?';
    if (scr) {
      const symNames = new Set((sd.symbols || []).map(s => s.name));
      scr.innerHTML = sd.error
        ? `<div style="padding:12px;color:var(--red);font-size:10px">${esc(sd.error)}</div>`
        : injectSymLinks(sd.html || '', symNames, st.path);
    }
  };
  if (sides[0] && d.a) renderSide(sides[0], d.a);
  if (sides[1] && d.b) renderSide(sides[1], d.b);

  // Synchronized scrolling
  const scrolls = qa('.dv-scroll', slot);
  let syncing = false;
  scrolls.forEach((el, i) => {
    el.addEventListener('scroll', () => {
      if (syncing) return; syncing = true;
      scrolls.forEach((o, j) => { if (j !== i) { o.scrollTop = el.scrollTop; o.scrollLeft = el.scrollLeft; } });
      setTimeout(() => { syncing = false; }, 0);
    });
  });
}

// ── Duplicates panel ──────────────────────────────────────────────────────────
async function loadDupsSlot(slot) {
  const grid   = qs('.dup-grid',       slot); if (!grid) return;
  const filter = qs('.dup-filter-sel', slot);
  const search = qs('.dup-search',     slot);
  let allDups  = [];

  const render = () => {
    const risk = filter?.value || '';
    const q    = (search?.value || '').toLowerCase();
    const dups = allDups.filter(d =>
      (!risk || d.risk === risk) &&
      (!q    || d.name.toLowerCase().includes(q) || d.kernel_file.includes(q) || d.uapi_file.includes(q))
    );
    grid.innerHTML = dups.length
      ? dups.map(d => `
        <div class="dc r${d.risk[0]}" onclick="openFile('${esc(d.kernel_file)}')">
          <div class="dc-name">${esc(d.name)}</div>
          <div class="dc-row"><b>K </b>${esc(d.kernel_file)}</div>
          <div class="dc-row"><b>U </b>${esc(d.uapi_file)}</div>
          <div class="dc-risk">${esc(d.risk)}</div>
        </div>`).join('')
      : '<div style="color:var(--text4);font-size:10px;padding:8px">No duplicates found (indexing may be in progress).</div>';
  };

  grid.innerHTML = '<div style="color:var(--text4);font-size:10px;padding:8px">Loading…</div>';
  const d = await api('/duplicates?version=' + encodeURIComponent(A.activeVer));
  allDups = d.duplicates || [];
  render();
  filter?.addEventListener('change', render);
  search?.addEventListener('input',  render);
  Bus.on('index:done', async () => {
    const d2 = await api('/duplicates?version=' + encodeURIComponent(A.activeVer));
    allDups = d2.duplicates || [];
    render();
  });
}

// ── XRef panel ────────────────────────────────────────────────────────────────
async function xvSearch(btn) {
  const pv   = btn?.closest('.pv'); if (!pv) return;
  const inp  = qs('.xv-input', pv); if (!inp) return;
  const name = inp.value.trim();    if (!name) return;
  const list = qs('.xv-list',  pv); if (!list) return;

  list.innerHTML = '<div style="padding:8px;color:var(--text4);font-size:10px">Loading…</div>';
  const d = await api('/xref/' + encodeURIComponent(name) + '?version=' + encodeURIComponent(A.activeVer));
  list.innerHTML = '';

  if (!(d.xrefs || []).length) {
    list.innerHTML = '<div style="padding:8px;color:var(--text4);font-size:10px">No cross-references found.</div>';
    return;
  }

  // Group by file
  const byFile = {};
  (d.xrefs || []).forEach(x => { (byFile[x.file] = byFile[x.file] || []).push(x); });

  Object.entries(byFile).forEach(([file, refs]) => {
    const fhd = document.createElement('div');
    fhd.className = 'xr-file-hd';
    fhd.textContent = `${file}  (${refs.length})`;
    list.appendChild(fhd);

    refs.forEach(x => {
      const r = document.createElement('div'); r.className = 'xr-row';
      r.innerHTML = `<span class="xr-fn">${esc(x.used_in)}</span><span class="xr-ctx">${esc(x.context)}</span><span class="xr-loc">${x.line}</span>`;
      r.addEventListener('click', () => openFile(x.file));
      r.addEventListener('contextmenu', e => {
        e.preventDefault();
        ctxTarget = { sym: name, file: x.file, line: x.line };
        showCtxMenu(e.clientX, e.clientY, [
          { title: name },
          '-',
          { label: 'Open file',        action: 'opendef',        icon: '→'  },
          { label: 'Symbol detail',    action: 'symbol_detail',  icon: 'ℹ'  },
          { label: 'XRef graph',       action: 'xrefgraph',      icon: '◎'  },
          { label: 'Send to plugin…',  action: 'send_to_plugin', icon: '⌁'  },
          { label: 'Copy name',        action: 'copyname',       icon: '📋' },
        ]);
      });
      list.appendChild(r);
    });
  });
}

async function xvGraph(btn) {
  const pv   = btn?.closest('.pv'); if (!pv) return;
  const inp  = qs('.xv-input', pv); if (!inp) return;
  const name = inp.value.trim();    if (!name) return;
  openPanelWithState('graph', { mode: 'xref', sym: name });
}

// ── Annotation overlay in source view ─────────────────────────────────────────
// After loading a source file, fetch annotations and overlay badges on lines.
async function overlayAnnotations(scroll, path, ver) {
  if (!scroll) return;
  try {
    const d = await api('/annotations?version=' + encodeURIComponent(ver || A.activeVer)
                       + '&file=' + encodeURIComponent(path));
    const anns = d.annotations || [];
    if (!anns.length) return;

    // Group by line
    const byLine = {};
    anns.forEach(a => {
      if (!a.line) return;
      (byLine[a.line] = byLine[a.line] || []).push(a);
    });

    // Find table rows in the rendered HTML and inject badges
    const table = scroll.querySelector('.src-table');
    if (!table) return;
    table.querySelectorAll('tr.src-line').forEach(row => {
      const lnEl = row.querySelector('.src-ln');
      if (!lnEl) return;
      const ln = parseInt(lnEl.textContent, 10);
      const rowAnns = byLine[ln];
      if (!rowAnns || !rowAnns.length) return;

      const cd = row.querySelector('.src-cd');
      if (!cd) return;

      // Remove existing badges
      cd.querySelectorAll('.src-ann-badge').forEach(b => b.remove());

      // Add annotation badges at end of line
      rowAnns.forEach(a => {
        const badge = document.createElement('span');
        badge.className = 'src-ann-badge ' + annBadgeClass(a.key);
        badge.textContent = annBadgeLabel(a.key);
        badge.title = a.note || a.key;
        badge.addEventListener('click', e => {
          e.stopPropagation();
          showModal('<h3>Annotation</h3>'
            + '<p style="font-size:10px;color:var(--text2);margin:6px 0"><code>' + esc(a.key) + '</code></p>'
            + '<p style="font-size:11px;color:var(--text)">' + esc(a.note || a.value || '') + '</p>'
            + '<p style="font-size:9px;color:var(--text3);margin-top:8px">Source: ' + esc(a.source || '') + ' · ' + esc(a.file || '') + ':' + (a.line||0) + '</p>'
            + '<div class="modal-btns"><button class="btn-cancel" onclick="closeModal()">Close</button>'
            + '<button class="btn-ok" onclick="deleteAnnotation(\'' + esc(a.id) + '\')">Delete</button></div>');
        });
        cd.appendChild(badge);
      });

      // Highlight rows with critical annotations
      if (rowAnns.some(a => a.key === 'copy_from_user_site')) row.classList.add('ann-s');
      if (rowAnns.some(a => a.key === 'unchecked'))           row.classList.add('ann-d');
      if (rowAnns.some(a => a.key === 'checked'))             row.classList.add('ann-k');
    });
  } catch (_) { /* annotations are optional — silently skip */ }
}

function annBadgeClass(key) {
  const m = {
    copy_from_user_site:  'ann-copy-from',
    copy_to_user_site:    'ann-copy-to',
    sizeof_call:          'ann-sizeof',
    unchecked:            'ann-unchecked',
    checked:              'ann-checked',
    user_controlled_size: 'ann-user-size',
  };
  return m[key] || '';
}

function annBadgeLabel(key) {
  const m = {
    copy_from_user_site:  '↓copy_from_user',
    copy_to_user_site:    '↑copy_to_user',
    sizeof_call:          'sizeof',
    sizeof_too_small:     '⚠sizeof',
    unchecked:            '⚠unchecked',
    checked:              '✓checked',
    user_controlled_size: '⚠user-size',
  };
  return m[key] || key;
}

async function deleteAnnotation(id) {
  closeModal();
  await fetch('/api/annotations/' + encodeURIComponent(id), { method: 'DELETE' });
}

// Expose functions used in onclick attributes
window.deleteAnnotation = deleteAnnotation;
window.xvGraph = xvGraph;
window.xvSearch = xvSearch;
