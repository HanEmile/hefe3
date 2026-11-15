// js/structview.js — Struct layout visualizer.
// Displays kernel-side vs userspace-side struct field layouts side by side,
// annotates fields with size, offset, and any store annotations (sizeof_call,
// user_controlled_size, etc.). A "filter" panel that accepts structs via
// the plugin popup and renders them visually.
// Part of github.com/hanemile/kscope
'use strict';

// ── Struct view panel ─────────────────────────────────────────────────────────

async function initStructviewSlot(slot, st) {
  const body   = qs('.sv2-body',  slot);
  const symIn  = qs('.sv2-sym',   slot);
  const goBtn  = qs('.sv2-go',    slot);
  const canvas = qs('.sv2-canvas', slot);
  if (!body) return;

  if (st && st.sym && symIn) symIn.value = st.sym;

  const load = async () => {
    const sym = symIn?.value.trim();
    if (!sym || !canvas) return;

    try {
      // Fetch symbol view (defs + annotations)
      const sv  = await api('/symbol/' + encodeURIComponent(sym) + '/view?version=' + encodeURIComponent(A.activeVer));
      const ann = await api('/annotations?version=' + encodeURIComponent(A.activeVer) + '&symbol=' + encodeURIComponent(sym));

      // Separate kernel and uapi definitions
      const kernelDef = (sv.definitions || []).find(d => d.kind_str === 'kernel' || d.kind_str === 'duplicated');
      const uapiDef   = (sv.definitions || []).find(d => d.kind_str === 'uapi'   || d.kind_str === 'duplicated');

      renderStructCanvas(canvas, sym, kernelDef, uapiDef, ann.annotations || [], sv);

      // Save state
      const pi = Number(slot.dataset.slot);
      if (!isNaN(pi) && ws().panels[pi]) ws().panels[pi].st = { sym };
    } catch (e) {
      const ctx = canvas.getContext('2d');
      if (ctx) { ctx.clearRect(0,0,canvas.width,canvas.height); ctx.fillStyle='var(--red)'; ctx.font='11px monospace'; ctx.fillText('Error: '+e.message, 10, 20); }
    }
  };

  goBtn?.addEventListener('click', load);
  symIn?.addEventListener('keydown', e => { if (e.key === 'Enter') load(); });

  canvas.width  = canvas.offsetWidth  || 600;
  canvas.height = canvas.offsetHeight || 400;
  new ResizeObserver(() => { canvas.width = canvas.offsetWidth; canvas.height = canvas.offsetHeight; if (symIn?.value) load(); }).observe(canvas);

  if (st && st.sym) load();
}

// ── Canvas struct renderer ─────────────────────────────────────────────────────

function renderStructCanvas(canvas, name, kDef, uDef, anns, sv) {
  const ctx = canvas.getContext('2d');
  ctx.clearRect(0, 0, canvas.width, canvas.height);

  // Header
  ctx.fillStyle = '#f0f0f0';
  ctx.font      = 'bold 13px monospace';
  ctx.fillText('struct ' + name, 12, 22);

  // Annotation badges
  const annKeys = [...new Set(anns.map(a => a.key))];
  let bx = 12 + ctx.measureText('struct ' + name).width + 10;
  annKeys.forEach(k => {
    const label = svAnnLabel(k);
    const color = svAnnColor(k);
    ctx.fillStyle = color + '33';
    const tw = ctx.measureText(label).width + 8;
    roundRectFill(ctx, bx, 8, tw, 16, 3);
    ctx.fillStyle = color;
    ctx.font = '700 9px sans-serif';
    ctx.fillText(label, bx + 4, 19);
    ctx.font = 'bold 13px monospace';
    bx += tw + 4;
  });

  // Duplicate warning
  if (sv.dup_peer) {
    ctx.fillStyle = '#ff2d2d';
    ctx.font = '10px sans-serif';
    ctx.fillText('⑂ Kernel/UAPI duplicate: ' + sv.dup_peer, 12, 42);
  }

  const startY = sv.dup_peer ? 58 : 40;
  const colW   = (canvas.width - 24) / 2 - 4;

  // Kernel side
  if (kDef) {
    svDrawSide(ctx, 'Kernel-internal', kDef, anns, 12, startY, colW, canvas);
  } else {
    svNoSide(ctx, 'Kernel-internal', 12, startY, colW);
  }

  // UAPI side
  if (uDef) {
    svDrawSide(ctx, 'UAPI (stable ABI)', uDef, anns, 12 + colW + 8, startY, colW, canvas);
  } else {
    svNoSide(ctx, 'UAPI (stable ABI)', 12 + colW + 8, startY, colW);
  }

  // XRef usage bar
  if (sv.xref_count > 0) {
    const y = canvas.height - 28;
    ctx.fillStyle = '#1a1a1a';
    ctx.fillRect(12, y, canvas.width - 24, 18);
    ctx.fillStyle = '#00aaff';
    ctx.fillRect(12, y, Math.min((canvas.width - 24) * (sv.xref_count / 500), canvas.width - 24), 18);
    ctx.fillStyle = '#888';
    ctx.font = '9px sans-serif';
    ctx.fillText(sv.xref_count + ' xrefs across codebase', 16, y + 12);
  }
}

