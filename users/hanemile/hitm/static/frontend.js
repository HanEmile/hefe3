const flows = new Map();
let flowList;

let currentFlowId;
let interceptedFlowId = null;
let selectedFlowId;
let selectedFullFlow = null; // last fully-fetched flow, used as editor template

// Central tracking address matching your proxy instantiation logic
const ACTIVE_LISTENER = "https://127.0.0.1:9002";

// === Filter Engine ===

// Default filter shown in the bar on load: hide fuzzer/spider traffic so the
// flow list shows live proxy traffic by default. Clear or edit it to see all.
const DEFAULT_FILTER = 'source==proxy';
let currentFilter = DEFAULT_FILTER;
let flowSortKey = 'id';
let flowSortAsc = false;

// parseFilter is memoized by filter string: the same string is parsed once.
// This keeps regex work off hot paths (per-SSE-event filtering, poll renders).
const _filterCache = new Map();
function parseFilter(filterStr) {
  if (!filterStr || !filterStr.trim()) return null;
  if (_filterCache.has(filterStr)) return _filterCache.get(filterStr);
  const clauses = [];
  let ok = true;
  const parts = filterStr.trim().split(/\s+and\s+|\s{2,}/i);
  for (const part of parts) {
    const m = part.match(/^(\w+)\s*(==|!=|>=|<=|>|<|~=|!~=|contains|!contains)\s*(.+)$/i);
    if (!m) { ok = false; break; }
    clauses.push({ field: m[1].toLowerCase(), op: m[2].toLowerCase(), value: m[3] });
  }
  const result = ok ? clauses : null;
  if (_filterCache.size > 200) _filterCache.clear(); // bound the cache
  _filterCache.set(filterStr, result);
  return result;
}

function matchesFilter(item, clauses) {
  if (!clauses) return true;
  for (const c of clauses) {
    const raw = item[c.field];
    const val = (raw === undefined || raw === null) ? '' : String(raw);
    const cmp = c.value;
    const numVal = Number(val);
    const numCmp = Number(cmp);
    switch (c.op) {
      case '==': if (val !== cmp && !(isFinite(numVal) && isFinite(numCmp) && numVal === numCmp)) return false; break;
      case '!=': if (val === cmp || (isFinite(numVal) && isFinite(numCmp) && numVal === numCmp)) return false; break;
      case '>=': if (!(numVal >= numCmp)) return false; break;
      case '<=': if (!(numVal <= numCmp)) return false; break;
      case '>':  if (!(numVal > numCmp)) return false; break;
      case '<':  if (!(numVal < numCmp)) return false; break;
      case '~=': case 'contains': if (!val.toLowerCase().includes(cmp.toLowerCase())) return false; break;
      case '!~=': case '!contains': if (val.toLowerCase().includes(cmp.toLowerCase())) return false; break;
    }
  }
  return true;
}

function getFilterClauses() {
  return parseFilter(currentFilter);
}

function setFilter(str) {
  currentFilter = str;
  const input = document.getElementById('filter-input');
  if (input) input.value = str;
  applyFilter();
}

function appendFilter(clause) {
  if (currentFilter.trim()) {
    setFilter(currentFilter.trim() + '  ' + clause);
  } else {
    setFilter(clause);
  }
}

function clearFilter() {
  setFilter('');
}

function applyFilter() {
  const clauses = getFilterClauses();
  const input = document.getElementById('filter-input');
  if (input) {
    input.classList.toggle('filter-error', currentFilter.trim() && !clauses);
  }

  if (!flowList) return;
  const sorted = getSortedFlows();
  const FLOW_RENDER_CAP = 1000;
  const frag = document.createDocumentFragment();
  let shown = 0, matched = 0;
  for (const flow of sorted) {
    if (!matchesFilter(flow, clauses)) continue;
    matched++;
    if (shown >= FLOW_RENDER_CAP) continue;
    frag.appendChild(createFlowRow(flow));
    shown++;
  }
  flowList.replaceChildren(frag);
  if (matched > FLOW_RENDER_CAP) {
    const note = document.createElement('div');
    note.className = 'flow-cap-note';
    note.textContent = `showing ${FLOW_RENDER_CAP} of ${matched} — narrow the filter`;
    flowList.appendChild(note);
  }
}

function getSortedFlows() {
  const arr = Array.from(flows.values());
  arr.sort((a, b) => {
    let av = a[flowSortKey], bv = b[flowSortKey];
    if (av === undefined) av = '';
    if (bv === undefined) bv = '';
    const an = Number(av), bn = Number(bv);
    let cmp;
    if (isFinite(an) && isFinite(bn)) cmp = an - bn;
    else cmp = String(av).localeCompare(String(bv));
    return flowSortAsc ? cmp : -cmp;
  });
  return arr;
}

function createFlowRow(flow) {
  const div = document.createElement('div');
  div.id = "flow-item-" + flow.id;
  div.className = 'flow-item';

  function addCell(name, content, flex) {
    const el = document.createElement('div');
    el.className = 'flow-item-' + name;
    el.textContent = content;
    el.title = 'Click to filter, right-click for menu';
    if (flex) el.style.flex = flex;
    // Right-click → context menu
    el.addEventListener('contextmenu', (e) => {
      e.preventDefault();
      e.stopPropagation();
      showCtxMenu(e.clientX, e.clientY, name, content, flow);
    });
    // Left-click → context menu too (Wireshark-style), but only when modifier held,
    // otherwise the row click (inspect) takes over.
    el.addEventListener('click', (e) => {
      if (e.altKey || e.ctrlKey || e.metaKey) {
        e.preventDefault();
        e.stopPropagation();
        showCtxMenu(e.clientX, e.clientY, name, content, flow);
      }
    });
    div.appendChild(el);
    return el;
  }

  addCell('id', flow.id);
  addCell('method', flow.method);
  addCell('url', flow.url, '5');
  const statusEl = addCell('status', flow.status || '-');

  if (flow.status >= 400) statusEl.style.color = "#ff0000";
  else if (flow.status >= 300) statusEl.style.color = "#ffa500";
  else if (flow.status >= 200) statusEl.style.color = "#00ff00";

  if (flow.source && flow.source !== 'proxy') {
    const srcEl = addCell('source', flow.source);
    srcEl.style.color = '#888';
    srcEl.style.fontSize = '0.8em';
  }

  div.onclick = () => {
    inspectFlow(flow.id); // fetches full flow and seeds repeat/intercept/fuzzer editors
    const flowDisplay = document.getElementById("selected-flow");
    if (flowDisplay) flowDisplay.textContent = `${flow.id} ${flow.method} ${flow.url} ${flow.status || ''}`;
  };

  return div;
}

// === Context Menu ===

function showCtxMenu(x, y, field, value, flow) {
  removeCtxMenu();
  const menu = document.createElement('div');
  menu.className = 'ctx-menu';
  menu.id = 'ctx-menu';
  menu.style.left = x + 'px';
  menu.style.top = y + 'px';

  const displayVal = String(value).length > 40 ? String(value).substring(0, 40) + '...' : value;

  function addItem(label, action) {
    const item = document.createElement('div');
    item.className = 'ctx-menu-item';
    item.textContent = label;
    item.addEventListener('click', (e) => {
      e.stopPropagation();
      removeCtxMenu();
      action();
    });
    menu.appendChild(item);
  }

  addItem(`Apply as filter: ${field}==${displayVal}`, () => appendFilter(`${field}==${value}`));
  addItem(`Exclude: ${field}!=${displayVal}`, () => appendFilter(`${field}!=${value}`));

  if (field === 'status' && isFinite(Number(value))) {
    menu.appendChild(Object.assign(document.createElement('div'), {className: 'ctx-menu-sep'}));
    addItem(`Status >= ${value}`, () => appendFilter(`status>=${value}`));
    addItem(`Status < ${value}`, () => appendFilter(`status<${value}`));
  }

  if (field === 'url') {
    menu.appendChild(Object.assign(document.createElement('div'), {className: 'ctx-menu-sep'}));
    try {
      const u = new URL(value);
      addItem(`Filter host: url~=${u.host}`, () => appendFilter(`url~=${u.host}`));
      if (u.pathname !== '/') addItem(`Filter path: url~=${u.pathname}`, () => appendFilter(`url~=${u.pathname}`));
    } catch(e) {}
  }

  menu.appendChild(Object.assign(document.createElement('div'), {className: 'ctx-menu-sep'}));
  addItem('Copy value', () => navigator.clipboard.writeText(String(value)));

  document.body.appendChild(menu);

  const rect = menu.getBoundingClientRect();
  if (rect.right > window.innerWidth) menu.style.left = (window.innerWidth - rect.width - 4) + 'px';
  if (rect.bottom > window.innerHeight) menu.style.top = (window.innerHeight - rect.height - 4) + 'px';

  // Dismiss on any outside mousedown / scroll / escape. Defer binding so the
  // event that opened the menu doesn't immediately close it.
  setTimeout(() => {
    document.addEventListener('mousedown', dismissCtxOnOutside, true);
    window.addEventListener('scroll', removeCtxMenu, true);
    document.addEventListener('keydown', dismissCtxOnEscape, true);
  }, 0);
}

function dismissCtxOnOutside(e) {
  const m = document.getElementById('ctx-menu');
  if (m && !m.contains(e.target)) removeCtxMenu();
}

function dismissCtxOnEscape(e) {
  if (e.key === 'Escape') removeCtxMenu();
}

function removeCtxMenu() {
  const m = document.getElementById('ctx-menu');
  if (m) m.remove();
  document.removeEventListener('mousedown', dismissCtxOnOutside, true);
  window.removeEventListener('scroll', removeCtxMenu, true);
  document.removeEventListener('keydown', dismissCtxOnEscape, true);
}

// === Flow Header (sortable columns) ===

function initFlowHeader() {
  const header = document.getElementById('flow-header');
  if (!header) return;
  header.innerHTML = '';

  const cols = [
    { key: 'id', label: '#', flex: '' },
    { key: 'method', label: 'Method', flex: '' },
    { key: 'url', label: 'URL', flex: '5' },
    { key: 'status', label: 'Status', flex: '' },
  ];

  for (const col of cols) {
    const el = document.createElement('div');
    el.className = col.flex ? 'flow-item-url' : '';
    el.textContent = col.label + (flowSortKey === col.key ? (flowSortAsc ? ' ▲' : ' ▼') : '');
    el.onclick = () => {
      if (flowSortKey === col.key) flowSortAsc = !flowSortAsc;
      else { flowSortKey = col.key; flowSortAsc = true; }
      initFlowHeader();
      applyFilter();
    };
    header.appendChild(el);
  }
}

async function loadHistory() {
  try {
    const activeListener = encodeURIComponent(ACTIVE_LISTENER);
    const res = await fetch(`/history?l=${activeListener}`);
    
    if (!res.ok) {
      const errTxt = await res.text();
      console.error("History endpoint error:", errTxt);
      return;
    }

    const history = await res.json();
    // Populate the map first, then render ONCE. Calling applyFilter per row is
    // O(n^2) and locks the browser with thousands of restored flows.
    history.forEach(flow => flows.set(flow.id, flow));
    applyFilter();
  } catch (err) {
    console.error("Failed to load history:", err);
  }
}

// renderFlowItem handles a single live flow (from SSE). It updates the map and
// incrementally inserts/updates just that row instead of rebuilding the list.
function renderFlowItem(flow) {
  const isNew = !flows.has(flow.id);
  flows.set(flow.id, flow);
  if (!flowList) return;

  const clauses = getFilterClauses();
  const matches = matchesFilter(flow, clauses);
  const existing = document.getElementById('flow-item-' + flow.id);

  if (!matches) {
    if (existing) existing.remove();
    return;
  }
  const row = createFlowRow(flow);
  if (existing) {
    existing.replaceWith(row);
  } else if (isNew) {
    // New rows go to the top when sorting by id descending (the default),
    // otherwise just append; a full re-sort only happens on explicit sort.
    if (flowSortKey === 'id' && !flowSortAsc) {
      flowList.insertBefore(row, flowList.firstChild);
    } else {
      flowList.appendChild(row);
    }
  } else {
    flowList.appendChild(row);
  }
}

function autoPopulateIntercept(flow) {
  interceptedFlowId = flow.id;
  const textarea = document.querySelector('#intercept-view textarea') || document.getElementById('intercept-request');
  if (textarea) {
    textarea.value = flow.request || '';
  }
}

const es = new EventSource("/events");
es.onmessage = (e) => {
  try {
    const flow = JSON.parse(e.data);
    renderFlowItem(flow);

    const interceptBtn = document.getElementById("enable-intercept");
    if (!flow.response && interceptBtn && interceptBtn.style.display === "none") {
      autoPopulateIntercept(flow);
    }
    if (currentFlowId == flow.id) {
      renderDetails(flow);
    }
  } catch (err) {
    console.error("Error parsing event data stream block:", err);
  }
};
es.onerror = (e) => {
  console.error('EventSource connection error:', e);
};

// Sitemap filter, seeded like the main flow list (hide fuzzer/spider traffic).
let sitemapFilter = DEFAULT_FILTER;
let sitemapFilterTimer = null;

// Build the static sitemap chrome (filter bar + tree container) once.
function buildSitemapChrome(view) {
  view.innerHTML = '';

  const fbar = document.createElement('div');
  fbar.id = 'sitemap-filter-bar';
  fbar.className = 'sitemap-filter-bar';
  const icon = document.createElement('span');
  icon.className = 'sitemap-filter-icon';
  icon.textContent = '⌕';
  fbar.appendChild(icon);
  const input = document.createElement('input');
  input.id = 'sitemap-filter-input';
  input.type = 'text';
  input.placeholder = 'filter flows  e.g. status==404  method==POST  url~=login';
  input.value = sitemapFilter;
  input.oninput = () => {
    if (sitemapFilterTimer) clearTimeout(sitemapFilterTimer);
    sitemapFilterTimer = setTimeout(() => {
      sitemapFilterTimer = null;
      sitemapFilter = input.value;
      const clauses = parseFilter(sitemapFilter);
      input.classList.toggle('filter-error', sitemapFilter.trim() !== '' && clauses === null);
      buildSitemap();
    }, 200);
  };
  fbar.appendChild(input);
  view.appendChild(fbar);

  const treeWrap = document.createElement('div');
  treeWrap.id = 'sitemap-tree';
  treeWrap.className = 'sitemap-tree';
  view.appendChild(treeWrap);
}

function buildSitemap() {
  const sitemapView = document.getElementById('sitemap-view');
  if (!sitemapView) return;
  if (!sitemapView.dataset.initialized) {
    sitemapView.dataset.initialized = '1';
    buildSitemapChrome(sitemapView);
  }
  const treeWrap = document.getElementById('sitemap-tree');
  if (!treeWrap) return;
  treeWrap.innerHTML = '';

  const clauses = parseFilter(sitemapFilter);
  const tree = {};

  flows.forEach(flow => {
    if (clauses && !matchesFilter(flow, clauses)) return;
    try {
      const url = new URL(flow.url);
      const host = url.host;
      const pathParts = url.pathname.split('/').filter(p => p !== "");

      if (!tree[host]) tree[host] = { _isDir: true, children: {} };

      let currentNode = tree[host].children;
      pathParts.forEach((part, index) => {
        if (!currentNode[part]) {
          currentNode[part] = { _isDir: true, children: {} };
        }
        if (index === pathParts.length - 1) {
          currentNode[part]._isLeaf = true;
          currentNode[part]._flowId = flow.id;
          currentNode[part].status = flow.status;
          currentNode[part].method = flow.method;
        }
        currentNode = currentNode[part].children;
      });
    } catch (e) {
      console.error("Invalid URL in sitemap build:", flow.url);
    }
  });

  const container = document.createElement('div');

  Object.keys(tree).sort().forEach(host => {
    container.appendChild(renderNode(host, tree[host]));
  });

  treeWrap.appendChild(container);
}

function renderNode(name, data) {
  if (data._isLeaf && Object.keys(data.children).length === 0) {
    const leafDiv = document.createElement('div');
    leafDiv.className = 'sitemap-leaf';
    leafDiv.textContent = name + " | " + data.method + " " + (data.status || '');
    leafDiv.style.cursor = "pointer";
    leafDiv.style.padding = "2px 5px";
    leafDiv.style.marginLeft = "3.5ex";
    if (data.status >= 400) {
      leafDiv.style.color = "#ff0000";
    } else if (data.status >= 300) {
      leafDiv.style.color = "#ffa500";
    } else if (data.status >= 200) {
      leafDiv.style.color = "#00ff00";
    }

    leafDiv.onclick = () => {
      inspectFlow(data._flowId);   // loads request/response into #details
      showTab('inspect');          // and switch to the Inspect tab so it's visible
    };
    return leafDiv;
  }

  const details = document.createElement('details');
  details.style.marginLeft = "1.5ex";

  const summary = document.createElement('summary');
  summary.style.cursor = "pointer";
  summary.style.padding = "2px 5px";
  const childKeys = Object.keys(data.children).sort();
  summary.textContent = name + ' (' + childKeys.length + ')';

  // This path node is itself a visited page (has a flow) AND has sub-pages.
  // Add a small "open" link in the summary that loads its request/response
  // without toggling the tree. Clicking the rest of the summary still expands.
  if (data._isLeaf && data._flowId != null) {
    const open = document.createElement('span');
    open.className = 'sitemap-open';
    open.textContent = ' ⤴ ' + (data.method || '') + ' ' + (data.status || '');
    if (data.status >= 400) open.style.color = '#ff0000';
    else if (data.status >= 300) open.style.color = '#ffa500';
    else if (data.status >= 200) open.style.color = '#00ff00';
    open.onclick = (ev) => {
      ev.preventDefault();
      ev.stopPropagation(); // don't toggle the <details>
      inspectFlow(data._flowId);
      showTab('inspect');
    };
    summary.appendChild(open);
  }

  details.appendChild(summary);

  // Lazily build children only when this node is first expanded. This keeps the
  // initial sitemap render O(hosts) instead of O(all path nodes) — critical with
  // tens of thousands of URLs.
  let built = false;
  const buildChildren = () => {
    if (built) return;
    built = true;
    const frag = document.createDocumentFragment();
    for (const key of childKeys) {
      frag.appendChild(renderNode(key, data.children[key]));
    }
    details.appendChild(frag);
  };
  details.addEventListener('toggle', () => { if (details.open) buildChildren(); });

  // Auto-open host-level nodes, but build their children eagerly only at that
  // one level (not recursively) so the page is responsive.
  if (name.includes('.')) {
    details.open = true;
    buildChildren();
  }

  return details;
}

async function loadCATab() {
  const caView = document.getElementById('ca-view');
  if (!caView) return;
  caView.innerHTML = '';

  try {
    const res = await fetch('/ca/list');
    const files = await res.json();

    const rootSection = document.createElement('div');
    rootSection.innerHTML = `
        <div style="background:#2d2d2d; padding:15px; margin:10px; border-radius:4px; border-left:4px solid #f1e05a;">
            <h4>Primary CA Certificate</h4>
            <p>Download and install this certificate in your browser/OS to decrypt HTTPS traffic.</p>
            <button onclick="window.location='/ca/download?name=HITM-Proxy.pem'" class="btn">Download HITM-Proxy.pem (Public)</button>
        </div>
    `;
    caView.appendChild(rootSection);

    const listSection = document.createElement('div');
    listSection.style.padding = "10px";
    listSection.innerHTML = `<h4>Generated Site Certificates</h4>`;

    const table = document.createElement('table');
    table.style.width = "100%";
    table.innerHTML = `<tr><th align="left">Filename</th><th align="right">Action</th></tr>`;

    files.forEach(file => {
      const row = table.insertRow();
      const nameCell = row.insertCell();
      const code = document.createElement('code');
      code.textContent = file;
      nameCell.appendChild(code);
      const actionCell = row.insertCell();
      actionCell.align = "right";
      const link = document.createElement('a');
      link.href = "/ca/download?name=" + encodeURIComponent(file);
      link.style.color = "#85e89d";
      link.textContent = "Download";
      actionCell.appendChild(link);
    });

    listSection.appendChild(table);
    caView.appendChild(listSection);

  } catch (err) {
    const errP = document.createElement('p');
    errP.style.color = "red";
    errP.textContent = "Error loading certs: " + err;
    caView.appendChild(errP);
  }
}

