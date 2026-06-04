const flows = new Map();
let flowList;

let currentFlowId;
let interceptedFlowId = null;
let selectedFlowId;

// Central tracking address matching your proxy instantiation logic
const ACTIVE_LISTENER = "http://127.0.0.1:9002";

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
    if (flowList) flowList.innerHTML = '';
    
    history.forEach(flow => {
      renderFlowItem(flow);
    });
  } catch (err) {
    console.error("Failed to load history:", err);
  }
}

function renderFlowItem(flow) {
  flows.set(flow.id, flow);
  if (!flowList) return;

  let div = document.getElementById("flow-item-" + flow.id);
  let Id = document.getElementById("flow-item-" + flow.id + "-id");
  let method = document.getElementById("flow-item-" + flow.id + "-method");
  let url = document.getElementById("flow-item-" + flow.id + "-url");
  let HTTPstatus = document.getElementById("flow-item-" + flow.id + "-status");

  if (!div) {
    div = document.createElement('div');
    div.id = "flow-item-" + flow.id;
    div.className = 'flow-item';

    const siblings = Array.from(flowList.children);
    const successor = siblings.find(sib => {
      const sibId = sib.id.replace('flow-item-', '');
      return Number(flow.id) > Number(sibId);
    });

    if (successor) {
      flowList.insertBefore(div, successor);
    } else {
      flowList.appendChild(div);
    }

    function createElement(name, parent, content) {
      const element = document.createElement('div');
      element.id = 'flow-item-' + flow.id + `-${name}`;
      element.className = `flow-item-${name}`;
      parent.appendChild(element);

      const elementText = document.createTextNode(content);
      element.appendChild(elementText);
      return element;
    }

    Id = createElement('id', div, flow.id);
    method = createElement('method', div, flow.method);
    url = createElement('url', div, flow.url);
    HTTPstatus = createElement('status', div, flow.status || '-');
  } else {
    Id = document.getElementById("flow-item-" + flow.id + "-id");
    method = document.getElementById("flow-item-" + flow.id + "-method");
    url = document.getElementById("flow-item-" + flow.id + "-url");
    HTTPstatus = document.getElementById("flow-item-" + flow.id + "-status");
  }

  // FIX: Swapped nodeValue updates to modern textContent targeting to prevent empty lifecycle rendering errors
  if (flow.id !== undefined) Id.textContent = flow.id;
  if (flow.method !== undefined) method.textContent = flow.method;
  if (flow.url !== undefined) url.textContent = flow.url;
  if (flow.status !== undefined) HTTPstatus.textContent = flow.status || '-';

  if (flow.status >= 400) {
    HTTPstatus.style.color = "#ff0000";
  } else if (flow.status >= 300) {
    HTTPstatus.style.color = "#ffa500";
  } else if (flow.status >= 200) {
    HTTPstatus.style.color = "#00ff00";
  }

  div.onclick = () => {
    inspectFlow(flow.id);
    const flowDisplay = document.getElementById("selected-flow");
    if (flowDisplay) {
      flowDisplay.innerHTML = `${flow.id} ${flow.method} ${flow.url} ${flow.status || ''}`;
    }
    const repeatArea = document.getElementById("repeat-request");
    if (repeatArea) {
      repeatArea.value = flows.get(flow.id).request || '';
    }
  };
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

function buildSitemap() {
  const sitemapView = document.getElementById('sitemap-view');
  if (!sitemapView) return;
  sitemapView.innerHTML = '';

  const tree = {};

  flows.forEach(flow => {
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

  sitemapView.appendChild(container);
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
      inspectFlow(data._flowId);
    };
    return leafDiv;
  }

  const details = document.createElement('details');
  details.style.marginLeft = "1.5ex";

  const summary = document.createElement('summary');
  summary.style.cursor = "pointer";
  summary.style.padding = "2px 5px";
  summary.textContent = name;

  details.appendChild(summary);

  const childKeys = Object.keys(data.children).sort();
  childKeys.forEach(key => {
    details.appendChild(renderNode(key, data.children[key]));
  });

  if (name.includes('.')) details.open = true;

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
      row.innerHTML = `
          <td><code>${file}</code></td>
          <td align="right"><a href="/ca/download?name=${file}" style="color:#85e89d;">Download</a></td>
      `;
    });

    listSection.appendChild(table);
    caView.appendChild(listSection);

  } catch (err) {
    caView.innerHTML += `<p style="color:red;">Error loading certs: ${err}</p>`;
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
}

function addElement(tag, text, parent, className = "", id = "") {
  const el = document.createElement(tag);
  if (className) el.className = className;
  if (id) el.id = id;
  if (text) el.textContent = text;
  if (parent) parent.appendChild(el);
  return el;
}

function renderDetails(flow) {
  const details = document.getElementById('details');
  if (!details) return;

  const req = flow.request || "";
  const reqBody = flow.requestBody || "";
  const res = flow.responseBody || "";
  const resBody = flow.response || "";

  details.innerHTML = ""; // Clear loader explicitly

  addElement("div", `Request ${flow.requestTime || ''}`, details, "h3", "inspect-request");
  addElement("pre", req, details);
  if (reqBody) addElement("pre", reqBody, details);
  
  addElement("div", `Response ${flow.responseTime || ''}`, details, "h3", "inspect-response");
  if (res || resBody) {
    if (resBody) addElement("pre", resBody, details);
    if (res) addElement("pre", res, details);
  } else {
    addElement("p", "Waiting for response...", details);
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
    renderDetails(flow);

  } catch (err) {
    details.innerHTML = "<span style='color:#ff0000; font-weight:bold;'>Error loading flow: " + err.message + "</span>";
    console.error("Inspect flow crash details:", err);
  }

  updateUrlState('inspect');
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