function svDrawSide(ctx, title, def, anns, x, y, w, canvas) {
  // Side header
  const isUAPI = title.includes('UAPI');
  ctx.fillStyle = isUAPI ? '#ffe00033' : '#00ff7f22';
  roundRectFill(ctx, x, y, w, 20, 3);
  ctx.fillStyle = isUAPI ? '#ffe000' : '#00ff7f';
  ctx.font = '700 10px sans-serif';
  ctx.fillText(title, x + 6, y + 13);
  ctx.fillStyle = '#666';
  ctx.font = '9px monospace';
  ctx.fillText(def.file + ':' + def.line, x + w - ctx.measureText(def.file + ':' + def.line).width - 4, y + 13);

  // Field rows (simulated — real field data would require full AST)
  const fieldY = y + 28;
  const rowH   = 22;
  const fields = svMockFields(def, anns);
  fields.forEach((f, i) => {
    const fy = fieldY + i * rowH;
    if (fy + rowH > (canvas.height - 40)) return;
    const color = f.origin === 'uapi' ? '#ffe000' : f.origin === 'kernel' ? '#00ff7f' : '#aaa';
    // Field background
    ctx.fillStyle = i % 2 === 0 ? '#0e0e0e' : '#111';
    ctx.fillRect(x, fy, w, rowH);
    // Offset badge
    ctx.fillStyle = '#2a2a2a';
    ctx.fillRect(x + 1, fy + 2, 36, rowH - 4);
    ctx.fillStyle = '#555';
    ctx.font = '9px monospace';
    ctx.fillText(f.offset, x + 4, fy + 13);
    // Type
    ctx.fillStyle = '#00e5ff';
    ctx.font = '9px monospace';
    ctx.fillText(f.type, x + 42, fy + 13);
    // Name
    ctx.fillStyle = color;
    ctx.font = '9px monospace';
    const typeW = ctx.measureText(f.type + ' ').width;
    ctx.fillText(f.name, x + 42 + typeW, fy + 13);
    // Size
    ctx.fillStyle = '#444';
    ctx.font = '8px sans-serif';
    ctx.fillText(f.size > 0 ? f.size + 'B' : '?', x + w - 28, fy + 13);
    // Annotations on this field
    if (f.ann) {
      ctx.fillStyle = svAnnColor(f.ann);
      ctx.font = '700 8px sans-serif';
      ctx.fillText('●', x + w - 10, fy + 13);
    }
  });

  if (fields.length === 0) {
    ctx.fillStyle = '#333';
    ctx.font = '9px sans-serif';
    ctx.fillText('(fields extracted after full AST parse)', x + 6, fieldY + 14);
  }
}

function svNoSide(ctx, title, x, y, w) {
  ctx.fillStyle = '#1a1a1a';
  roundRectFill(ctx, x, y, w, 20, 3);
  ctx.fillStyle = '#555';
  ctx.font = '9px sans-serif';
  ctx.fillText(title + ' — not found', x + 6, y + 13);
}

// Mock field list — in production this comes from a full AST parse (tree-sitter)
// For now, show a placeholder row per known xref annotation
function svMockFields(def, anns) {
  const fields = [];
  // Add a representative placeholder
  fields.push({ offset: '+0x00', type: 'u32', name: '(fields)', size: 4, origin: def.kind_str, ann: null });
  anns.filter(a => a.key === 'sizeof_call').forEach(a => {
    fields.push({ offset: '—', type: 'sizeof', name: a.value, size: 0, origin: 'auto', ann: a.key });
  });
  return fields;
}

function svAnnLabel(key) {
  return {
    copy_from_user_site:   'copy_from_user',
    copy_to_user_site:     'copy_to_user',
    sizeof_call:           'sizeof',
    sizeof_too_small:      'sizeof⚠',
    user_controlled_size:  'user-size',
    static_size:           'static',
    dynamic_size:          'dynamic',
    checked:               'checked',
    unchecked:             'UNCHECKED',
  }[key] || key;
}

function svAnnColor(key) {
  return {
    copy_from_user_site:  '#00e5ff',
    copy_to_user_site:    '#cc44ff',
    sizeof_call:          '#ffe000',
    sizeof_too_small:     '#ff2d2d',
    user_controlled_size: '#ff6a00',
    static_size:          '#00ff7f',
    dynamic_size:         '#ffe000',
    checked:              '#00ff7f',
    unchecked:            '#ff2d2d',
  }[key] || '#888';
}

function roundRectFill(ctx, x, y, w, h, r) {
  ctx.beginPath();
  ctx.moveTo(x + r, y);
  ctx.lineTo(x + w - r, y); ctx.arcTo(x+w,y,x+w,y+r,r);
  ctx.lineTo(x + w, y + h - r); ctx.arcTo(x+w,y+h,x+w-r,y+h,r);
  ctx.lineTo(x + r, y + h); ctx.arcTo(x,y+h,x,y+h-r,r);
  ctx.lineTo(x, y + r); ctx.arcTo(x,y,x+r,y,r);
  ctx.closePath();
  ctx.fill();
}

// Allow struct views to receive symbols from the plugin popup
Bus.on('structview:load', ({ sym }) => {
  openPanelWithState('structview', { sym });
});