function updateUrlState(tab) {
  const flowId = selectedFlowId || '';
  window.location.hash = flowId ? `${tab}/${flowId}` : `${tab}`;
}

async function showTab(tab) {
  console.log("Showing tab:", tab);

  const tabs = document.querySelectorAll('.tab');
  tabs.forEach(t => t.classList.remove('active'));

  const activeTab = document.getElementById(`${tab}-tab`);
  if (activeTab) activeTab.classList.add('active');

  const views = document.querySelectorAll('[id$="-view"]');
  views.forEach(v => v.style.display = 'none');

  // Leaving the map tab: stop the force-sim + WebGL rAF loops so they don't run hidden.
  if (tab !== 'map') {
    if (typeof stopForceSim === 'function') stopForceSim();
    if (typeof mv2StopGLLoop === 'function') mv2StopGLLoop();
  }

  const selectedView = document.getElementById(`${tab}-view`);
  if (selectedView) selectedView.style.display = 'flex';

  updateUrlState(tab);

  if (tab === 'repeat') {
    const textarea = document.getElementById('repeat-request');
    if (textarea) {
      if (!currentFlowId) {
        textarea.placeholder = 'Select a flow from the sidebar to populate the repeat...';
        return;
      }
      const flow = flows.get(currentFlowId);
      if (flow && flow.request) textarea.value = flow.request;
    }
  }

  if (tab === 'intercept') {
    const textarea = document.getElementById('intercept-request');
    if (textarea) {
      if (!currentFlowId) {
        textarea.placeholder = 'Enable interception and send a request to the proxy';
        return;
      }
      const flow = flows.get(currentFlowId);
      if (flow && flow.request) textarea.value = flow.request;
    }
  }

  if (tab === "sitemap") buildSitemap();
  if (tab === 'ca') loadCATab();
  if (tab === 'fuzz') loadFuzzTab();
  if (tab === 'spider') loadSpiderTab();
  if (tab === 'map') loadMapTab();
  if (tab === 'jobs') loadJobsTab();
  if (tab === 'listeners') loadListenersTab();
}

function addElement(tag, text, parent, className = "", id = "") {
  const el = document.createElement(tag);
  if (className) el.className = className;
  if (id) el.id = id;
  if (text) el.textContent = text;
  if (parent) parent.appendChild(el);
  return el;
}

function splitHeadersBody(raw) {
  if (!raw) return { headers: '', body: '' };
  const sep = raw.indexOf('\r\n\r\n');
  if (sep === -1) {
    const sep2 = raw.indexOf('\n\n');
    if (sep2 === -1) return { headers: raw, body: '' };
    return { headers: raw.substring(0, sep2), body: raw.substring(sep2 + 2) };
  }
  return { headers: raw.substring(0, sep), body: raw.substring(sep + 4) };
}

function makeCollapsible(title, content, parent, open) {
  const el = document.createElement('details');
  if (open) el.open = true;
  const summary = document.createElement('summary');
  summary.textContent = title;
  summary.className = 'inspect-section-header';
  el.appendChild(summary);
  const pre = document.createElement('pre');
  pre.textContent = content;
  el.appendChild(pre);
  parent.appendChild(el);
  return el;
}

function renderDetails(flow) {
  const details = document.getElementById('details');
  if (!details) return;

  details.innerHTML = "";

  const reqParsed = splitHeadersBody(flow.request || "");
  const resParsed = splitHeadersBody(flow.response || "");
  const reqBody = flow.requestBody || "";
  const resBody = flow.responseBody || "";

  // Request
  addElement("div", `Request ${flow.requestTime || ''}`, details, "h3", "inspect-request");
  if (reqParsed.headers) makeCollapsible('Headers', reqParsed.headers, details, true);
  if (reqBody) makeCollapsible('Body', reqBody, details, true);

  // Response
  addElement("div", `Response ${flow.responseTime || ''}`, details, "h3", "inspect-response");
  if (!flow.response && !resBody) {
    addElement("p", "Waiting for response...", details);
  } else {
    if (resParsed.headers) makeCollapsible('Headers', resParsed.headers, details, true);
    if (resBody) makeCollapsible('Body', resBody, details, true);
  }
}

async function inspectFlow(id) {
  const details = document.getElementById('details');
  if (!details) return;

  document.querySelectorAll('.flow-item').forEach(el => el.classList.remove('active-flow'));
  const targetEl = document.getElementById("flow-item-" + id);
  if (targetEl) targetEl.classList.add('active-flow');
  currentFlowId = id;

  try {
    details.innerHTML = "Loading...";
    const activeListener = encodeURIComponent(ACTIVE_LISTENER); 
    const raw = await fetch(`/flow?id=${id}&l=${activeListener}`);
    
    if (!raw.ok) {
      const errorText = await raw.text();
      throw new Error(`Server returned ${raw.status}: ${errorText}`);
    }

    const textData = await raw.text();
    const flow = JSON.parse(textData);
    selectedFullFlow = flow;
    renderDetails(flow);
    populateEditorsFromFlow(flow);

  } catch (err) {
    details.innerHTML = "<span style='color:#ff0000; font-weight:bold;'>Error loading flow: " + err.message + "</span>";
    console.error("Inspect flow crash details:", err);
  }

  updateUrlState('inspect');
}

// populateEditorsFromFlow seeds the repeat / intercept / fuzzer editors with the
// selected flow's raw request so it can be used as a template for alterations.
function populateEditorsFromFlow(flow) {
  const raw = flow.request || '';
  if (!raw) return;

  const repeatArea = document.getElementById('repeat-request');
  if (repeatArea) repeatArea.value = raw;

  const interceptArea = document.getElementById('intercept-request');
  // Don't clobber a request the user is actively editing mid-interception.
  if (interceptArea && !interceptedFlowId) interceptArea.value = raw;

  const fuzzArea = document.getElementById('fuzz-request');
  if (fuzzArea) fuzzArea.value = raw;
}

function copyToRepeater(rawReq) {
  const repeatArea = document.getElementById('repeat-request');
  if (repeatArea) repeatArea.value = rawReq;
  showTab('repeat');
}

async function sendRepeat() {
  const respContainer = document.getElementById('repeat-response');
  const reqArea = document.getElementById('repeat-request');
  if (respContainer) respContainer.innerText = "Loading...";
  if (!reqArea) return;

  const raw = reqArea.value;
  const res = await fetch('/repeat', { method: 'POST', body: raw });
  const resp = await res.text();
  if (respContainer) respContainer.innerText = resp;
}

function loadStateFromUrl() {
  const hash = window.location.hash.substring(1);
  if (!hash) return;

  const [tab, flowId] = hash.split('/');
  if (tab) {
    showTab(tab);
  } else {
    showTab('inspect');
  }

  if (flowId) {
    const selectedFlowId = parseInt(flowId);
    inspectFlow(selectedFlowId);
  }
}

document.addEventListener('DOMContentLoaded', () => {
  flowList = document.getElementById('flow-list');

  const filterInput = document.getElementById('filter-input');
  if (filterInput) {
    filterInput.value = currentFilter; // seed the bar with the default filter
    // Debounce: typing in the bar shouldn't re-sort+re-render 20k flows per keystroke.
    let filterDebounce;
    filterInput.addEventListener('input', (e) => {
      currentFilter = e.target.value;
      clearTimeout(filterDebounce);
      filterDebounce = setTimeout(applyFilter, 150);
    });
    filterInput.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') { clearFilter(); filterInput.blur(); }
    });
  }

  initFlowHeader();
  loadHistory();
  loadStateFromUrl();
});

async function enableIntercept() {
  document.getElementById("enable-intercept").style.display = "none";
  document.getElementById("disable-intercept").style.display = "";
  const header = document.getElementById("header");
  if (header) header.style = "background: #ff0000; color: white;";
  
  const activeListener = encodeURIComponent(ACTIVE_LISTENER);
  await fetch(`/intercept?enable=true&l=${activeListener}`, { method: 'POST', body: '' });
}

async function disableIntercept() {
  document.getElementById("enable-intercept").style.display = "";
  document.getElementById("disable-intercept").style.display = "none";
  const header = document.getElementById("header");
  if (header) header.style = "background: #dddddd;";
  
  const activeListener = encodeURIComponent(ACTIVE_LISTENER);
  await fetch(`/intercept?enable=false&l=${activeListener}`, { method: 'POST', body: '' });
}

async function forwardRequest() {
  if (!interceptedFlowId) {
    console.error("No flow ID to forward");
    return;
  }

  const textarea = document.getElementById('intercept-request');
  const raw = textarea ? textarea.value : "";
  const activeListener = encodeURIComponent(ACTIVE_LISTENER);
  
  const res = await fetch(`/forward?id=${interceptedFlowId}&l=${activeListener}`, {
    method: 'POST',
    body: raw
  });

  if (res.ok) {
    interceptedFlowId = null;
    if (textarea) textarea.value = "";
  }
}

async function dropRequest() {
  if (!interceptedFlowId) return;

  const activeListener = encodeURIComponent(ACTIVE_LISTENER);
  await fetch(`/drop?id=${interceptedFlowId}&l=${activeListener}`, { method: 'POST' });

  const textarea = document.querySelector('#intercept-view textarea') || document.getElementById('intercept-request');
  if (textarea) textarea.value = "";
  interceptedFlowId = null;
}

// === Fuzzer ===

const SECLISTS_BASE = 'https://raw.githubusercontent.com/danielmiessler/SecLists/master/Discovery/Web-Content/';
const SECLISTS_PRESETS = [
  'raft-small-files.txt', 'raft-small-files-lowercase.txt',
  'raft-small-directories.txt', 'raft-small-directories-lowercase.txt',
  'raft-small-extensions.txt', 'raft-small-extensions-lowercase.txt',
  'raft-medium-files.txt', 'raft-medium-files-lowercase.txt',
  'raft-medium-directories.txt', 'raft-medium-directories-lowercase.txt',
  'raft-medium-extensions.txt', 'raft-medium-extensions-lowercase.txt',
  'raft-medium-words.txt', 'raft-medium-words-lowercase.txt',
  'raft-large-files.txt', 'raft-large-files-lowercase.txt',
  'raft-large-directories.txt', 'raft-large-directories-lowercase.txt',
  'raft-large-extensions.txt', 'raft-large-extensions-lowercase.txt',
  'raft-large-words.txt', 'raft-large-words-lowercase.txt',
];

let fuzzPayloads = [];

function makeInput(id, placeholder, width) {
  const input = document.createElement('input');
  input.id = id;
  if (placeholder) input.placeholder = placeholder;
  input.className = 'opt-input';
  if (width) input.style.width = width;
  return input;
}

// Build a labelled field (label + input/select) for the options grid.
function optField(label, el) {
  const wrap = document.createElement('label');
  wrap.className = 'opt-field';
  const span = document.createElement('span');
  span.className = 'opt-label';
  span.textContent = label;
  wrap.appendChild(span);
  wrap.appendChild(el);
  return wrap;
}

function optInput(id, placeholder) {
  const i = document.createElement('input');
  i.id = id;
  i.className = 'opt-input';
  if (placeholder) i.placeholder = placeholder;
  return i;
}

function optSelect(id, options) {
  const s = document.createElement('select');
  s.id = id;
  s.className = 'opt-input';
  for (const o of options) {
    const opt = document.createElement('option');
    opt.value = o;
    opt.textContent = o;
    s.appendChild(opt);
  }
  return s;
}

function optCheckbox(id, label) {
  const wrap = document.createElement('label');
  wrap.className = 'opt-check';
  const cb = document.createElement('input');
  cb.type = 'checkbox';
  cb.id = id;
  wrap.appendChild(cb);
  wrap.appendChild(document.createTextNode(' ' + label));
  return wrap;
}

function loadFuzzTab() {
  const view = document.getElementById('fuzz-view');
  if (!view) return;
  if (view.dataset.initialized) return;
  view.dataset.initialized = "1";

  view.innerHTML = '';

  // ---- Config (left column) ----
  const config = document.createElement('div');
  config.className = 'rows fuzz-config';
  config.style.gap = '8px';

  // Request template
  config.appendChild(labelDiv('Request template (use marker for payload positions). Leave empty to build from URL below.'));
  const reqArea = document.createElement('textarea');
  reqArea.id = 'fuzz-request';
  reqArea.className = 'fuzz-req-area';
  reqArea.placeholder = 'GET /FUZZ HTTP/1.1\nHost: example.com\n';
  if (selectedFullFlow && selectedFullFlow.request) reqArea.value = selectedFullFlow.request;
  config.appendChild(reqArea);

  // Collapsible option groups
  config.appendChild(optionGroup('HTTP Options', [
    optField('Target URL (-u)', optInput('fuzz-url', 'https://example.com/FUZZ')),
    optField('Method (-X)', optInput('fuzz-method', 'GET')),
    optField('Marker', Object.assign(optInput('fuzz-marker'), { value: 'FUZZ' })),
    optField('Host', optInput('fuzz-host', 'example.com')),
    optField('POST data (-d)', optInput('fuzz-data', 'a=b&c=FUZZ')),
    optField('Cookies (-b)', optInput('fuzz-cookies', 'NAME=VALUE; NAME2=VALUE2')),
    optField('SNI (-sni)', optInput('fuzz-sni', '')),
    optField('Proxy (-x)', optInput('fuzz-proxy', 'http://127.0.0.1:8080')),
    optField('Timeout s (-timeout)', Object.assign(optInput('fuzz-timeout'), { value: '10', type: 'number' })),
  ], [
    optCheckbox('fuzz-http2', 'HTTP/2 (-http2)'),
    optCheckbox('fuzz-ignore-body', 'Ignore body (-ignore-body)'),
    optCheckbox('fuzz-follow-redir', 'Follow redirects (-r)'),
  ]));

  const headersArea = document.createElement('textarea');
  headersArea.id = 'fuzz-headers';
  headersArea.style.height = '60px';
  headersArea.placeholder = 'One header per line, e.g.\nAuthorization: Bearer xxx\nX-Custom: FUZZ';
  const headersGroup = optionGroup('Headers (-H, one per line)', [], []);
  headersGroup.querySelector('.opt-body').appendChild(headersArea);
  config.appendChild(headersGroup);

  config.appendChild(optionGroup('General', [
    optField('Threads (-t)', Object.assign(optInput('fuzz-threads'), { value: '40', type: 'number' })),
    optField('Rate req/s (-rate)', Object.assign(optInput('fuzz-rate'), { value: '0', type: 'number' })),
    optField('Delay min s (-p)', Object.assign(optInput('fuzz-delay-min'), { value: '0', type: 'number', step: '0.1' })),
    optField('Delay max s', Object.assign(optInput('fuzz-delay-max'), { value: '0', type: 'number', step: '0.1' })),
    optField('Max time s (-maxtime)', Object.assign(optInput('fuzz-maxtime'), { value: '0', type: 'number' })),
    optField('Extensions (-e)', optInput('fuzz-extensions', '.php,.html')),
    optField('Mode (-mode)', optSelect('fuzz-mode', ['sniper', 'clusterbomb', 'pitchfork'])),
  ], [
    optCheckbox('fuzz-stop-403', 'Stop on >95% 403 (-sf)'),
    optCheckbox('fuzz-stop-err', 'Stop on errors (-se)'),
  ]));

  config.appendChild(optionGroup('Matchers', [
    optField('Status (-mc)', Object.assign(optInput('fuzz-mc'), { value: '200-299,301,302,307,401,403,405,500' })),
    optField('Lines (-ml)', optInput('fuzz-ml')),
    optField('Words (-mw)', optInput('fuzz-mw')),
    optField('Size (-ms)', optInput('fuzz-ms')),
    optField('Regex (-mr)', optInput('fuzz-mr')),
    optField('Mode (-mmode)', optSelect('fuzz-mmode', ['or', 'and'])),
  ], []));

  config.appendChild(optionGroup('Filters', [
    optField('Status (-fc)', optInput('fuzz-fc')),
    optField('Lines (-fl)', optInput('fuzz-fl')),
    optField('Words (-fw)', optInput('fuzz-fw')),
    optField('Size (-fs)', optInput('fuzz-fs')),
    optField('Regex (-fr)', optInput('fuzz-fr')),
    optField('Mode (-fmode)', optSelect('fuzz-fmode', ['or', 'and'])),
  ], []));

  // ---- Payload source ----
  config.appendChild(buildPayloadSource());

  // Start button
  const startBtn = document.createElement('button');
  startBtn.textContent = 'Start Fuzzing';
  startBtn.onclick = startFuzz;
  config.appendChild(startBtn);

  view.appendChild(config);

  // ---- Results ----
  const results = document.createElement('div');
  results.id = 'fuzz-results';
  results.className = 'rows';
  view.appendChild(results);
}

function labelDiv(text) {
  const d = document.createElement('div');
  d.textContent = text;
  d.style.color = '#aaa';
  d.style.fontSize = '12px';
  return d;
}

// optionGroup builds a collapsible <details> with a grid of fields and a row of checkboxes.
function optionGroup(title, fields, checks) {
  const det = document.createElement('details');
  det.className = 'opt-group';
  const sum = document.createElement('summary');
  sum.textContent = title;
  det.appendChild(sum);

  const body = document.createElement('div');
  body.className = 'opt-body';

  if (fields.length) {
    const grid = document.createElement('div');
    grid.className = 'opt-grid';
    for (const f of fields) grid.appendChild(f);
    body.appendChild(grid);
  }
  if (checks.length) {
    const row = document.createElement('div');
    row.className = 'opt-checks';
    for (const c of checks) row.appendChild(c);
    body.appendChild(row);
  }
  det.appendChild(body);
  return det;
}

function buildPayloadSource() {
  const section = optionGroup('Payloads (-w)', [], []);
  section.open = true;
  const body = section.querySelector('.opt-body');

  const status = document.createElement('div');
  status.id = 'fuzz-payload-status';
  status.style.color = '#888';
  status.textContent = 'No payloads loaded';
  body.appendChild(status);

  const tabs = document.createElement('div');
  tabs.className = 'cols';
  tabs.style.gap = '4px';
  tabs.style.margin = '6px 0';

  const btns = {};
  for (const t of ['preset', 'url', 'file', 'manual']) {
    const b = document.createElement('button');
    b.textContent = { preset: 'SecLists', url: 'From URL', file: 'Upload', manual: 'Manual' }[t];
    b.style.width = 'auto';
    b.style.padding = '2px 10px';
    b.onclick = () => showPayloadPanel(t, btns, panel);
    tabs.appendChild(b);
    btns[t] = b;
  }
  body.appendChild(tabs);

  const panel = document.createElement('div');
  panel.id = 'fuzz-src-panel';
  body.appendChild(panel);

  showPayloadPanel('preset', btns, panel);
  return section;
}

function showPayloadPanel(type, btns, panel) {
  panel.innerHTML = '';
  Object.values(btns).forEach(b => b.style.background = '#444');
  btns[type].style.background = '#666';

  if (type === 'preset') {
    const select = optSelect('fuzz-preset', SECLISTS_PRESETS);
    select.style.width = '100%';
    panel.appendChild(select);
    const load = document.createElement('button');
    load.textContent = 'Load Wordlist';
    load.style.marginTop = '4px';
    load.onclick = () => fetchWordlistUrl(SECLISTS_BASE + select.value);
    panel.appendChild(load);
  } else if (type === 'url') {
    const row = document.createElement('div');
    row.className = 'cols';
    row.style.gap = '4px';
    const inp = makeInput('fuzz-wordlist-url', 'https://...wordlist.txt', '');
    inp.style.flex = '1';
    row.appendChild(inp);
    const load = document.createElement('button');
    load.textContent = 'Fetch';
    load.style.width = 'auto';
    load.style.padding = '2px 12px';
    load.onclick = () => fetchWordlistUrl(inp.value);
    row.appendChild(load);
    panel.appendChild(row);
  } else if (type === 'file') {
    const fi = document.createElement('input');
    fi.type = 'file';
    fi.accept = '.txt,.lst,.csv';
    fi.style.color = '#ccc';
    fi.onchange = (e) => {
      const file = e.target.files[0];
      if (!file) return;
      const reader = new FileReader();
      reader.onload = (ev) => {
        fuzzPayloads = ev.target.result.split('\n').map(l => l.trim()).filter(l => l && !l.startsWith('#'));
        updatePayloadStatus();
      };
      reader.readAsText(file);
    };
    panel.appendChild(fi);
  } else if (type === 'manual') {
    const ta = document.createElement('textarea');
    ta.style.height = '80px';
    ta.placeholder = 'One payload per line...';
    panel.appendChild(ta);
    const apply = document.createElement('button');
    apply.textContent = 'Apply';
    apply.style.marginTop = '4px';
    apply.onclick = () => {
      fuzzPayloads = ta.value.split('\n').map(l => l.trim()).filter(l => l);
      updatePayloadStatus();
    };
    panel.appendChild(apply);
  }
}

function updatePayloadStatus() {
  const el = document.getElementById('fuzz-payload-status');
  if (el) {
    el.textContent = fuzzPayloads.length + ' payloads loaded';
    el.style.color = fuzzPayloads.length > 0 ? '#85e89d' : '#888';
  }
}

async function fetchWordlistUrl(url) {
  if (!url) { alert('Enter a URL'); return; }
  const status = document.getElementById('fuzz-payload-status');
  if (status) { status.textContent = 'Fetching...'; status.style.color = '#ffa500'; }
  try {
    const res = await fetch('/fuzz/fetch-wordlist', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url })
    });
    if (!res.ok) throw new Error(await res.text());
    const data = await res.json();
    fuzzPayloads = data.payloads || [];
    updatePayloadStatus();
  } catch (err) {
    if (status) { status.textContent = 'Error: ' + err.message; status.style.color = '#ff0000'; }
  }
}

function val(id) { const e = document.getElementById(id); return e ? e.value : ''; }
function num(id) { const v = parseFloat(val(id)); return isFinite(v) ? v : 0; }
function checked(id) { const e = document.getElementById(id); return e ? e.checked : false; }

async function startFuzz() {
  const rawRequest = val('fuzz-request');
  const url = val('fuzz-url');

  if ((!rawRequest && !url) || !fuzzPayloads.length) {
    alert('Provide a request template OR a target URL, and load payloads first');
    return;
  }

  const cfg = {
    rawRequest,
    url,
    marker: val('fuzz-marker') || 'FUZZ',
    host: val('fuzz-host'),
    scheme: 'https',
    listener: ACTIVE_LISTENER,
    payloads: fuzzPayloads,

    method: val('fuzz-method') || 'GET',
    data: val('fuzz-data'),
    cookies: val('fuzz-cookies'),
    sni: val('fuzz-sni'),
    proxyURL: val('fuzz-proxy'),
    timeoutSec: num('fuzz-timeout') || 10,
    http2: checked('fuzz-http2'),
    ignoreBody: checked('fuzz-ignore-body'),
    followRedir: checked('fuzz-follow-redir'),
    headers: val('fuzz-headers').split('\n').map(h => h.trim()).filter(Boolean),

    threads: Math.max(1, num('fuzz-threads') || 40),
    rate: num('fuzz-rate'),
    delayMin: num('fuzz-delay-min'),
    delayMax: num('fuzz-delay-max'),
    maxTimeSec: num('fuzz-maxtime'),
    stopOn403: checked('fuzz-stop-403'),
    stopOnErr: checked('fuzz-stop-err'),
    extensions: val('fuzz-extensions'),
    mode: val('fuzz-mode') || 'sniper',

    matchCodes: val('fuzz-mc'),
    matchLines: val('fuzz-ml'),
    matchWords: val('fuzz-mw'),
    matchSize: val('fuzz-ms'),
    matchRegex: val('fuzz-mr'),
    matchMode: val('fuzz-mmode') || 'or',

    filterCodes: val('fuzz-fc'),
    filterLines: val('fuzz-fl'),
    filterWords: val('fuzz-fw'),
    filterSize: val('fuzz-fs'),
    filterRegex: val('fuzz-fr'),
    filterMode: val('fuzz-fmode') || 'or',
  };

  const res = await fetch('/fuzz', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(cfg)
  });
  const data = await res.json();
  if (data.jobId) pollFuzzResults(data.jobId);
}

async function pollFuzzResults(jobId) {
  const container = document.getElementById('fuzz-results');
  if (!container) return;
  container.innerHTML = '';

  const controls = document.createElement('div');
  controls.className = 'cols';
  controls.style.gap = '8px';
  controls.style.alignItems = 'center';
  controls.style.padding = '4px';

  const statusDiv = document.createElement('div');
  statusDiv.style.flex = '1';
  controls.appendChild(statusDiv);

  const filterInput = document.createElement('input');
  filterInput.placeholder = 'Filter: statusCode>=400  length>1000  payload~=admin  matched==true';
  filterInput.className = 'opt-input';
  filterInput.style.flex = '2';
  controls.appendChild(filterInput);

  const onlyMatched = document.createElement('label');
  onlyMatched.className = 'opt-check';
  const omcb = document.createElement('input');
  omcb.type = 'checkbox';
  onlyMatched.appendChild(omcb);
  onlyMatched.appendChild(document.createTextNode(' only matched'));
  controls.appendChild(onlyMatched);

  container.appendChild(controls);

  const table = document.createElement('table');
  table.style.width = '100%';
  table.style.borderCollapse = 'collapse';
  const thead = document.createElement('thead');
  thead.style.position = 'sticky';
  thead.style.top = '0';
  thead.style.background = '#1e1e1e';
  table.appendChild(thead);
  const tbody = document.createElement('tbody');
  table.appendChild(tbody);
  container.appendChild(table);

  let sortKey = null, sortAsc = true, filter = '', all = [];
  const RENDER_CAP = 500;

  const cols = [
    { key: 'payload', label: 'Payload' },
    { key: 'statusCode', label: 'Status' },
    { key: 'length', label: 'Length' },
    { key: 'words', label: 'Words' },
    { key: 'lines', label: 'Lines' },
    { key: 'durationMs', label: 'ms' },
    { key: 'matched', label: 'Match' },
    { key: 'error', label: 'Error' },
  ];

  function renderHeader() {
    thead.innerHTML = '';
    const tr = document.createElement('tr');
    for (const col of cols) {
      const th = document.createElement('th');
      th.style.textAlign = 'left';
      th.style.padding = '4px';
      th.style.cursor = 'pointer';
      th.style.userSelect = 'none';
      th.style.borderBottom = '1px solid #555';
      th.textContent = col.label + (sortKey === col.key ? (sortAsc ? ' ▲' : ' ▼') : '');
      th.onclick = () => {
        if (sortKey === col.key) sortAsc = !sortAsc;
        else { sortKey = col.key; sortAsc = true; }
        renderHeader(); renderRows();
      };
      tr.appendChild(th);
    }
    thead.appendChild(tr);
  }

  function renderRows() {
    const clauses = parseFilter(filter);
    let filtered = all;
    if (omcb.checked) filtered = filtered.filter(r => r.matched);
    if (clauses) filtered = filtered.filter(r => matchesFilter(r, clauses));

    if (sortKey) {
      filtered = filtered.slice().sort((a, b) => {
        let av = a[sortKey], bv = b[sortKey];
        if (av === undefined) av = '';
        if (bv === undefined) bv = '';
        const an = Number(av), bn = Number(bv);
        let cmp = (isFinite(an) && isFinite(bn)) ? an - bn : String(av).localeCompare(String(bv));
        return sortAsc ? cmp : -cmp;
      });
    }

    const shown = filtered.slice(0, RENDER_CAP);
    const frag = document.createDocumentFragment();
    for (const r of shown) {
      const row = document.createElement('tr');
      row.style.borderBottom = '1px solid #333';
      if (r.matched) row.style.background = 'rgba(133,232,157,0.06)';

      function addCell(key, text, color) {
        const td = document.createElement('td');
        td.textContent = text;
        td.style.padding = '2px 4px';
        if (color) td.style.color = color;
        td.addEventListener('contextmenu', (e) => {
          e.preventDefault(); e.stopPropagation();
          showCtxMenu(e.clientX, e.clientY, key, text, r);
        });
        row.appendChild(td);
      }
      addCell('payload', r.payload);
      const sc = r.statusCode;
      addCell('statusCode', sc || '-', sc >= 400 ? '#ff0000' : sc >= 300 ? '#ffa500' : sc >= 200 ? '#00ff00' : null);
      addCell('length', r.length || '-');
      addCell('words', r.words || '-');
      addCell('lines', r.lines || '-');
      addCell('durationMs', r.durationMs || '-');
      addCell('matched', r.matched ? '✓' : '', r.matched ? '#85e89d' : null);
      addCell('error', r.error || '', r.error ? '#ff6666' : null);
      frag.appendChild(row);
    }
    tbody.replaceChildren(frag);

    if (filtered.length > RENDER_CAP) {
      const note = document.createElement('tr');
      const td = document.createElement('td');
      td.colSpan = cols.length;
      td.style.padding = '6px';
      td.style.color = '#ffa500';
      td.style.textAlign = 'center';
      td.textContent = `Showing first ${RENDER_CAP} of ${filtered.length} — narrow with a filter or "only matched".`;
      note.appendChild(td);
      tbody.appendChild(note);
    }
  }

  filterInput.addEventListener('input', () => { filter = filterInput.value; renderRows(); });
  omcb.addEventListener('change', renderRows);
  renderHeader();

  let lastDone = -1;
  let matchedCount = 0;
  const poll = async () => {
    const jobs = await (await fetch('/jobs')).json();
    const job = jobs.find(j => j.id === jobId);
    if (!job) return;
    // Only refetch results when progress actually changed — avoids transferring
    // and re-parsing the entire (10k+) result set every poll tick.
    if (job.done !== lastDone) {
      lastDone = job.done;
      all = await (await fetch(`/jobs/results?id=${jobId}`)).json();
      matchedCount = 0;
      for (const r of all) if (r.matched) matchedCount++;
      renderRows();
    }
    statusDiv.textContent = `Job #${jobId}: ${job.status} (${job.done}/${job.total}) — ${matchedCount} matched`;
    if (job.status === 'running') {
      setTimeout(poll, job.total > 2000 ? 1500 : 600);
    }
  };
  poll();
}

// === Spider ===

function loadSpiderTab() {
  const view = document.getElementById('spider-view');
  if (!view) return;
  if (view.dataset.initialized) return;
  view.dataset.initialized = "1";

  view.innerHTML = '';

  const config = document.createElement('div');
  config.className = 'rows fuzz-config';
  config.style.gap = '8px';

  config.appendChild(labelDiv('Sites to crawl (-s / -S, one per line):'));
  const sites = document.createElement('textarea');
  sites.id = 'spider-sites';
  sites.className = 'fuzz-req-area';
  sites.style.height = '70px';
  sites.placeholder = 'https://example.com\nhttps://test.example.com';
  config.appendChild(sites);

  config.appendChild(optionGroup('Crawl', [
    optField('Threads (-t)', Object.assign(optInput('spider-threads'), { value: '1', type: 'number' })),
    optField('Concurrency (-c)', Object.assign(optInput('spider-concurrent'), { value: '5', type: 'number' })),
    optField('Depth (-d, 0=∞)', Object.assign(optInput('spider-depth'), { value: '1', type: 'number' })),
    optField('Delay s (-k)', Object.assign(optInput('spider-delay'), { value: '0', type: 'number' })),
    optField('Random delay s (-K)', Object.assign(optInput('spider-rdelay'), { value: '0', type: 'number' })),
    optField('Timeout s (-m)', Object.assign(optInput('spider-timeout'), { value: '10', type: 'number' })),
    optField('Max URLs', Object.assign(optInput('spider-maxurls'), { value: '5000', type: 'number' })),
  ], [
    optCheckbox('spider-js', 'Linkfinder in JS (--js)'),
    optCheckbox('spider-subs', 'Include subdomains (--subs)'),
    optCheckbox('spider-sitemap', 'Crawl sitemap.xml (--sitemap)'),
    optCheckbox('spider-robots', 'Crawl robots.txt (--robots)'),
    optCheckbox('spider-base', 'Base / HTML only (-B)'),
    optCheckbox('spider-noredirect', 'No redirect (--no-redirect)'),
  ]));

  config.appendChild(optionGroup('Request', [
    optField('User-Agent (-u)', optSelectFree('spider-ua', ['web', 'mobi'], 'web')),
    optField('Proxy (-p)', optInput('spider-proxy', 'http://127.0.0.1:8080')),
    optField('Cookie (--cookie)', optInput('spider-cookie', 'a=b; c=d')),
  ], []));

  const headers = document.createElement('textarea');
  headers.id = 'spider-headers';
  headers.style.height = '50px';
  headers.placeholder = 'One header per line (-H)';
  const hg = optionGroup('Headers (-H)', [], []);
  hg.querySelector('.opt-body').appendChild(headers);
  config.appendChild(hg);

  config.appendChild(optionGroup('Scope', [
    optField('Blacklist regex (--blacklist)', optInput('spider-blacklist', '\\.(png|jpg|css)$')),
    optField('Whitelist regex (--whitelist)', optInput('spider-whitelist')),
    optField('Whitelist domain (--whitelist-domain)', optInput('spider-wdomain')),
  ], []));

  // checkbox defaults: js + robots on (gospider defaults)
  config.querySelector('#spider-js') && (config.querySelector('#spider-js').checked = true);
  config.querySelector('#spider-robots') && (config.querySelector('#spider-robots').checked = true);

  const startBtn = document.createElement('button');
  startBtn.textContent = 'Start Crawl';
  startBtn.onclick = startSpider;
  config.appendChild(startBtn);

  view.appendChild(config);

  const results = document.createElement('div');
  results.id = 'spider-results';
  results.className = 'rows';
  view.appendChild(results);

  // ensure defaults applied after append
  const js = document.getElementById('spider-js'); if (js) js.checked = true;
  const rob = document.getElementById('spider-robots'); if (rob) rob.checked = true;
}

// A select that also allows free-text (renders select + text input fallback).
function optSelectFree(id, options, def) {
  const i = document.createElement('input');
  i.id = id;
  i.className = 'opt-input';
  i.value = def || '';
  i.setAttribute('list', id + '-list');
  const dl = document.createElement('datalist');
  dl.id = id + '-list';
  for (const o of options) {
    const opt = document.createElement('option');
    opt.value = o;
    dl.appendChild(opt);
  }
  const wrap = document.createElement('span');
  wrap.appendChild(i);
  wrap.appendChild(dl);
  return wrap;
}

async function startSpider() {
  const sites = val('spider-sites').split('\n').map(s => s.trim()).filter(Boolean);
  if (!sites.length) { alert('Enter at least one site'); return; }

  const cfg = {
    sites,
    listener: ACTIVE_LISTENER,
    proxyURL: val('spider-proxy'),
    userAgent: val('spider-ua') || 'web',
    cookie: val('spider-cookie'),
    headers: val('spider-headers').split('\n').map(h => h.trim()).filter(Boolean),
    blacklist: val('spider-blacklist'),
    whitelist: val('spider-whitelist'),
    whitelistDomain: val('spider-wdomain'),
    threads: Math.max(1, num('spider-threads') || 1),
    concurrent: Math.max(1, num('spider-concurrent') || 5),
    depth: num('spider-depth') === 0 ? -1 : num('spider-depth'),
    delaySec: num('spider-delay'),
    randomDelaySec: num('spider-rdelay'),
    timeoutSec: num('spider-timeout') || 10,
    maxUrls: num('spider-maxurls') || 5000,
    js: checked('spider-js'),
    subs: checked('spider-subs'),
    sitemap: checked('spider-sitemap'),
    robots: checked('spider-robots'),
    baseOnly: checked('spider-base'),
    noRedirect: checked('spider-noredirect'),
  };

  const res = await fetch('/spider', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(cfg)
  });
  const data = await res.json();
  if (data.jobId) pollSpiderResults(data.jobId);
  else alert('Failed to start crawl: ' + JSON.stringify(data));
}

async function pollSpiderResults(jobId) {
  const container = document.getElementById('spider-results');
  if (!container) return;
  container.innerHTML = '';

  const controls = document.createElement('div');
  controls.className = 'cols';
  controls.style.gap = '8px';
  controls.style.alignItems = 'center';
  controls.style.padding = '4px';
  const statusDiv = document.createElement('div');
  statusDiv.style.flex = '1';
  controls.appendChild(statusDiv);
  const filterInput = document.createElement('input');
  filterInput.placeholder = 'Filter: statusCode==200  payload~=/api  length>500';
  filterInput.className = 'opt-input';
  filterInput.style.flex = '2';
  controls.appendChild(filterInput);
  container.appendChild(controls);

  const table = document.createElement('table');
  table.style.width = '100%';
  table.style.borderCollapse = 'collapse';
  const thead = document.createElement('thead');
  thead.style.position = 'sticky';
  thead.style.top = '0';
  thead.style.background = '#1e1e1e';
  table.appendChild(thead);
  const tbody = document.createElement('tbody');
  table.appendChild(tbody);
  container.appendChild(table);

  let sortKey = null, sortAsc = true, filter = '', all = [];
  const RENDER_CAP = 500;
  const cols = [
    { key: 'payload', label: 'URL' },
    { key: 'statusCode', label: 'Status' },
    { key: 'length', label: 'Length' },
    { key: 'durationMs', label: 'ms' },
  ];

  function renderHeader() {
    thead.innerHTML = '';
    const tr = document.createElement('tr');
    for (const col of cols) {
      const th = document.createElement('th');
      th.style.textAlign = 'left'; th.style.padding = '4px';
      th.style.cursor = 'pointer'; th.style.userSelect = 'none';
      th.style.borderBottom = '1px solid #555';
      th.textContent = col.label + (sortKey === col.key ? (sortAsc ? ' ▲' : ' ▼') : '');
      th.onclick = () => {
        if (sortKey === col.key) sortAsc = !sortAsc;
        else { sortKey = col.key; sortAsc = true; }
        renderHeader(); renderRows();
      };
      tr.appendChild(th);
    }
    thead.appendChild(tr);
  }

  function renderRows() {
    const clauses = parseFilter(filter);
    let filtered = clauses ? all.filter(r => matchesFilter(r, clauses)) : all;
    if (sortKey) {
      filtered = filtered.slice().sort((a, b) => {
        let av = a[sortKey], bv = b[sortKey];
        if (av === undefined) av = ''; if (bv === undefined) bv = '';
        const an = Number(av), bn = Number(bv);
        let cmp = (isFinite(an) && isFinite(bn)) ? an - bn : String(av).localeCompare(String(bv));
        return sortAsc ? cmp : -cmp;
      });
    }
    const frag = document.createDocumentFragment();
    for (const r of filtered.slice(0, RENDER_CAP)) {
      const row = document.createElement('tr');
      row.style.borderBottom = '1px solid #333';
      function addCell(key, text, color) {
        const td = document.createElement('td');
        td.textContent = text;
        td.style.padding = '2px 4px';
        if (color) td.style.color = color;
        td.addEventListener('contextmenu', (e) => {
          e.preventDefault(); e.stopPropagation();
          showCtxMenu(e.clientX, e.clientY, key, text, r);
        });
        row.appendChild(td);
      }
      addCell('payload', r.payload);
      const sc = r.statusCode;
      addCell('statusCode', sc || '-', sc >= 400 ? '#ff0000' : sc >= 300 ? '#ffa500' : sc >= 200 ? '#00ff00' : null);
      addCell('length', r.length || '-');
      addCell('durationMs', r.durationMs || '-');
      frag.appendChild(row);
    }
    tbody.replaceChildren(frag);
    if (filtered.length > RENDER_CAP) {
      const note = document.createElement('tr');
      const td = document.createElement('td');
      td.colSpan = cols.length; td.style.padding = '6px'; td.style.color = '#ffa500'; td.style.textAlign = 'center';
      td.textContent = `Showing first ${RENDER_CAP} of ${filtered.length} URLs — narrow with a filter.`;
      note.appendChild(td); tbody.appendChild(note);
    }
  }

  filterInput.addEventListener('input', () => { filter = filterInput.value; renderRows(); });
  renderHeader();

  let lastResults = -1;
  const poll = async () => {
    const jobs = await (await fetch('/jobs')).json();
    const job = jobs.find(j => j.id === jobId);
    if (!job) return;
    statusDiv.textContent = `Crawl #${jobId}: ${job.status} — ${job.results} URLs found`;
    // Only refetch when the result count changed.
    if (job.results !== lastResults) {
      lastResults = job.results;
      all = await (await fetch(`/jobs/results?id=${jobId}`)).json();
      renderRows();
    }
    if (job.status === 'running') setTimeout(poll, job.results > 2000 ? 1500 : 1000);
  };
  poll();
}

// === Jobs ===

async function loadJobsTab() {
  const view = document.getElementById('jobs-view');
  if (!view) return;
  view.innerHTML = '';
  view.style.padding = '10px';
  view.style.overflowY = 'auto';

  const title = document.createElement('div');
  title.textContent = 'Jobs';
  title.style.fontWeight = 'bold';
  title.style.marginBottom = '8px';
  view.appendChild(title);

  try {
    const res = await fetch('/jobs');
    const jobs = await res.json();

    if (!jobs || jobs.length === 0) {
      const empty = document.createElement('div');
      empty.textContent = 'No jobs yet. Start an intrude attack to create one.';
      empty.style.color = '#888';
      view.appendChild(empty);
      return;
    }

    const table = document.createElement('table');
    table.style.width = '100%';
    table.style.borderCollapse = 'collapse';

    const thead = document.createElement('thead');
    thead.innerHTML = '<tr><th>ID</th><th>Type</th><th>Status</th><th>Progress</th><th>Results</th><th>Created</th><th>Actions</th></tr>';
    table.appendChild(thead);

    const tbody = document.createElement('tbody');
    jobs.forEach(job => {
      const row = document.createElement('tr');
      row.style.borderBottom = '1px solid #333';

      const idCell = document.createElement('td');
      idCell.textContent = job.id;
      idCell.style.padding = '4px';
      row.appendChild(idCell);

      const typeCell = document.createElement('td');
      typeCell.textContent = job.type;
      typeCell.style.padding = '4px';
      row.appendChild(typeCell);

      const statusCell = document.createElement('td');
      statusCell.textContent = job.status;
      statusCell.style.padding = '4px';
      if (job.status === 'running') statusCell.style.color = '#ffa500';
      else if (job.status === 'completed') statusCell.style.color = '#00ff00';
      else if (job.status === 'cancelled') statusCell.style.color = '#ff0000';
      row.appendChild(statusCell);

      const progressCell = document.createElement('td');
      progressCell.textContent = `${job.done}/${job.total}`;
      progressCell.style.padding = '4px';
      row.appendChild(progressCell);

      const resultsCell = document.createElement('td');
      resultsCell.textContent = job.results;
      resultsCell.style.padding = '4px';
      row.appendChild(resultsCell);

      const createdCell = document.createElement('td');
      createdCell.textContent = job.createdAt;
      createdCell.style.padding = '4px';
      row.appendChild(createdCell);

      const actionsCell = document.createElement('td');
      actionsCell.style.padding = '4px';
      if (job.status === 'running') {
        const cancelBtn = document.createElement('button');
        cancelBtn.textContent = 'Cancel';
        cancelBtn.style.width = 'auto';
        cancelBtn.style.padding = '2px 8px';
        cancelBtn.onclick = async () => {
          await fetch(`/jobs/cancel?id=${job.id}`, { method: 'POST' });
          loadJobsTab();
        };
        actionsCell.appendChild(cancelBtn);
      }
      const viewBtn = document.createElement('button');
      viewBtn.textContent = 'View';
      viewBtn.style.width = 'auto';
      viewBtn.style.padding = '2px 8px';
      viewBtn.style.marginLeft = '4px';
      viewBtn.onclick = () => {
        if (job.type === 'spider') {
          showTab('spider');
          pollSpiderResults(job.id);
        } else {
          showTab('fuzz');
          pollFuzzResults(job.id);
        }
      };
      actionsCell.appendChild(viewBtn);
      row.appendChild(actionsCell);

      tbody.appendChild(row);
    });
    table.appendChild(tbody);
    view.appendChild(table);
  } catch (err) {
    const errDiv = document.createElement('div');
    errDiv.style.color = 'red';
    errDiv.textContent = 'Error loading jobs: ' + err;
    view.appendChild(errDiv);
  }
}

// === Listeners ===

async function loadListenersTab() {
  const view = document.getElementById('listeners-view');
  if (!view) return;
  view.innerHTML = '';
  view.style.padding = '10px';
  view.style.gap = '10px';

  const title = document.createElement('div');
  title.textContent = 'Active Listeners';
  title.style.fontWeight = 'bold';
  title.style.marginBottom = '8px';
  view.appendChild(title);

  try {
    const res = await fetch('/listeners');
    const listeners = await res.json();

    const table = document.createElement('table');
    table.style.width = '100%';
    table.style.borderCollapse = 'collapse';
    table.style.marginBottom = '16px';

    const thead = document.createElement('thead');
    thead.innerHTML = '<tr><th style="text-align:left;padding:4px;">Address</th><th style="text-align:left;padding:4px;">Scheme</th><th style="text-align:left;padding:4px;">Key</th></tr>';
    table.appendChild(thead);

    const tbody = document.createElement('tbody');
    if (listeners) {
      listeners.forEach(l => {
        const row = document.createElement('tr');
        row.style.borderBottom = '1px solid #333';

        const addrCell = document.createElement('td');
        addrCell.textContent = l.host;
        addrCell.style.padding = '4px';
        row.appendChild(addrCell);

        const schemeCell = document.createElement('td');
        schemeCell.textContent = l.scheme;
        schemeCell.style.padding = '4px';
        row.appendChild(schemeCell);

        const keyCell = document.createElement('td');
        keyCell.textContent = l.key;
        keyCell.style.padding = '4px';
        keyCell.style.color = '#888';
        row.appendChild(keyCell);

        tbody.appendChild(row);
      });
    }
    table.appendChild(tbody);
    view.appendChild(table);
  } catch (err) {
    const errDiv = document.createElement('div');
    errDiv.style.color = 'red';
    errDiv.textContent = 'Error loading listeners: ' + err;
    view.appendChild(errDiv);
  }

  const addSection = document.createElement('div');
  addSection.className = 'rows';
  addSection.style.gap = '8px';
  addSection.style.padding = '12px';
  addSection.style.border = '1px solid #444';
  addSection.style.borderRadius = '4px';

  const addTitle = document.createElement('div');
  addTitle.textContent = 'Create New Listener';
  addTitle.style.fontWeight = 'bold';
  addSection.appendChild(addTitle);

  const formRow = document.createElement('div');
  formRow.className = 'cols';
  formRow.style.gap = '8px';
  formRow.style.alignItems = 'center';

  const addrInput = document.createElement('input');
  addrInput.id = 'new-listener-addr';
  addrInput.placeholder = '127.0.0.1:9003';
  addrInput.style.background = '#2d2d2d';
  addrInput.style.color = '#85e89d';
  addrInput.style.border = '1px solid #555';
  addrInput.style.padding = '4px 8px';
  addrInput.style.fontFamily = 'monospace';
  addrInput.style.flex = '1';
  formRow.appendChild(addrInput);

  const typeSelect = document.createElement('select');
  typeSelect.id = 'new-listener-type';
  typeSelect.style.background = '#2d2d2d';
  typeSelect.style.color = '#85e89d';
  typeSelect.style.border = '1px solid #555';
  typeSelect.style.padding = '4px 8px';
  typeSelect.style.fontFamily = 'monospace';
  const httpOpt = document.createElement('option');
  httpOpt.value = 'http';
  httpOpt.textContent = 'HTTP';
  typeSelect.appendChild(httpOpt);
  const httpsOpt = document.createElement('option');
  httpsOpt.value = 'https';
  httpsOpt.textContent = 'HTTPS';
  typeSelect.appendChild(httpsOpt);
  formRow.appendChild(typeSelect);

  const createBtn = document.createElement('button');
  createBtn.textContent = 'Create';
  createBtn.style.width = 'auto';
  createBtn.style.padding = '4px 16px';
  createBtn.onclick = async () => {
    const addr = addrInput.value;
    const type_ = typeSelect.value;
    if (!addr) { alert('Enter an address'); return; }
    await fetch(`/newListener?addr=${encodeURIComponent(addr)}&type=${type_}`, { method: 'POST' });
    loadListenersTab();
  };
  formRow.appendChild(createBtn);

  addSection.appendChild(formRow);
  view.appendChild(addSection);
}

// ===================================================================
// Map tab — renders the proxy state graph (GET /map) as a layered
// left->right SVG, similar to the Fuzz Map. Pure JS, no libraries.
// ===================================================================

const SVG_NS = 'http://www.w3.org/2000/svg';
const MAP_PAGE_W = 150;      // page-node rect width
const MAP_PAGE_H = 28;       // page-node rect height
const MAP_STATUS_R = 16;     // status-node pill radius

// --- Force-simulation parameters ---
const FS_THETA = 0.8;        // Barnes-Hut opening angle
const FS_K_REPULSE = 6000;   // repulsion strength (k_repulse * mass / dist^2)
const FS_K_SPRING = 0.04;    // spring stiffness (F = k_spring * (dist - rest))
const FS_REST_TRANSITION = 130; // rest length for transition edges
const FS_REST_RENDERS = 48;     // rest length for "renders" edges (status near page)
const FS_GRAVITY = 0.015;    // centering pull toward canvas center
const FS_DAMPING = 0.85;     // velocity damping per tick
const FS_ALPHA_DECAY = 0.99; // cooldown decay per tick
const FS_ALPHA_MIN = 0.01;   // stop threshold
const FS_ALPHA_START = 1.0;  // initial heat
const FS_MAX_TICKS = 400;    // hard cap on ticks
const FS_REHEAT = 0.3;       // alpha bump on drag

// User-tunable spacing multiplier (the "spread" slider). Scales both repulsion
// strength and edge rest lengths so a higher value pushes nodes further apart.
let fsSpacing = 1.0;

// Module-level map state shared by the reused force-sim neighborhood pane.
let mapView = { tx: 40, ty: 40, scale: 1 }; // pan/zoom transform
let mapG = null;                    // the <g> we transform
let mapSvg = null;                  // the <svg> element
let mapRebuildTimer = null;         // debounce timer for the live SSE refresh

// Force-sim runtime state (rebuilt on each neighborhood render).
let fsNodes = [];        // [{id, n, x, y, vx, vy, fx, fy, pinned, g, w, h}]
let fsEdges = [];        // [{from:idx, to:idx, rest, renders}]
let fsAlpha = 0;
let fsTicks = 0;
let fsRaf = null;        // requestAnimationFrame handle
let fsWidth = 800;       // canvas dims used for centering
let fsHeight = 600;

// ===================================================================
// MAP v2 module state.
// ===================================================================
const MV2_ROW_H = 22;          // icicle row height (CSS px)
const MV2_INDENT = 16;         // per-depth indent (CSS px)
const MV2_RENDER_CAP = 3000;   // hard cap on flattened visible rows
const MV2_OVERSCAN = 6;        // rows rendered above/below the viewport

// Contract status colors.
const MV2_C2 = '#1f7a3f', MV2_C3 = '#5b8db8', MV2_C4 = '#cc7a00', MV2_C5 = '#ff5555';

const mv2 = {
  mode: 'tree',          // 'tree' | 'overview'
  lens: 'none',          // 'none' | 'error-rate' | 'traffic'
  gen: 0,
  meta: null,            // last /map/meta payload
  // Tree (icicle) state.
  rootRows: [],          // top-level tree rows from /map/tree?path=
  children: new Map(),   // path -> [child rows] (lazily fetched)
  expanded: new Set(),   // expanded paths
  loadingPaths: new Set(),
  flat: [],              // current flattened visible rows [{row, depth, expandable}]
  scrollTop: 0,
  hoverIdx: -1,
  selectedPath: null,
  searchTerm: '',
  searchMatch: new Set(),// matched paths (highlight)
  searchTimer: null,
  // Canvas refs.
  canvas: null, ctx: null, host: null, dpr: 1, cw: 0, ch: 0,
  // WebGL state lives in mv2gl.
};

// WebGL Overview state.
const mv2gl = {
  inited: false, ok: false, gl: null, canvas: null, host: null, labelHost: null,
  progNode: null, progEdge: null,
  quadVBO: null, instVBO: null, nflagVBO: null, edgeVBO: null,
  nx: null, ny: null, nr: null,            // node world columns (Float32)
  esrc: null, edst: null, ecolorU8: null, eflag: null,
  nflagArr: null, ncolorF: null, edgeData: null, // CPU-side mirrors
  nodeN: 0, edgeN: 0, loadedGen: -1,       // loadedGen: snapshot gen of loaded geometry
  labels: null,            // [{label,path,...}] aligned to node idx (lazy)
  labelCandidates: [],     // node indices currently shown as HTML labels
  worldBBox: null,         // [x0,y0,x1,y1] of cluster bubbles
  cam: { x: 2048, y: 2048, scale: 0.15 }, // world center + scale (px-per-world derived)
  vw: 0, vh: 0,
  grid: null, gridCell: 0, gridW: 0, gridH: 0, gridMinX: 0, gridMinY: 0, // CPU picking grid
  raf: null, dragging: false, lastX: 0, lastY: 0,
  hoverNode: -1, labelDivs: [],
};

function svgEl(name, attrs) {
  const el = document.createElementNS(SVG_NS, name);
  if (attrs) for (const k in attrs) el.setAttribute(k, attrs[k]);
  return el;
}

function truncate(s, n) {
  s = s == null ? '' : String(s);
  return s.length > n ? s.slice(0, n - 1) + '…' : s;
}

// ===================================================================
// MAP v2 — icicle (Canvas2D) + WebGL2 cluster overview.
// loadMapTab/buildMapChrome keep their names (dispatched from showTab).
// The flat /map SVG force functions below (initForceSim/drawNode/drawEdge/
// collapseSingleStatus/attachMapInteractions/...) are kept INTACT and reused
// by the neighborhood pane only.
// ===================================================================

async function loadMapTab() {
  const view = document.getElementById('map-view');
  if (!view) return;
  if (!view.dataset.initialized) {
    view.dataset.initialized = '1';
    buildMapChrome(view);
  }
  // Entering the tab: refresh meta, (re)load the icicle roots if needed.
  await mv2Enter();
}

function buildMapChrome(view) {
  view.innerHTML = '';

  // --- Toolbar ---
  const toolbar = document.createElement('div');
  toolbar.className = 'mapv2-toolbar cols';

  // Command/search bar.
  const searchWrap = document.createElement('div');
  searchWrap.className = 'mapv2-searchbar';
  const sIcon = document.createElement('span');
  sIcon.className = 'mapv2-search-icon';
  sIcon.textContent = '⌕';
  searchWrap.appendChild(sIcon);
  const search = document.createElement('input');
  search.id = 'mapv2-search';
  search.type = 'text';
  search.placeholder = 'search  substring · status:404 · error:* · flow:123';
  search.spellcheck = false;
  search.oninput = () => {
    if (mv2.searchTimer) clearTimeout(mv2.searchTimer);
    mv2.searchTimer = setTimeout(() => { mv2.searchTimer = null; mv2RunSearch(search.value); }, 220);
  };
  searchWrap.appendChild(search);
  toolbar.appendChild(searchWrap);

  // Lens select.
  const lens = document.createElement('select');
  lens.id = 'mapv2-lens';
  lens.className = 'mapv2-select';
  [['none', 'lens: none'], ['error-rate', 'lens: error-rate'], ['traffic', 'lens: traffic']]
    .forEach(([v, t]) => { const o = document.createElement('option'); o.value = v; o.textContent = t; lens.appendChild(o); });
  lens.value = mv2.lens;
  lens.onchange = () => { mv2.lens = lens.value; mv2DrawIcicle(); };
  toolbar.appendChild(lens);

  // Mode select.
  const mode = document.createElement('select');
  mode.id = 'mapv2-mode';
  mode.className = 'mapv2-select';
  [['tree', 'Tree'], ['overview', 'Overview']]
    .forEach(([v, t]) => { const o = document.createElement('option'); o.value = v; o.textContent = t; mode.appendChild(o); });
  mode.value = mv2.mode;
  mode.onchange = () => mv2SetMode(mode.value);
  toolbar.appendChild(mode);

  const status = document.createElement('div');
  status.id = 'mapv2-status';
  status.className = 'mapv2-status';
  toolbar.appendChild(status);

  view.appendChild(toolbar);

  // --- Main flex row ---
  const main = document.createElement('div');
  main.id = 'mapv2-main';
  main.className = 'mapv2-main';

  // Left: icicle canvas host (Tree mode). The canvas is a fixed overlay pinned
  // to the host box; a separate absolutely-positioned scroll layer owns the
  // scrollbar + scroll offset (its lone child .mapv2-spacer sets the height).
  // The canvas is redrawn on the scroll layer's scroll event (virtualized).
  const left = document.createElement('div');
  left.id = 'mapv2-left';
  left.className = 'mapv2-left';
  const treeCanvas = document.createElement('canvas');
  treeCanvas.id = 'mapv2-tree-canvas';
  treeCanvas.className = 'mapv2-tree-canvas';
  left.appendChild(treeCanvas);
  const scroll = document.createElement('div');
  scroll.id = 'mapv2-scroll';
  scroll.className = 'mapv2-scroll';
  const spacer = document.createElement('div');
  spacer.className = 'mapv2-spacer';
  scroll.appendChild(spacer);
  left.appendChild(scroll);
  main.appendChild(left);

  // WebGL host (Overview mode); sits inside main, hidden in Tree.
  const glHost = document.createElement('div');
  glHost.id = 'mapv2-gl-host';
  glHost.className = 'mapv2-gl-host';
  glHost.style.display = 'none';
  const glCanvas = document.createElement('canvas');
  glCanvas.id = 'mapv2-gl-canvas';
  glCanvas.className = 'mapv2-gl-canvas';
  glHost.appendChild(glCanvas);
  const glLabels = document.createElement('div');
  glLabels.id = 'mapv2-gl-labels';
  glLabels.className = 'mapv2-gl-labels';
  glHost.appendChild(glLabels);
  main.appendChild(glHost);

  // Right: detail + neighborhood pane.
  const right = document.createElement('div');
  right.id = 'mapv2-right';
  right.className = 'mapv2-right';
  const detail = document.createElement('div');
  detail.id = 'mapv2-detail';
  detail.className = 'mapv2-detail';
  detail.textContent = 'Select a node to inspect its neighborhood.';
  right.appendChild(detail);
  const neigh = document.createElement('div');
  neigh.id = 'mapv2-neighborhood';
  neigh.className = 'mapv2-neighborhood';
  right.appendChild(neigh);
  main.appendChild(right);

  view.appendChild(main);

  // Wire icicle interactions once.
  mv2WireIcicle(treeCanvas, left);
  // Wire WebGL interactions once (lazily inits GL on first Overview entry).
  mv2WireGL(glCanvas, glHost, glLabels);
}

// ===================================================================
// MAP v2 — controller
// ===================================================================

function mv2SetStatus(text) {
  const el = document.getElementById('mapv2-status');
  if (el) el.textContent = text || '';
}

// Called whenever the Map tab is shown: refresh meta and (re)load the icicle
// roots if the snapshot generation changed or nothing is loaded yet.
async function mv2Enter() {
  let meta;
  try {
    meta = await mv2FetchJSON('/map/meta');
  } catch (err) {
    mv2SetStatus('error: ' + err.message);
    return;
  }
  const genChanged = !mv2.meta || meta.gen !== mv2.gen || !mv2.rootRows.length;
  mv2.meta = meta;
  mv2.gen = meta.gen;
  mv2SetStatus(meta.building ? 'building…' : (meta.nodeN + ' nodes · ' + meta.edgeN + ' edges · ' + (meta.clusterN || 0) + ' clusters'));

  if (genChanged) {
    // New snapshot: clear the lazy caches and reload roots.
    mv2.children.clear();
    mv2.expanded.clear();
    mv2.searchMatch.clear();
    mv2gl.labels = null;     // force label re-resolve on next overview
    mv2gl.loadedGen = -1;    // force cluster geometry reload on next overview
    try {
      const t = await mv2FetchJSON('/map/tree?path=&depth=1');
      mv2.rootRows = (t && t.rows) || [];
    } catch (err) {
      mv2SetStatus('error: ' + err.message);
      return;
    }
  }

  if (mv2.mode === 'overview') {
    mv2EnterOverview();
  } else {
    mv2ResizeIcicle();
    mv2DrawIcicle();
  }
}

// fetch JSON honoring the snapshot gen and surfacing HTTP errors.
async function mv2FetchJSON(url) {
  const res = await fetch(url, { headers: { 'Accept': 'application/json' } });
  if (res.status === 202) throw new Error('snapshot building');
  if (!res.ok) throw new Error('HTTP ' + res.status);
  return res.json();
}

// --- Tree / icicle ------------------------------------------------

// Lazily fetch the direct children of a path (depth=1). Cached in mv2.children.
async function mv2FetchChildren(path) {
  if (mv2.children.has(path)) return mv2.children.get(path);
  if (mv2.loadingPaths.has(path)) return null;
  mv2.loadingPaths.add(path);
  try {
    const t = await mv2FetchJSON('/map/tree?path=' + encodeURIComponent(path) + '&depth=1');
    const rows = (t && t.rows) || [];
    mv2.children.set(path, rows);
    return rows;
  } finally {
    mv2.loadingPaths.delete(path);
  }
}

// Flatten the currently-expanded tree into mv2.flat (visible rows only).
// Each entry: {row, depth, kind:'row'|'more', moreCount, parentPath}. Capped at
// MV2_RENDER_CAP with a synthetic "+N more" summary row that expands the cap.
function mv2Flatten() {
  const out = [];
  let capped = false;
  const walk = (rows, depth) => {
    for (const row of rows) {
      if (out.length >= MV2_RENDER_CAP) { capped = true; return; }
      out.push({ kind: 'row', row, depth });
      if (row.hasChildren && mv2.expanded.has(row.path)) {
        const kids = mv2.children.get(row.path);
        if (kids) walk(kids, depth + 1);
        else out.push({ kind: 'loading', depth: depth + 1 });
      }
    }
  };
  walk(mv2.rootRows, 0);
  if (capped) out.push({ kind: 'more', depth: 0 });
  mv2.flat = out;
}

function mv2WireIcicle(canvas, host) {
  mv2.canvas = canvas;
  mv2.ctx = canvas.getContext('2d');
  mv2.host = host;                                 // the .mapv2-left box
  const scroll = host.querySelector('.mapv2-scroll');
  mv2.scroll = scroll;                             // the scrolling overlay

  // Virtualized scroll: the scroll layer owns the offset; redraw the (fixed)
  // canvas to show the window at the new scrollTop.
  scroll.addEventListener('scroll', () => {
    mv2.scrollTop = scroll.scrollTop;
    mv2DrawIcicle();
  }, { passive: true });

  scroll.addEventListener('mousemove', (e) => {
    const rect = scroll.getBoundingClientRect();
    const y = e.clientY - rect.top + scroll.scrollTop;
    const idx = mv2RowAt(y);
    if (idx !== mv2.hoverIdx) { mv2.hoverIdx = idx; mv2DrawIcicle(); }
  });
  scroll.addEventListener('mouseleave', () => {
    if (mv2.hoverIdx !== -1) { mv2.hoverIdx = -1; mv2DrawIcicle(); }
  });
  scroll.addEventListener('click', (e) => {
    const rect = scroll.getBoundingClientRect();
    const y = e.clientY - rect.top + scroll.scrollTop;
    const x = e.clientX - rect.left;
    mv2OnRowClick(mv2RowAt(y), x);
  });

  // Resize observer to keep the canvas DPR-correct.
  if (typeof ResizeObserver !== 'undefined') {
    const ro = new ResizeObserver(() => { mv2ResizeIcicle(); mv2DrawIcicle(); });
    ro.observe(host);
  } else {
    window.addEventListener('resize', () => { mv2ResizeIcicle(); mv2DrawIcicle(); });
  }
}

// O(1) row index from a content-space y (prefix-sum is trivial: uniform height).
function mv2RowAt(y) {
  const idx = Math.floor(y / MV2_ROW_H);
  return (idx >= 0 && idx < mv2.flat.length) ? idx : -1;
}

function mv2ResizeIcicle() {
  const host = mv2.host, canvas = mv2.canvas;
  if (!host || !canvas) return;
  const dpr = window.devicePixelRatio || 1;
  const w = host.clientWidth, h = host.clientHeight;
  mv2.dpr = dpr; mv2.cw = w; mv2.ch = h;
  canvas.width = Math.max(1, Math.round(w * dpr));
  canvas.height = Math.max(1, Math.round(h * dpr));
  canvas.style.width = w + 'px';
  canvas.style.height = h + 'px';
}

async function mv2OnRowClick(idx, x) {
  if (idx < 0 || idx >= mv2.flat.length) return;
  const item = mv2.flat[idx];
  if (item.kind === 'more') {
    // Expand the render cap by another budget.
    mv2.extraCap = (mv2.extraCap || 0) + MV2_RENDER_CAP;
    mv2DrawIcicle();
    return;
  }
  if (item.kind !== 'row') return;
  const row = item.row;

  // Click on the caret region toggles expand; else treat leaf click as select.
  const caretX = item.depth * MV2_INDENT;
  const onCaret = row.hasChildren && x >= caretX && x <= caretX + MV2_INDENT;

  if (row.hasChildren && onCaret) {
    await mv2ToggleExpand(row.path);
    return;
  }
  if (row.hasChildren && !onCaret) {
    // Clicking the label of an internal row also toggles (common UX).
    await mv2ToggleExpand(row.path);
    return;
  }
  // Leaf: load its neighborhood + detail.
  mv2.selectedPath = row.path;
  mv2DrawIcicle();
  mv2ShowDetail(row);
  mv2LoadNeighborhood(row);
}

async function mv2ToggleExpand(path) {
  if (mv2.expanded.has(path)) {
    mv2.expanded.delete(path);
    mv2DrawIcicle();
    return;
  }
  mv2.expanded.add(path);
  mv2DrawIcicle(); // show "loading…" immediately
  await mv2FetchChildren(path);
  mv2DrawIcicle();
}

// Draw only the visible window of rows into the canvas.
function mv2DrawIcicle() {
  const ctx = mv2.ctx, host = mv2.host;
  if (!ctx || !host) return;
  if (mv2.cw === 0) mv2ResizeIcicle();

  mv2Flatten();
  const total = mv2.flat.length;

  // Drive the scrollbar: spacer height = total rows * row height.
  const spacer = mv2.scroll && mv2.scroll.querySelector('.mapv2-spacer');
  if (spacer) spacer.style.height = (total * MV2_ROW_H) + 'px';

  const dpr = mv2.dpr;
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, mv2.cw, mv2.ch);
  ctx.textBaseline = 'middle';
  ctx.font = '12px monospace';

  if (!total) {
    ctx.fillStyle = '#888';
    ctx.fillText('No data — browse through the proxy to build the map', 12, 20);
    return;
  }

  const scrollTop = mv2.scrollTop;
  const first = Math.max(0, Math.floor(scrollTop / MV2_ROW_H) - MV2_OVERSCAN);
  const last = Math.min(total - 1, Math.ceil((scrollTop + mv2.ch) / MV2_ROW_H) + MV2_OVERSCAN);

  for (let i = first; i <= last; i++) {
    mv2DrawRow(ctx, mv2.flat[i], i, (i * MV2_ROW_H) - scrollTop);
  }
}

function mv2DrawRow(ctx, item, idx, y) {
  const w = mv2.cw;
  const h = MV2_ROW_H;

  if (item.kind === 'loading') {
    ctx.fillStyle = '#666';
    ctx.fillText('loading…', item.depth * MV2_INDENT + 18, y + h / 2);
    return;
  }
  if (item.kind === 'more') {
    ctx.fillStyle = '#0d0d0d';
    ctx.fillRect(0, y, w, h);
    ctx.fillStyle = '#d0a85e';
    ctx.fillText('+ more rows (cap reached, click to load more)', 18, y + h / 2);
    return;
  }

  const row = item.row;
  const isSel = mv2.selectedPath === row.path;
  const isHover = mv2.hoverIdx === idx;
  const isMatch = mv2.searchMatch.has(row.path);

  // Row background.
  if (isSel) { ctx.fillStyle = '#1f3a2a'; ctx.fillRect(0, y, w, h); }
  else if (isHover) { ctx.fillStyle = '#232323'; ctx.fillRect(0, y, w, h); }
  if (isMatch) { ctx.fillStyle = 'rgba(208,168,94,0.18)'; ctx.fillRect(0, y, w, h); }

  const indent = item.depth * MV2_INDENT;

  // Caret.
  ctx.fillStyle = '#888';
  if (row.hasChildren) {
    ctx.fillText(mv2.expanded.has(row.path) ? '▾' : '▸', indent + 2, y + h / 2);
  }

  // Lens-based color accent bar at the left.
  const accent = mv2LensColor(row);
  if (accent) { ctx.fillStyle = accent; ctx.fillRect(0, y, 3, h); }

  // Label.
  const labelX = indent + 16;
  ctx.fillStyle = row.errorRate >= 0.5 ? '#ff8888' : (isMatch ? '#ffd479' : '#c8e6c9');
  const labelMax = Math.max(60, w * 0.45);
  ctx.fillText(mv2ClipText(ctx, row.label || row.path || '(root)', labelMax), labelX, y + h / 2);

  // Right side: status bar + counts + error glyph.
  const rightPad = 8;
  let rx = w - rightPad;

  // Error glyph.
  if (row.statusMix && row.statusMix.err > 0) {
    ctx.fillStyle = MV2_C5;
    ctx.textAlign = 'right';
    ctx.fillText('⚠', rx, y + h / 2);
    ctx.textAlign = 'left';
    rx -= 16;
  }

  // Counts (flowCount, childCount).
  ctx.fillStyle = '#777';
  ctx.textAlign = 'right';
  const counts = (row.flowCount || 0) + (row.childCount ? '·' + row.childCount : '');
  ctx.fillText(counts, rx, y + h / 2);
  const cm = ctx.measureText(counts).width;
  ctx.textAlign = 'left';
  rx -= cm + 10;

  // Inline stacked status bar from statusMix.
  mv2DrawStatusBar(ctx, row.statusMix, rx - 90, y + 5, 90, h - 10);
}

function mv2DrawStatusBar(ctx, mix, x, y, w, h) {
  if (!mix) return;
  const total = (mix.c2 || 0) + (mix.c3 || 0) + (mix.c4 || 0) + (mix.c5 || 0) + (mix.err || 0);
  if (total <= 0) return;
  const segs = [[mix.c2, MV2_C2], [mix.c3, MV2_C3], [mix.c4, MV2_C4], [mix.c5, MV2_C5], [mix.err, MV2_C5]];
  let cx = x;
  for (const [n, col] of segs) {
    if (!n) continue;
    const sw = (n / total) * w;
    ctx.fillStyle = col;
    ctx.fillRect(cx, y, Math.max(1, sw), h);
    cx += sw;
  }
  // Thin border.
  ctx.strokeStyle = '#333';
  ctx.lineWidth = 1;
  ctx.strokeRect(x + 0.5, y + 0.5, w, h);
}

// Lens recolor: returns a left-accent color or null.
function mv2LensColor(row) {
  if (mv2.lens === 'error-rate') {
    const r = row.errorRate || 0;
    if (r <= 0) return null;
    // amber->red ramp.
    return r >= 0.5 ? MV2_C5 : MV2_C4;
  }
  if (mv2.lens === 'traffic') {
    const f = row.flowCount || 0;
    if (f <= 0) return null;
    // brighten with traffic (log scale).
    const t = Math.min(1, Math.log10(f + 1) / 4);
    const v = Math.round(60 + t * 180);
    return 'rgb(' + Math.round(v * 0.5) + ',' + v + ',' + Math.round(v * 0.6) + ')';
  }
  return null;
}

function mv2ClipText(ctx, s, maxW) {
  s = s == null ? '' : String(s);
  if (ctx.measureText(s).width <= maxW) return s;
  while (s.length > 1 && ctx.measureText(s + '…').width > maxW) s = s.slice(0, -1);
  return s + '…';
}

// --- Search -------------------------------------------------------

async function mv2RunSearch(q) {
  mv2.searchTerm = q;
  if (!q || !q.trim()) {
    mv2.searchMatch.clear();
    mv2DrawIcicle();
    return;
  }
  let resp;
  try {
    resp = await mv2FetchJSON('/map/search?gen=' + mv2.gen + '&q=' + encodeURIComponent(q));
  } catch (err) {
    mv2SetStatus('search error: ' + err.message);
    return;
  }
  const matches = (resp && resp.matches) || [];
  const expandPaths = (resp && resp.expandPaths) || [];
  mv2.searchMatch = new Set(matches.map(m => m.path));

  // Auto-expand all ancestor prefixes, fetching children as needed.
  for (const p of expandPaths) {
    if (!mv2.expanded.has(p)) {
      mv2.expanded.add(p);
      await mv2FetchChildren(p);
    }
  }
  mv2DrawIcicle();

  // Scroll the first match into view.
  if (matches.length) {
    const firstPath = matches[0].path;
    mv2Flatten();
    const idx = mv2.flat.findIndex(it => it.kind === 'row' && it.row.path === firstPath);
    if (idx >= 0 && mv2.scroll) {
      const target = idx * MV2_ROW_H - mv2.ch / 2;
      mv2.scroll.scrollTop = Math.max(0, target);
      mv2.scrollTop = mv2.scroll.scrollTop;
      mv2DrawIcicle();
    }
  }
  mv2SetStatus(matches.length + ' match' + (matches.length === 1 ? '' : 'es'));
}

// --- Detail + neighborhood pane (reuses the SVG force functions) --

function mv2ShowDetail(row) {
  const d = document.getElementById('mapv2-detail');
  if (!d) return;
  d.replaceChildren();
  const title = document.createElement('div');
  title.className = 'mapv2-detail-title';
  title.textContent = row.label || row.path;
  d.appendChild(title);

  const meta = document.createElement('div');
  meta.className = 'mapv2-detail-meta';
  const mix = row.statusMix || {};
  meta.textContent = 'path=' + row.path + '  flows=' + (row.flowCount || 0) +
    '  kind=' + (row.kind || '?') +
    '  [2xx ' + (mix.c2 || 0) + ' · 3xx ' + (mix.c3 || 0) + ' · 4xx ' + (mix.c4 || 0) +
    ' · 5xx ' + (mix.c5 || 0) + ' · err ' + (mix.err || 0) + ']';
  d.appendChild(meta);

  if (row.sampleFlowId != null && row.sampleFlowId !== 0) {
    const btn = document.createElement('button');
    btn.className = 'mapv2-detail-btn';
    btn.textContent = 'Inspect sample flow #' + row.sampleFlowId;
    btn.onclick = () => { inspectFlow(row.sampleFlowId); showTab('inspect'); };
    d.appendChild(btn);
  }
}

// Load /map/neighborhood for the row and render it in #mapv2-neighborhood,
// reusing the existing SVG force-graph machinery (initForceSim/drawNode/...).
async function mv2LoadNeighborhood(row) {
  const host = document.getElementById('mapv2-neighborhood');
  if (!host) return;
  host.replaceChildren();
  const loading = document.createElement('div');
  loading.className = 'map-empty';
  loading.textContent = 'loading neighborhood…';
  host.appendChild(loading);

  let data;
  try {
    data = await mv2FetchJSON('/map/neighborhood?gen=' + mv2.gen +
      '&node=' + encodeURIComponent(row.path) + '&hops=1');
  } catch (err) {
    host.replaceChildren();
    const e = document.createElement('div');
    e.className = 'map-empty';
    e.textContent = 'neighborhood error: ' + err.message;
    host.appendChild(e);
    return;
  }
  mv2RenderNeighborhood(host, data);
}

// Render a {nodes,edges} graph into `host` using the kept force-sim funcs.
function mv2RenderNeighborhood(host, data) {
  stopForceSim();
  host.replaceChildren();
  const allNodes = (data && data.nodes) || [];
  const allEdges = (data && data.edges) || [];
  if (!allNodes.length) {
    const empty = document.createElement('div');
    empty.className = 'map-empty';
    empty.textContent = 'no neighborhood';
    host.appendChild(empty);
    return;
  }

  // Reuse the single-status collapse to keep the neighborhood readable.
  const collapsed = collapseSingleStatus(allNodes, allEdges);
  const renderNodes = collapsed.nodes;
  const keep = new Set(renderNodes.map(n => n.id));
  const renderEdges = collapsed.edges.filter(e => keep.has(e.from) && keep.has(e.to));

  const svg = svgEl('svg', { class: 'map-svg' });
  mapSvg = svg;
  const defs = svgEl('defs');
  defs.appendChild(makeArrowMarker('map-arrow', '#888'));
  defs.appendChild(makeArrowMarker('map-arrow-err', '#ff5555'));
  svg.appendChild(defs);
  const g = svgEl('g');
  mapG = g;
  svg.appendChild(g);
  const edgeLayer = svgEl('g', { class: 'map-edge-layer' });
  const nodeLayer = svgEl('g', { class: 'map-node-layer' });
  g.appendChild(edgeLayer);
  g.appendChild(nodeLayer);
  host.appendChild(svg);

  const rect = host.getBoundingClientRect();
  fsWidth = rect.width || 400;
  fsHeight = rect.height || 400;

  initForceSim(renderNodes, renderEdges, nodeLayer, edgeLayer);
  attachMapInteractions(svg);
  fitMapView();
  applyMapTransform();
  startForceSim();
}

// --- Mode switching -----------------------------------------------

function mv2SetMode(mode) {
  mv2.mode = mode;
  const left = document.getElementById('mapv2-left');
  const right = document.getElementById('mapv2-right');
  const glHost = document.getElementById('mapv2-gl-host');
  if (mode === 'overview') {
    if (left) left.style.display = 'none';
    if (right) right.style.display = 'none';
    if (glHost) glHost.style.display = 'block';
    mv2EnterOverview();
  } else {
    if (glHost) glHost.style.display = 'none';
    if (left) left.style.display = 'block';
    if (right) right.style.display = 'flex';
    mv2StopGLLoop();
    mv2ResizeIcicle();
    mv2DrawIcicle();
  }
}

// ===================================================================
// MAP v2 — WebGL2 cluster Overview (hand-written, no libraries).
//
// Architecture:
//  * Geometry source: GET /map/clusters?depth=2 -> HMAP binary (section A).
//    Decoded into per-instance node columns (x,y,r,color) and edge index
//    columns (esrc,edst,ecolor,eflag). Strings are NOT in the binary; node
//    labels are fetched lazily via /map/labels for the hovered/top-N nodes.
//  * Two instanced draw programs sharing one unit-quad VBO:
//      - progNode: per-instance (x,y,r,color); fragment shader draws an
//        antialiased disc (smoothstep on distance from quad center). Vertex
//        shader applies the camera mat3 and discards (degenerate) instances
//        when on-screen pixel radius < 0.75 (LOD cull) or the filter flag
//        clears bit0.
//      - progEdge: a per-instance THICK LINE built as a unit quad stretched
//        between node[esrc] and node[edst]. The node positions are passed as
//        two extra instanced attributes (asrc, adst) so panning/zooming never
//        re-uploads geometry — only the camera uniform changes.
//  * Camera: a 3x3 column-major mat3 uniform mapping world -> clip. Pan = drag
//    (translate world center), zoom = wheel toward cursor (adjust scale +
//    recenter so the cursor world point is fixed). Buffers are uploaded ONCE.
//  * Picking: a CPU uniform grid over world space (built once per dataset);
//    hover maps the cursor to a world point, scans the cursor's grid cell +
//    neighbors for the nearest disc within its radius. O(1) amortized.
//  * Labels: pooled .mapv2-label-div elements positioned via CSS transform for
//    the hovered node + a handful of the largest on-screen nodes (top-N).
//  * Filtering: a Uint8Array flag column; toggling re-uploads ONLY that column
//    via bufferSubData and the VS discards cleared instances.
// ===================================================================

const MV2_NODE_VS = `#version 300 es
layout(location=0) in vec2 a_quad;     // unit quad [-1,1]
layout(location=1) in vec2 a_pos;      // world center
layout(location=2) in float a_r;       // world radius
layout(location=3) in vec4 a_color;    // rgba 0..1
layout(location=4) in float a_flag;    // bit0 = visible
uniform mat3 u_cam;                    // world -> clip
uniform float u_minPx;                 // LOD: min on-screen px radius
uniform vec2 u_viewport;               // px
out vec2 v_uv;
out vec4 v_color;
void main() {
  // On-screen pixel radius = world r * scale (cam scale lives in u_cam[0][0] & [1][1]).
  float pxR = a_r * length(vec2(u_cam[0][0], u_cam[1][0])) * 0.5 * u_viewport.x;
  if (a_flag < 0.5 || pxR < u_minPx) {
    gl_Position = vec4(2.0, 2.0, 2.0, 1.0); // off-screen, culled
    return;
  }
  vec3 world = vec3(a_pos + a_quad * a_r, 1.0);
  vec3 clip = u_cam * world;
  gl_Position = vec4(clip.xy, 0.0, 1.0);
  v_uv = a_quad;
  v_color = a_color;
}`;

const MV2_NODE_FS = `#version 300 es
precision mediump float;
in vec2 v_uv;
in vec4 v_color;
out vec4 o;
void main() {
  float d = length(v_uv);
  float aa = fwidth(d) + 0.001;
  float alpha = 1.0 - smoothstep(1.0 - aa, 1.0, d);
  if (alpha <= 0.0) discard;
  o = vec4(v_color.rgb, v_color.a * alpha);
}`;

const MV2_EDGE_VS = `#version 300 es
layout(location=0) in vec2 a_quad;     // unit quad [-1,1] -> long side along x
layout(location=1) in vec2 a_src;      // world endpoint
layout(location=2) in vec2 a_dst;      // world endpoint
layout(location=3) in vec4 a_color;
layout(location=4) in float a_flag;
uniform mat3 u_cam;
uniform float u_halfW;                 // half line width in world units
out vec4 v_color;
void main() {
  if (a_flag < 0.5) { gl_Position = vec4(2.0,2.0,2.0,1.0); return; }
  vec2 dir = a_dst - a_src;
  float len = length(dir);
  if (len < 1e-4) { gl_Position = vec4(2.0,2.0,2.0,1.0); return; }
  vec2 t = dir / len;
  vec2 n = vec2(-t.y, t.x);
  // a_quad.x in [-1,1] -> along the segment; a_quad.y in [-1,1] -> across.
  float s = (a_quad.x * 0.5 + 0.5);          // 0..1 param along segment
  vec2 world = a_src + dir * s + n * (a_quad.y * u_halfW);
  vec3 clip = u_cam * vec3(world, 1.0);
  gl_Position = vec4(clip.xy, 0.0, 1.0);
  v_color = a_color;
}`;

const MV2_EDGE_FS = `#version 300 es
precision mediump float;
in vec4 v_color;
out vec4 o;
void main() { o = v_color; }`;

function mv2WireGL(canvas, host, labelHost) {
  mv2gl.canvas = canvas;
  mv2gl.host = host;
  mv2gl.labelHost = labelHost;

  // Pan (drag).
  canvas.addEventListener('mousedown', (e) => {
    mv2gl.dragging = true; mv2gl.lastX = e.clientX; mv2gl.lastY = e.clientY;
    canvas.style.cursor = 'grabbing';
  });
  window.addEventListener('mouseup', () => {
    if (mv2gl.dragging) { mv2gl.dragging = false; if (mv2gl.canvas) mv2gl.canvas.style.cursor = 'grab'; }
  });
  canvas.addEventListener('mousemove', (e) => {
    if (mv2gl.dragging) {
      const dx = e.clientX - mv2gl.lastX, dy = e.clientY - mv2gl.lastY;
      mv2gl.lastX = e.clientX; mv2gl.lastY = e.clientY;
      // Convert px delta to world delta (scale = px-per-world derived below).
      const ppw = mv2GLPixelsPerWorld();
      mv2gl.cam.x -= dx / ppw;
      mv2gl.cam.y += dy / ppw; // screen y is down; world y is up in our mapping
      mv2GLRequestDraw();
    } else {
      mv2GLHover(e);
    }
  });
  canvas.addEventListener('mouseleave', () => { mv2gl.hoverNode = -1; mv2GLRequestDraw(); });

  // Zoom toward cursor.
  canvas.addEventListener('wheel', (e) => {
    e.preventDefault();
    const rect = canvas.getBoundingClientRect();
    const mx = e.clientX - rect.left, my = e.clientY - rect.top;
    const before = mv2GLScreenToWorld(mx, my);
    const factor = e.deltaY < 0 ? 1.1 : 1 / 1.1;
    mv2gl.cam.scale = Math.max(0.01, Math.min(20, mv2gl.cam.scale * factor));
    const after = mv2GLScreenToWorld(mx, my);
    // Keep the world point under the cursor fixed.
    mv2gl.cam.x += before.x - after.x;
    mv2gl.cam.y += before.y - after.y;
    mv2GLRequestDraw();
  }, { passive: false });

  // Click a cluster -> jump to Tree mode focused at that path.
  canvas.addEventListener('click', () => {
    if (mv2gl.hoverNode < 0) return;
    const lbl = mv2gl.labels && mv2gl.labels[mv2gl.hoverNode];
    if (lbl && lbl.path != null) mv2GLJumpToTree(lbl.path);
  });

  if (typeof ResizeObserver !== 'undefined') {
    const ro = new ResizeObserver(() => { mv2GLResize(); mv2GLRequestDraw(); });
    ro.observe(host);
  }
}

// px-per-world: our camera maps a world span of (2/scale) across the smaller
// viewport dimension... actually we derive ppw from the mat3 we build.
function mv2GLPixelsPerWorld() {
  return mv2gl.cam.scale * Math.min(mv2gl.vw, mv2gl.vh) / 2;
}

function mv2GLScreenToWorld(px, py) {
  const ppw = mv2GLPixelsPerWorld();
  // screen center = world center.
  const wx = mv2gl.cam.x + (px - mv2gl.vw / 2) / ppw;
  const wy = mv2gl.cam.y - (py - mv2gl.vh / 2) / ppw;
  return { x: wx, y: wy };
}

// Build the column-major mat3 (world -> clip) for the current camera + viewport.
// world span across min(vw,vh) is (2 / scale) ... we want ppw px per world unit
// where ppw = scale * min(vw,vh)/2. clipX = (wx - camx) * ppw / (vw/2).
function mv2GLCamMatrix() {
  const ppw = mv2GLPixelsPerWorld();
  const sx = ppw / (mv2gl.vw / 2);
  const sy = ppw / (mv2gl.vh / 2);
  const cx = mv2gl.cam.x, cy = mv2gl.cam.y;
  // clip = S * (world - cam); y up.
  // column-major 3x3:
  // [ sx   0   0 ]
  // [ 0    sy  0 ]
  // [ -sx*cx -sy*cy 1 ]
  return new Float32Array([
    sx, 0, 0,
    0, sy, 0,
    -sx * cx, -sy * cy, 1,
  ]);
}

function mv2GLResize() {
  const host = mv2gl.host, canvas = mv2gl.canvas;
  if (!host || !canvas) return;
  const dpr = window.devicePixelRatio || 1;
  const w = host.clientWidth, h = host.clientHeight;
  mv2gl.vw = w; mv2gl.vh = h;
  canvas.width = Math.max(1, Math.round(w * dpr));
  canvas.height = Math.max(1, Math.round(h * dpr));
  canvas.style.width = w + 'px';
  canvas.style.height = h + 'px';
  if (mv2gl.gl) mv2gl.gl.viewport(0, 0, canvas.width, canvas.height);
}

function mv2GLCompile(gl, type, src) {
  const sh = gl.createShader(type);
  gl.shaderSource(sh, src);
  gl.compileShader(sh);
  if (!gl.getShaderParameter(sh, gl.COMPILE_STATUS)) {
    const log = gl.getShaderInfoLog(sh);
    gl.deleteShader(sh);
    throw new Error('shader compile: ' + log);
  }
  return sh;
}

function mv2GLProgram(gl, vsSrc, fsSrc) {
  const vs = mv2GLCompile(gl, gl.VERTEX_SHADER, vsSrc);
  const fs = mv2GLCompile(gl, gl.FRAGMENT_SHADER, fsSrc);
  const p = gl.createProgram();
  gl.attachShader(p, vs); gl.attachShader(p, fs);
  gl.linkProgram(p);
  if (!gl.getProgramParameter(p, gl.LINK_STATUS)) {
    throw new Error('program link: ' + gl.getProgramInfoLog(p));
  }
  return {
    prog: p,
    u_cam: gl.getUniformLocation(p, 'u_cam'),
    u_minPx: gl.getUniformLocation(p, 'u_minPx'),
    u_viewport: gl.getUniformLocation(p, 'u_viewport'),
    u_halfW: gl.getUniformLocation(p, 'u_halfW'),
  };
}

function mv2GLInit() {
  if (mv2gl.inited) return mv2gl.ok;
  mv2gl.inited = true;
  const canvas = mv2gl.canvas;
  const gl = canvas.getContext('webgl2', { antialias: true, premultipliedAlpha: false });
  if (!gl) { mv2gl.ok = false; return false; }
  mv2gl.gl = gl;
  try {
    mv2gl.progNode = mv2GLProgram(gl, MV2_NODE_VS, MV2_NODE_FS);
    mv2gl.progEdge = mv2GLProgram(gl, MV2_EDGE_VS, MV2_EDGE_FS);
  } catch (err) {
    console.error('WebGL init failed', err);
    mv2gl.ok = false; return false;
  }
  // Unit quad shared by node + edge instancing.
  const quad = new Float32Array([-1, -1, 1, -1, -1, 1, 1, 1]); // triangle strip
  mv2gl.quadVBO = gl.createBuffer();
  gl.bindBuffer(gl.ARRAY_BUFFER, mv2gl.quadVBO);
  gl.bufferData(gl.ARRAY_BUFFER, quad, gl.STATIC_DRAW);
  gl.clearColor(0.102, 0.102, 0.102, 1.0); // #1a1a1a
  gl.enable(gl.BLEND);
  gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
  mv2gl.ok = true;
  return true;
}

// HMAP decoder (section A). Returns columns as fresh typed arrays (slice-copied
// for alignment) plus header counts/flags.
function mv2DecodeHMAP(buf) {
  const dv = new DataView(buf);
  const magic = dv.getUint32(0, true);
  if (magic !== 0x484D4150) throw new Error('bad HMAP magic 0x' + magic.toString(16));
  const version = dv.getUint32(4, true);
  const gen = dv.getUint32(8, true);
  const nodeN = dv.getUint32(12, true);
  const edgeN = dv.getUint32(16, true);
  const flags = dv.getUint32(20, true);
  let off = 32;
  const f32 = (n) => { const a = new Float32Array(buf.slice(off, off + n * 4)); off += n * 4; return a; };
  const u32 = (n) => { const a = new Uint32Array(buf.slice(off, off + n * 4)); off += n * 4; return a; };
  const u8 = (n) => { const a = new Uint8Array(buf.slice(off, off + n)); off += n; return a; };
  const i16 = (n) => { const a = new Int16Array(buf.slice(off, off + n * 2)); off += n * 2; return a; };
  // Node columns.
  const x = f32(nodeN), y = f32(nodeN), r = f32(nodeN);
  const color = u32(nodeN);
  const ntype = u8(nodeN);
  const status = i16(nodeN);
  // Edge columns.
  const esrc = u32(edgeN), edst = u32(edgeN), ecolor = u32(edgeN);
  const eflags = u8(edgeN);
  return { version, gen, nodeN, edgeN, flags, x, y, r, color, ntype, status, esrc, edst, ecolor, eflags };
}

// Unpack a 0xRRGGBBAA uint32 (r in byte0) into a [r,g,b,a] float array 0..1.
function mv2UnpackColor(u, out, o) {
  out[o] = ((u >>> 24) & 0xff) / 255;
  out[o + 1] = ((u >>> 16) & 0xff) / 255;
  out[o + 2] = ((u >>> 8) & 0xff) / 255;
  out[o + 3] = (u & 0xff) / 255;
}

async function mv2EnterOverview() {
  const glHost = document.getElementById('mapv2-gl-host');
  if (!glHost) return;
  mv2GLResize();

  if (!mv2GLInit()) {
    mv2GLShowFallback();
    return;
  }
  // Load cluster geometry if the snapshot generation changed.
  if (mv2gl.loadedGen !== mv2.gen || mv2gl.nodeN === 0) {
    try {
      await mv2GLLoadClusters();
    } catch (err) {
      mv2SetStatus('overview error: ' + err.message);
      return;
    }
  }
  mv2GLFitView();
  mv2GLSelectLabels();
  mv2GLRequestDraw();
}

function mv2GLShowFallback() {
  const lh = mv2gl.labelHost;
  if (!lh) return;
  lh.replaceChildren();
  const msg = document.createElement('div');
  msg.className = 'mapv2-gl-fallback';
  msg.textContent = 'WebGL2 not available — Overview disabled. Tree mode still works.';
  lh.appendChild(msg);
}

async function mv2GLLoadClusters() {
  const res = await fetch('/map/clusters?gen=' + mv2.gen + '&depth=2');
  if (res.status === 202) throw new Error('snapshot building');
  if (!res.ok) throw new Error('HTTP ' + res.status);
  const buf = await res.arrayBuffer();
  const d = mv2DecodeHMAP(buf);
  const gl = mv2gl.gl;

  mv2gl.nodeN = d.nodeN;
  mv2gl.edgeN = d.edgeN;
  mv2gl.nx = d.x; mv2gl.ny = d.y; mv2gl.nr = d.r;
  mv2gl.esrc = d.esrc; mv2gl.edst = d.edst; mv2gl.ecolorU8 = d.ecolor;
  mv2gl.eflag = d.eflags;
  mv2gl.labels = null; // lazy
  mv2gl.loadedGen = mv2.gen;

  // Build interleaved-ish parallel buffers.
  // Node instance buffer: [x,y,r, r,g,b,a, flag] * N (8 floats).
  const ncolorF = new Float32Array(d.nodeN * 4);
  for (let i = 0; i < d.nodeN; i++) mv2UnpackColor(d.color[i], ncolorF, i * 4);
  const nflag = new Float32Array(d.nodeN);
  nflag.fill(1);
  mv2gl.nflagArr = nflag;
  mv2gl.ncolorF = ncolorF;

  // Build the picking grid (uniform grid over world bbox).
  mv2GLBuildGrid();

  // Upload node positions+radius+color into one VBO (struct of 7 floats), and
  // the flag as a separate VBO so filtering re-uploads only the flag column.
  const nodeData = new Float32Array(d.nodeN * 7);
  for (let i = 0; i < d.nodeN; i++) {
    const o = i * 7;
    nodeData[o] = d.x[i]; nodeData[o + 1] = d.y[i]; nodeData[o + 2] = d.r[i];
    nodeData[o + 3] = ncolorF[i * 4]; nodeData[o + 4] = ncolorF[i * 4 + 1];
    nodeData[o + 5] = ncolorF[i * 4 + 2]; nodeData[o + 6] = ncolorF[i * 4 + 3];
  }
  if (mv2gl.instVBO) gl.deleteBuffer(mv2gl.instVBO);
  mv2gl.instVBO = gl.createBuffer();
  gl.bindBuffer(gl.ARRAY_BUFFER, mv2gl.instVBO);
  gl.bufferData(gl.ARRAY_BUFFER, nodeData, gl.STATIC_DRAW);

  if (mv2gl.nflagVBO) gl.deleteBuffer(mv2gl.nflagVBO);
  mv2gl.nflagVBO = gl.createBuffer();
  gl.bindBuffer(gl.ARRAY_BUFFER, mv2gl.nflagVBO);
  gl.bufferData(gl.ARRAY_BUFFER, nflag, gl.DYNAMIC_DRAW);

  // Edge instance buffer: [srcx,srcy, dstx,dsty, r,g,b,a, flag] * M (9 floats).
  const edgeData = new Float32Array(d.edgeN * 9);
  const ec = new Float32Array(4);
  for (let i = 0; i < d.edgeN; i++) {
    const s = d.esrc[i], t = d.edst[i];
    mv2UnpackColor(d.ecolor[i], ec, 0);
    const o = i * 9;
    // esrc/edst are dense local node indices into the node columns.
    const valid = s < d.nodeN && t < d.nodeN;
    edgeData[o] = valid ? d.x[s] : 0; edgeData[o + 1] = valid ? d.y[s] : 0;
    edgeData[o + 2] = valid ? d.x[t] : 0; edgeData[o + 3] = valid ? d.y[t] : 0;
    edgeData[o + 4] = ec[0]; edgeData[o + 5] = ec[1]; edgeData[o + 6] = ec[2];
    edgeData[o + 7] = Math.max(0.25, ec[3] * 0.6); // slightly translucent edges
    edgeData[o + 8] = valid ? 1 : 0;               // visibility flag (filterable later)
  }
  if (mv2gl.edgeVBO) gl.deleteBuffer(mv2gl.edgeVBO);
  mv2gl.edgeVBO = gl.createBuffer();
  gl.bindBuffer(gl.ARRAY_BUFFER, mv2gl.edgeVBO);
  gl.bufferData(gl.ARRAY_BUFFER, edgeData, gl.STATIC_DRAW);
  mv2gl.edgeData = edgeData;

  mv2SetStatus('overview: ' + d.nodeN + ' clusters · ' + d.edgeN + ' edges');
}

function mv2GLBuildGrid() {
  const N = mv2gl.nodeN;
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity, maxR = 1;
  for (let i = 0; i < N; i++) {
    if (mv2gl.nx[i] < minX) minX = mv2gl.nx[i];
    if (mv2gl.ny[i] < minY) minY = mv2gl.ny[i];
    if (mv2gl.nx[i] > maxX) maxX = mv2gl.nx[i];
    if (mv2gl.ny[i] > maxY) maxY = mv2gl.ny[i];
    if (mv2gl.nr[i] > maxR) maxR = mv2gl.nr[i];
  }
  if (!isFinite(minX)) { minX = 0; minY = 0; maxX = 1; maxY = 1; }
  mv2gl.worldBBox = [minX, minY, maxX, maxY];
  const cell = Math.max(maxR * 2, (maxX - minX + 1) / Math.max(1, Math.sqrt(N)));
  const gw = Math.max(1, Math.ceil((maxX - minX + 1) / cell));
  const gh = Math.max(1, Math.ceil((maxY - minY + 1) / cell));
  const grid = new Array(gw * gh);
  for (let i = 0; i < N; i++) {
    const gx = Math.min(gw - 1, Math.floor((mv2gl.nx[i] - minX) / cell));
    const gy = Math.min(gh - 1, Math.floor((mv2gl.ny[i] - minY) / cell));
    const k = gy * gw + gx;
    (grid[k] || (grid[k] = [])).push(i);
  }
  mv2gl.grid = grid; mv2gl.gridCell = cell; mv2gl.gridW = gw; mv2gl.gridH = gh;
  mv2gl.gridMinX = minX; mv2gl.gridMinY = minY;
}

function mv2GLFitView() {
  const bb = mv2gl.worldBBox || [0, 0, 4096, 4096];
  const cx = (bb[0] + bb[2]) / 2, cy = (bb[1] + bb[3]) / 2;
  const spanW = Math.max(1, bb[2] - bb[0]);
  const spanH = Math.max(1, bb[3] - bb[1]);
  const span = Math.max(spanW, spanH);
  mv2gl.cam.x = cx; mv2gl.cam.y = cy;
  // scale chosen so the span fits in ~90% of min(vw,vh): ppw = scale*min/2,
  // and span*ppw ≈ 0.9*min -> scale ≈ 1.8/span.
  mv2gl.cam.scale = Math.max(0.01, 1.8 / span);
}

function mv2GLHover(e) {
  if (!mv2gl.grid) return;
  const rect = mv2gl.canvas.getBoundingClientRect();
  const w = mv2GLScreenToWorld(e.clientX - rect.left, e.clientY - rect.top);
  const cell = mv2gl.gridCell;
  const gx = Math.floor((w.x - mv2gl.gridMinX) / cell);
  const gy = Math.floor((w.y - mv2gl.gridMinY) / cell);
  let best = -1, bestD = Infinity;
  for (let dy = -1; dy <= 1; dy++) {
    for (let dx = -1; dx <= 1; dx++) {
      const cxg = gx + dx, cyg = gy + dy;
      if (cxg < 0 || cyg < 0 || cxg >= mv2gl.gridW || cyg >= mv2gl.gridH) continue;
      const bucket = mv2gl.grid[cyg * mv2gl.gridW + cxg];
      if (!bucket) continue;
      for (const i of bucket) {
        if (mv2gl.nflagArr && mv2gl.nflagArr[i] < 0.5) continue;
        const ddx = w.x - mv2gl.nx[i], ddy = w.y - mv2gl.ny[i];
        const d2 = ddx * ddx + ddy * ddy;
        const r = mv2gl.nr[i];
        if (d2 <= r * r && d2 < bestD) { bestD = d2; best = i; }
      }
    }
  }
  if (best !== mv2gl.hoverNode) {
    mv2gl.hoverNode = best;
    mv2GLSelectLabels();
    mv2GLRequestDraw();
  }
}

// Label SELECTION (expensive, O(N log N)): recompute the candidate node set
// (hovered + top-N largest visible discs). Called only on data/filter/hover
// change — NOT per pan/zoom frame, so the draw loop stays cheap.
function mv2GLSelectLabels() {
  const TOPN = 12;
  const candidates = [];
  if (mv2gl.hoverNode >= 0) candidates.push(mv2gl.hoverNode);
  // Partial top-N selection without a full sort: linear scan keeping a small
  // max-heap-ish list (TOPN is tiny so insertion into a sorted array is O(N*TOPN)).
  const top = []; // {i, r} sorted desc by r, length <= TOPN
  for (let i = 0; i < mv2gl.nodeN; i++) {
    if (mv2gl.nflagArr && mv2gl.nflagArr[i] < 0.5) continue;
    const r = mv2gl.nr[i];
    if (top.length < TOPN) {
      top.push({ i, r });
      if (top.length === TOPN) top.sort((a, b) => a.r - b.r); // ascending: top[0] = smallest
    } else if (r > top[0].r) {
      top[0] = { i, r };
      // re-bubble smallest to front (cheap for tiny TOPN).
      top.sort((a, b) => a.r - b.r);
    }
  }
  top.sort((a, b) => b.r - a.r); // largest first
  for (const t of top) {
    if (candidates.length >= TOPN) break;
    if (candidates.indexOf(t.i) === -1) candidates.push(t.i);
  }
  mv2gl.labelCandidates = candidates;
  // Lazily resolve label strings (best-effort) then position immediately.
  mv2GLEnsureLabels(candidates).then(() => mv2GLPositionLabels());
  mv2GLPositionLabels();
}

// Label POSITIONING (cheap, per-frame): move pooled divs to track pan/zoom.
function mv2GLPositionLabels() {
  const lh = mv2gl.labelHost;
  if (!lh) return;
  const candidates = mv2gl.labelCandidates || [];
  const ppw = mv2GLPixelsPerWorld();
  while (mv2gl.labelDivs.length < candidates.length) {
    const div = document.createElement('div');
    div.className = 'mapv2-label-div';
    lh.appendChild(div);
    mv2gl.labelDivs.push(div);
  }
  for (let k = 0; k < mv2gl.labelDivs.length; k++) {
    const div = mv2gl.labelDivs[k];
    if (k >= candidates.length) { div.style.display = 'none'; continue; }
    const i = candidates[k];
    const lbl = mv2gl.labels && mv2gl.labels[i];
    let text = lbl ? (lbl.label || lbl.path || '') : '';
    if (!text && i === mv2gl.hoverNode) text = 'cluster #' + i; // hover fallback
    if (!text) { div.style.display = 'none'; continue; }
    const sx = mv2gl.vw / 2 + (mv2gl.nx[i] - mv2gl.cam.x) * ppw;
    const sy = mv2gl.vh / 2 - (mv2gl.ny[i] - mv2gl.cam.y) * ppw;
    if (sx < -50 || sy < -20 || sx > mv2gl.vw + 50 || sy > mv2gl.vh + 20) { div.style.display = 'none'; continue; }
    div.style.display = 'block';
    div.style.transform = 'translate(' + Math.round(sx) + 'px,' + Math.round(sy) + 'px)';
    div.textContent = text;
    div.classList.toggle('mapv2-label-hover', i === mv2gl.hoverNode);
  }
}

// Resolve label strings for cluster node indices via /map/labels (best-effort).
// The HMAP binary carries NO string ids, so we query the endpoint with the
// dense node index as the id; if the backend resolves it, great — otherwise the
// label stays blank (hover still highlights + click jumps to Tree mode).
async function mv2GLEnsureLabels(indices) {
  if (!mv2gl.labels) mv2gl.labels = new Array(mv2gl.nodeN).fill(null);
  const need = indices.filter(i => mv2gl.labels[i] == null);
  if (!need.length) return;
  for (const i of need) mv2gl.labels[i] = { label: '', path: null }; // placeholder
  try {
    const ids = need.join(',');
    const resp = await mv2FetchJSON('/map/labels?gen=' + mv2.gen + '&ids=' + encodeURIComponent(ids));
    for (const i of need) {
      const info = resp && resp[String(i)];
      if (info) mv2gl.labels[i] = { label: info.label, path: info.path, status: info.status, kind: info.kind };
    }
  } catch (_) { /* labels remain blank; non-fatal */ }
}

let mv2GLRafQueued = false;
function mv2GLRequestDraw() {
  if (mv2gl.raf || mv2GLRafQueued) return;
  mv2GLRafQueued = true;
  requestAnimationFrame(() => { mv2GLRafQueued = false; mv2GLDraw(); });
}

function mv2StopGLLoop() {
  if (mv2gl.raf) { cancelAnimationFrame(mv2gl.raf); mv2gl.raf = null; }
  mv2GLRafQueued = false;
}

function mv2GLDraw() {
  const gl = mv2gl.gl;
  if (!gl || !mv2gl.ok || mv2gl.nodeN === 0) {
    if (gl) { gl.clear(gl.COLOR_BUFFER_BIT); }
    return;
  }
  gl.viewport(0, 0, mv2gl.canvas.width, mv2gl.canvas.height);
  gl.clear(gl.COLOR_BUFFER_BIT);
  const cam = mv2GLCamMatrix();

  // --- Edges first (under nodes). ---
  const pe = mv2gl.progEdge;
  gl.useProgram(pe.prog);
  gl.uniformMatrix3fv(pe.u_cam, false, cam);
  gl.uniform1f(pe.u_halfW, 1.2 / mv2GLPixelsPerWorld()); // ~1.2px line in world units
  // unit quad (loc 0).
  gl.bindBuffer(gl.ARRAY_BUFFER, mv2gl.quadVBO);
  gl.enableVertexAttribArray(0);
  gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 0, 0);
  gl.vertexAttribDivisor(0, 0);
  // edge instance buffer: 9 floats stride.
  const eStride = 9 * 4;
  gl.bindBuffer(gl.ARRAY_BUFFER, mv2gl.edgeVBO);
  gl.enableVertexAttribArray(1); gl.vertexAttribPointer(1, 2, gl.FLOAT, false, eStride, 0); gl.vertexAttribDivisor(1, 1);
  gl.enableVertexAttribArray(2); gl.vertexAttribPointer(2, 2, gl.FLOAT, false, eStride, 2 * 4); gl.vertexAttribDivisor(2, 1);
  gl.enableVertexAttribArray(3); gl.vertexAttribPointer(3, 4, gl.FLOAT, false, eStride, 4 * 4); gl.vertexAttribDivisor(3, 1);
  gl.enableVertexAttribArray(4); gl.vertexAttribPointer(4, 1, gl.FLOAT, false, eStride, 8 * 4); gl.vertexAttribDivisor(4, 1);
  gl.drawArraysInstanced(gl.TRIANGLE_STRIP, 0, 4, mv2gl.edgeN);

  // --- Nodes. ---
  const pn = mv2gl.progNode;
  gl.useProgram(pn.prog);
  gl.uniformMatrix3fv(pn.u_cam, false, cam);
  gl.uniform1f(pn.u_minPx, 0.75);
  gl.uniform2f(pn.u_viewport, mv2gl.vw, mv2gl.vh);
  gl.bindBuffer(gl.ARRAY_BUFFER, mv2gl.quadVBO);
  gl.enableVertexAttribArray(0);
  gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 0, 0);
  gl.vertexAttribDivisor(0, 0);
  const nStride = 7 * 4;
  gl.bindBuffer(gl.ARRAY_BUFFER, mv2gl.instVBO);
  gl.enableVertexAttribArray(1); gl.vertexAttribPointer(1, 2, gl.FLOAT, false, nStride, 0); gl.vertexAttribDivisor(1, 1);
  gl.enableVertexAttribArray(2); gl.vertexAttribPointer(2, 1, gl.FLOAT, false, nStride, 2 * 4); gl.vertexAttribDivisor(2, 1);
  gl.enableVertexAttribArray(3); gl.vertexAttribPointer(3, 4, gl.FLOAT, false, nStride, 3 * 4); gl.vertexAttribDivisor(3, 1);
  gl.bindBuffer(gl.ARRAY_BUFFER, mv2gl.nflagVBO);
  gl.enableVertexAttribArray(4); gl.vertexAttribPointer(4, 1, gl.FLOAT, false, 0, 0); gl.vertexAttribDivisor(4, 1);
  gl.drawArraysInstanced(gl.TRIANGLE_STRIP, 0, 4, mv2gl.nodeN);

  // Reposition labels (cheap) to track pan/zoom; selection is cached.
  mv2GLPositionLabels();
}

// Filter clusters via the flag column + bufferSubData (no geometry re-upload).
function mv2GLSetFilter(predicate) {
  if (!mv2gl.nflagArr) return;
  for (let i = 0; i < mv2gl.nodeN; i++) {
    mv2gl.nflagArr[i] = predicate(i) ? 1 : 0;
  }
  const gl = mv2gl.gl;
  gl.bindBuffer(gl.ARRAY_BUFFER, mv2gl.nflagVBO);
  gl.bufferSubData(gl.ARRAY_BUFFER, 0, mv2gl.nflagArr);
  mv2GLSelectLabels();
  mv2GLRequestDraw();
}

// Click cluster -> Tree mode focused at the cluster's path.
function mv2GLJumpToTree(path) {
  const modeSel = document.getElementById('mapv2-mode');
  if (modeSel) modeSel.value = 'tree';
  mv2SetMode('tree');
  // Best-effort: expand toward the path (search reuses the expand machinery).
  if (path) {
    const search = document.getElementById('mapv2-search');
    if (search) { search.value = path; mv2RunSearch(path); }
  }
}

// collapseSingleStatus folds a page's status node into the page when that page
// produced exactly one status. The page is colored by that status (status/kind/
// error copied onto it) and any transition edge that pointed at the status node
// is rewired to the page. Pages with 2+ statuses are left untouched so their
// distinct outcomes remain visible. Returns {nodes, edges} (new arrays).
function collapseSingleStatus(allNodes, allEdges) {
  const byId = new Map(allNodes.map(n => [n.id, n]));
  // Count status children per page via "renders" edges (page -> status, label '').
  const statusChildren = new Map(); // pageId -> [statusId,...]
  for (const e of allEdges) {
    if (e.label === '' || e.label == null) {
      const from = byId.get(e.from), to = byId.get(e.to);
      if (from && to && from.type === 'page' && to.type === 'status') {
        if (!statusChildren.has(e.from)) statusChildren.set(e.from, []);
        statusChildren.get(e.from).push(e.to);
      }
    }
  }

  // Decide which status nodes to absorb, and into which page.
  const absorb = new Map(); // statusId -> pageId
  for (const [pageId, kids] of statusChildren) {
    if (kids.length === 1) {
      const sid = kids[0];
      const page = byId.get(pageId), st = byId.get(sid);
      if (page && st) {
        absorb.set(sid, pageId);
        // Color the page by its single status.
        page.status = st.status;
        page.error = st.error;
        page.collapsedStatus = st.status; // marker the renderer can use
        if (st.error) page.kind = 'error';
      }
    }
  }
  if (absorb.size === 0) return { nodes: allNodes, edges: allEdges };

  const map = (id) => absorb.get(id) || id; // status -> its page, else unchanged
  const nodes = allNodes.filter(n => !absorb.has(n.id));
  const seen = new Set();
  const edges = [];
  for (const e of allEdges) {
    // Drop the renders edge that fed an absorbed status node.
    if ((e.label === '' || e.label == null) && absorb.has(e.to)) continue;
    const from = map(e.from), to = map(e.to);
    if (from === to && (e.label === '' || e.label == null)) continue; // self renders -> noise
    const key = from + '|' + to + '|' + (e.label || '');
    if (seen.has(key)) {
      // merge counts into the existing edge
      const ex = edges.find(x => x._key === key);
      if (ex) { ex.count = (ex.count || 0) + (e.count || 0); ex.error = ex.error || e.error; }
      continue;
    }
    seen.add(key);
    edges.push(Object.assign({}, e, { from, to, _key: key }));
  }
  return { nodes, edges };
}

function makeArrowMarker(id, color) {
  const m = svgEl('marker', {
    id, markerWidth: '8', markerHeight: '8', refX: '7', refY: '3',
    orient: 'auto', markerUnits: 'userSpaceOnUse'
  });
  m.appendChild(svgEl('path', { d: 'M0,0 L7,3 L0,6 Z', fill: color }));
  return m;
}

// ===================================================================
// Force-directed layout with Barnes-Hut (quadtree) repulsion.
// O(n log n) per tick. Nodes are draggable; sim animates via rAF.
// ===================================================================

// --- Quadtree for Barnes-Hut. Each node carries mass=1; internal quads
// accumulate total mass and center-of-mass. Subdivides at capacity 1. ---
class Quadtree {
  constructor(x, y, w, h) {
    this.x = x; this.y = y; this.w = w; this.h = h; // bounds
    this.mass = 0; this.cx = 0; this.cy = 0;        // total mass + COM
    this.body = null;   // single body when leaf-with-1
    this.divided = false;
    this.nw = this.ne = this.sw = this.se = null;
  }
  _quadFor(px, py) {
    const mx = this.x + this.w / 2, my = this.y + this.h / 2;
    if (px < mx) return py < my ? 'nw' : 'sw';
    return py < my ? 'ne' : 'se';
  }
  _subdivide() {
    const hw = this.w / 2, hh = this.h / 2;
    this.nw = new Quadtree(this.x, this.y, hw, hh);
    this.ne = new Quadtree(this.x + hw, this.y, hw, hh);
    this.sw = new Quadtree(this.x, this.y + hh, hw, hh);
    this.se = new Quadtree(this.x + hw, this.y + hh, hw, hh);
    this.divided = true;
  }
  insert(b) {
    // Update running COM + mass for this quad.
    const m = this.mass + 1;
    this.cx = (this.cx * this.mass + b.x) / m;
    this.cy = (this.cy * this.mass + b.y) / m;
    this.mass = m;

    if (!this.divided && this.body === null) { this.body = b; return; }
    if (!this.divided) {
      // Was a leaf holding one body; push it down then subdivide.
      const old = this.body; this.body = null;
      this._subdivide();
      this._insertChild(old);
    }
    this._insertChild(b);
  }
  _insertChild(b) {
    const q = this._quadFor(b.x, b.y);
    // Guard against zero-size quads (coincident points): nudge slightly.
    if (this.w < 0.001 || this.h < 0.001) return;
    this[q].insert(b);
  }
}

// Walk the tree to accumulate repulsion on `node`. Treat a quad as a single
// mass at its COM when it is a leaf or (quadWidth/dist < theta).
function bhRepulse(tree, node, theta, acc) {
  if (!tree || tree.mass === 0) return;
  let dx = node.x - tree.cx;
  let dy = node.y - tree.cy;
  let d2 = dx * dx + dy * dy;
  if (d2 < 0.01) { // coincident: random jitter to break symmetry
    dx = (Math.random() - 0.5) * 0.1;
    dy = (Math.random() - 0.5) * 0.1;
    d2 = dx * dx + dy * dy + 0.01;
  }
  const isLeaf = !tree.divided;
  if (isLeaf || (tree.w / Math.sqrt(d2)) < theta) {
    if (isLeaf && tree.body === node) return; // skip self
    const dist = Math.sqrt(d2);
    const f = FS_K_REPULSE * fsSpacing * tree.mass / d2;
    acc.fx += (dx / dist) * f;
    acc.fy += (dy / dist) * f;
    return;
  }
  bhRepulse(tree.nw, node, theta, acc);
  bhRepulse(tree.ne, node, theta, acc);
  bhRepulse(tree.sw, node, theta, acc);
  bhRepulse(tree.se, node, theta, acc);
}

function initForceSim(nodes, edges, nodeLayer, edgeLayer) {
  const idx = new Map();
  fsNodes = nodes.map((n, i) => {
    idx.set(n.id, i);
    const isStatus = n.type === 'status';
    // Seed on a spiral/circle so the sim spreads out and converges quickly.
    const ang = (i / nodes.length) * Math.PI * 2 * 5;
    const rad = 30 + (i / nodes.length) * Math.min(fsWidth, fsHeight) * 0.45;
    return {
      id: n.id, n,
      x: fsWidth / 2 + Math.cos(ang) * rad + (Math.random() - 0.5) * 20,
      y: fsHeight / 2 + Math.sin(ang) * rad + (Math.random() - 0.5) * 20,
      vx: 0, vy: 0, pinned: false,
      g: null, isStatus,
      w: isStatus ? MAP_STATUS_R * 2 : MAP_PAGE_W,
      h: isStatus ? MAP_STATUS_R * 2 : MAP_PAGE_H,
    };
  });

  fsEdges = [];
  for (const e of edges) {
    const fi = idx.get(e.from), ti = idx.get(e.to);
    if (fi === undefined || ti === undefined) continue;
    const renders = e.label === '' || e.label == null;
    fsEdges.push({
      from: fi, to: ti, e,
      rest: renders ? FS_REST_RENDERS : FS_REST_TRANSITION,
      renders,
    });
  }

  // Build the SVG elements (positions filled in each tick).
  for (const fe of fsEdges) drawEdge(edgeLayer, fe);
  for (const fn of fsNodes) drawNode(nodeLayer, fn);

  fsAlpha = FS_ALPHA_START;
  fsTicks = 0;
  // Initial paint so things aren't at 0,0 before the first frame.
  fsApplyPositions();
}

function fsTick() {
  // 1) Build the quadtree over current positions.
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  for (const n of fsNodes) {
    if (n.x < minX) minX = n.x;
    if (n.y < minY) minY = n.y;
    if (n.x > maxX) maxX = n.x;
    if (n.y > maxY) maxY = n.y;
  }
  const pad = 10;
  const w = Math.max(maxX - minX, 1) + pad * 2;
  const h = Math.max(maxY - minY, 1) + pad * 2;
  const size = Math.max(w, h); // square root quad
  const tree = new Quadtree(minX - pad, minY - pad, size, size);
  for (const n of fsNodes) tree.insert(n);

  // 2) Repulsion (Barnes-Hut) + gravity, scaled by alpha.
  const cx = fsWidth / 2, cy = fsHeight / 2;
  for (const n of fsNodes) {
    if (n.pinned) { n.vx = 0; n.vy = 0; continue; }
    const acc = { fx: 0, fy: 0 };
    bhRepulse(tree, n, FS_THETA, acc);
    // Gravity toward center.
    acc.fx += (cx - n.x) * FS_GRAVITY;
    acc.fy += (cy - n.y) * FS_GRAVITY;
    n.vx = (n.vx + acc.fx * fsAlpha) * FS_DAMPING;
    n.vy = (n.vy + acc.fy * fsAlpha) * FS_DAMPING;
  }

  // 3) Spring attraction along edges.
  for (const fe of fsEdges) {
    const a = fsNodes[fe.from], b = fsNodes[fe.to];
    if (a === b) continue;
    let dx = b.x - a.x, dy = b.y - a.y;
    let dist = Math.sqrt(dx * dx + dy * dy) || 0.01;
    const f = FS_K_SPRING * (dist - fe.rest * fsSpacing) * fsAlpha;
    const ux = dx / dist, uy = dy / dist;
    if (!a.pinned) { a.vx += ux * f; a.vy += uy * f; }
    if (!b.pinned) { b.vx -= ux * f; b.vy -= uy * f; }
  }

  // 4) Integrate.
  for (const n of fsNodes) {
    if (n.pinned) continue;
    n.x += n.vx;
    n.y += n.vy;
  }

  fsApplyPositions();

  fsAlpha *= FS_ALPHA_DECAY;
  fsTicks++;
}

function fsApplyPositions() {
  // Update node group transforms (centered on x,y).
  for (const n of fsNodes) {
    if (!n.g) continue;
    n.g.setAttribute('transform', `translate(${n.x},${n.y})`);
  }
  // Update edge geometry.
  for (const fe of fsEdges) {
    const a = fsNodes[fe.from], b = fsNodes[fe.to];
    if (fe.path) {
      if (a === b) {
        const r = 26;
        fe.path.setAttribute('d',
          `M${a.x + 6},${a.y - 4} C${a.x + r},${a.y - r} ${a.x + r},${a.y + r} ${a.x + 6},${a.y + 4}`);
      } else {
        fe.path.setAttribute('d', `M${a.x},${a.y} L${b.x},${b.y}`);
      }
    }
    if (fe.label) {
      fe.label.setAttribute('x', (a.x + b.x) / 2);
      fe.label.setAttribute('y', (a.y + b.y) / 2 - 4);
    }
  }
}

function startForceSim() {
  stopForceSim();
  const loop = () => {
    fsTick();
    if (fsAlpha > FS_ALPHA_MIN && fsTicks < FS_MAX_TICKS) {
      fsRaf = requestAnimationFrame(loop);
    } else {
      fsRaf = null;
    }
  };
  fsRaf = requestAnimationFrame(loop);
}

function stopForceSim() {
  if (fsRaf) { cancelAnimationFrame(fsRaf); fsRaf = null; }
}

function reheatSim() {
  fsAlpha = Math.max(fsAlpha, FS_REHEAT);
  fsTicks = 0;
  if (!fsRaf) startForceSim();
}

// --- Node colors ---
// page nodes: subtle fill by kind, neutral border.
function nodeFill(n) {
  switch (n.kind) {
    case 'html': return '#242424';
    case 'json': return '#23302a';
    case 'redirect': return '#1f2d3a';
    case 'error': return '#33201f';
    default: return '#2a2a2a';
  }
}
function nodeStroke(n) {
  if (n.error) return '#ff5555';
  // A collapsed single-status page is bordered by its status-class color so its
  // outcome reads at a glance without a separate status node.
  if (n.collapsedStatus != null) return statusStroke(n);
  return '#555';
}
// status nodes: pill colored by status class.
function statusFill(n) {
  const s = n.status;
  if (n.error || s === 0) return '#ff5555';
  if (s >= 500) return '#ff5555';
  if (s >= 400) return '#cc7a00';
  if (s >= 300) return '#5b8db8';
  if (s >= 200) return '#1f7a3f';
  return '#444';
}
function statusStroke(n) {
  const s = n.status;
  if (n.error || s === 0 || s >= 500) return '#ff5555';
  if (s >= 400) return '#ffa500';
  if (s >= 300) return '#7fb0d8';
  if (s >= 200) return '#85e89d';
  return '#666';
}

// Draw a node. `fn` is a force-sim node ({n, isStatus, w, h, ...}).
function drawNode(layer, fn) {
  const n = fn.n;
  const g = svgEl('g', { class: 'map-node' });
  g.style.cursor = 'grab';

  const parts = [];
  parts.push(n.label || '(state)');
  if (n.type) parts.push('type=' + n.type);
  if (n.status != null) parts.push('status=' + n.status);
  if (n.kind) parts.push('kind=' + n.kind);
  parts.push('×' + (n.count || 0));
  const titleText = parts.join('\n');

  if (fn.isStatus) {
    const circle = svgEl('circle', {
      cx: 0, cy: 0, r: MAP_STATUS_R, class: 'map-status-node',
      fill: statusFill(n), stroke: statusStroke(n), 'stroke-width': n.error ? 2 : 1.5
    });
    g.appendChild(circle);
    const t = svgEl('text', { x: 0, y: 0, class: 'map-status-text' });
    t.textContent = truncate(String(n.label != null ? n.label : (n.status != null ? n.status : '')), 4);
    g.appendChild(t);
  } else {
    const rect = svgEl('rect', {
      x: -MAP_PAGE_W / 2, y: -MAP_PAGE_H / 2, width: MAP_PAGE_W, height: MAP_PAGE_H,
      rx: 4, ry: 4, class: 'map-page-node',
      fill: nodeFill(n), stroke: nodeStroke(n), 'stroke-width': n.error ? 2 : 1
    });
    g.appendChild(rect);
    const label = svgEl('text', { x: 0, y: 0, class: 'map-node-label' });
    label.textContent = truncate(n.label || '(page)', 20);
    g.appendChild(label);
  }

  const title = svgEl('title');
  title.textContent = titleText;
  g.appendChild(title);

  // Click -> inspect the sample flow. Distinguish click from drag below.
  attachNodeDrag(g, fn, n);

  fn.g = g;
  layer.appendChild(g);
}

// Pointer-based drag: pin the node to the pointer (graph-space) while held,
// reheat the sim, and stopPropagation so the background pan doesn't fire.
// A press with negligible movement is treated as a click -> inspectFlow.
function attachNodeDrag(g, fn, n) {
  let moved = false;
  let startX = 0, startY = 0;

  // Convert a client point to graph-space (account for pan/zoom transform).
  const toGraph = (clientX, clientY) => {
    const rect = mapSvg.getBoundingClientRect();
    const sx = clientX - rect.left;
    const sy = clientY - rect.top;
    return {
      x: (sx - mapView.tx) / mapView.scale,
      y: (sy - mapView.ty) / mapView.scale,
    };
  };

  g.addEventListener('pointerdown', (ev) => {
    ev.stopPropagation();
    moved = false;
    startX = ev.clientX; startY = ev.clientY;
    fn.pinned = true;
    fn.vx = 0; fn.vy = 0;
    const p = toGraph(ev.clientX, ev.clientY);
    fn.x = p.x; fn.y = p.y;
    g.style.cursor = 'grabbing';
    try { g.setPointerCapture(ev.pointerId); } catch (_) {}
    reheatSim();
  });
  g.addEventListener('pointermove', (ev) => {
    if (!fn.pinned) return;
    ev.stopPropagation();
    if (Math.abs(ev.clientX - startX) > 3 || Math.abs(ev.clientY - startY) > 3) moved = true;
    const p = toGraph(ev.clientX, ev.clientY);
    fn.x = p.x; fn.y = p.y;
    fn.vx = 0; fn.vy = 0;
    fsApplyPositions();
  });
  const end = (ev) => {
    if (!fn.pinned) return;
    ev.stopPropagation();
    fn.pinned = false;
    g.style.cursor = 'grab';
    try { g.releasePointerCapture(ev.pointerId); } catch (_) {}
    reheatSim();
    if (!moved && n.sampleFlowId != null) {
      inspectFlow(n.sampleFlowId);
      showTab('inspect');
    }
  };
  g.addEventListener('pointerup', end);
  g.addEventListener('pointercancel', end);
}

// Draw an edge. `fe` is a force-sim edge ({e, from, to, renders, ...}).
function drawEdge(layer, fe) {
  const e = fe.e;
  const color = e.error ? '#ff5555' : (fe.renders ? '#555' : '#888');
  const marker = e.error ? 'url(#map-arrow-err)' : 'url(#map-arrow)';

  const path = svgEl('path', {
    d: 'M0,0 L0,0', fill: 'none', stroke: color,
    'stroke-width': fe.renders ? 0.8 : 1.2,
    class: fe.renders ? 'map-renders-edge' : 'map-transition-edge'
  });
  if (!fe.renders) path.setAttribute('marker-end', marker);
  if (fe.renders) path.setAttribute('stroke-opacity', '0.4');
  if (e.async) path.setAttribute('stroke-dasharray', '5,4');

  const title = svgEl('title');
  title.textContent = (e.label || '(renders)') + '  (×' + (e.count || 0) + ')';
  path.appendChild(title);
  path.style.cursor = 'pointer';
  path.addEventListener('click', (ev) => { ev.stopPropagation(); showEdgeFlows(e, ev); });
  layer.appendChild(path);
  fe.path = path;

  // Transition edges get a midpoint label.
  if (!fe.renders && e.label) {
    const lbl = svgEl('text', { x: 0, y: 0, class: 'map-edge-label' });
    lbl.textContent = truncate(e.label, 24);
    const lt = svgEl('title');
    lt.textContent = (e.label || '') + '  (×' + (e.count || 0) + ')';
    lbl.appendChild(lt);
    lbl.style.cursor = 'pointer';
    lbl.addEventListener('click', (ev) => { ev.stopPropagation(); showEdgeFlows(e, ev); });
    layer.appendChild(lbl);
    fe.label = lbl;
  }
}

// --- Pan / zoom ---

function applyMapTransform() {
  if (mapG) mapG.setAttribute('transform',
    `translate(${mapView.tx},${mapView.ty}) scale(${mapView.scale})`);
}

function fitMapView() {
  mapView = { tx: 40, ty: 40, scale: 1 };
}

function attachMapInteractions(svg) {
  let dragging = false;
  let lastX = 0, lastY = 0;

  svg.addEventListener('pointerdown', (e) => {
    // Only background drag (not when a node/edge handled it — they stopPropagation).
    dragging = true;
    lastX = e.clientX; lastY = e.clientY;
    svg.style.cursor = 'grabbing';
    svg.setPointerCapture(e.pointerId);
  });
  svg.addEventListener('pointermove', (e) => {
    if (!dragging) return;
    mapView.tx += (e.clientX - lastX);
    mapView.ty += (e.clientY - lastY);
    lastX = e.clientX; lastY = e.clientY;
    applyMapTransform();
  });
  const endDrag = (e) => {
    dragging = false;
    svg.style.cursor = 'grab';
    try { svg.releasePointerCapture(e.pointerId); } catch (_) {}
  };
  svg.addEventListener('pointerup', endDrag);
  svg.addEventListener('pointercancel', endDrag);

  svg.addEventListener('wheel', (e) => {
    e.preventDefault();
    const rect = svg.getBoundingClientRect();
    const cx = e.clientX - rect.left;
    const cy = e.clientY - rect.top;
    const factor = e.deltaY < 0 ? 1.1 : 1 / 1.1;
    let ns = mapView.scale * factor;
    ns = Math.max(0.2, Math.min(3, ns));
    const k = ns / mapView.scale;
    // Zoom toward the cursor: keep the point under the cursor fixed.
    mapView.tx = cx - (cx - mapView.tx) * k;
    mapView.ty = cy - (cy - mapView.ty) * k;
    mapView.scale = ns;
    applyMapTransform();
  }, { passive: false });
}

// --- Edge flows floating panel ---

function removeEdgeFlowsPanel() {
  const old = document.getElementById('map-edge-flows');
  if (old) old.remove();
}

function showEdgeFlows(e, ev) {
  removeEdgeFlowsPanel();
  // The edge-flows popover anchors inside the neighborhood pane (Map v2).
  const canvas = document.getElementById('mapv2-neighborhood');
  if (!canvas) return;

  const panel = document.createElement('div');
  panel.id = 'map-edge-flows';
  panel.className = 'map-edge-flows';

  const head = document.createElement('div');
  head.className = 'map-ef-head';
  head.textContent = truncate(e.label || 'transition', 40);
  panel.appendChild(head);

  const sub = document.createElement('div');
  sub.className = 'map-ef-sub';
  sub.textContent = (e.flowIds ? e.flowIds.length : 0) + ' flow(s) · ×' + (e.count || 0);
  panel.appendChild(sub);

  const list = document.createElement('div');
  list.className = 'map-ef-list';
  (e.flowIds || []).forEach(id => {
    const row = document.createElement('div');
    row.className = 'map-ef-row';
    row.textContent = '#' + id + '  ' + truncate(e.label || '', 30);
    row.onclick = () => { inspectFlow(id); showTab('inspect'); removeEdgeFlowsPanel(); };
    list.appendChild(row);
  });
  if (!(e.flowIds && e.flowIds.length)) {
    const none = document.createElement('div');
    none.className = 'map-ef-row';
    none.textContent = '(no flow ids)';
    list.appendChild(none);
  }
  panel.appendChild(list);

  const close = document.createElement('button');
  close.className = 'map-ef-close';
  close.textContent = 'close';
  close.onclick = removeEdgeFlowsPanel;
  panel.appendChild(close);

  // Position near the click, clamped inside the canvas.
  const rect = canvas.getBoundingClientRect();
  let px = ev.clientX - rect.left + 8;
  let py = ev.clientY - rect.top + 8;
  px = Math.min(px, rect.width - 240);
  py = Math.min(py, rect.height - 200);
  panel.style.left = Math.max(4, px) + 'px';
  panel.style.top = Math.max(4, py) + 'px';

  canvas.appendChild(panel);
}

// --- Live refresh: debounced rebuild when new flows arrive while map open. ---

function mapIsVisible() {
  const v = document.getElementById('map-view');
  return v && v.style.display !== 'none';
}

function mapOnFlowEvent() {
  if (!mapIsVisible()) return;
  if (mapRebuildTimer) return; // already scheduled
  // Debounced refresh: re-check /map/meta; mv2Enter reloads the icicle if the
  // snapshot generation advanced (it serves stale during rebuild otherwise).
  mapRebuildTimer = setTimeout(() => {
    mapRebuildTimer = null;
    if (mapIsVisible()) mv2Enter();
  }, 1500);
}

// Hook the existing SSE without altering its core behavior: wrap the
// already-assigned es.onmessage so new flow events also nudge the map.
if (typeof es !== 'undefined' && es) {
  const _origMapOnMsg = es.onmessage;
  es.onmessage = function (e) {
    if (_origMapOnMsg) _origMapOnMsg.call(this, e);
    mapOnFlowEvent();
  };
}
