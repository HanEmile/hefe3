const flows = new Map();
let flowList;

let currentFlowId;
let interceptedFlowId = null;
let selectedFlowId;

async function loadHistory() {
  try {
    const res = await fetch('/history');
    const history = await res.json();
    history.forEach(flow => {
      f = JSON.parse(flow)
      renderFlowItem(f);
    });
  } catch (err) {
    console.error("Failed to load history:", err);
  }
}

function renderFlowItem(flow) {
  flows.set(flow.id, flow);
  if (!flowList) return;
  // console.log("rendering", flow)

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

      elementText = document.createTextNode(content);
      element.appendChild(elementText);
      return element;
    }

    Id = createElement('id', div, flow.id);
    method = createElement('method', div, flow.method);
    url = createElement('url', div, flow.url);
    HTTPstatus = createElement('status', div, flow.status);
  } else {
    Id = document.getElementById("flow-item-" + flow.id + "-id");
    method = document.getElementById("flow-item-" + flow.id + "-method");
    url = document.getElementById("flow-item-" + flow.id + "-url");
    HTTPstatus = document.getElementById("flow-item-" + flow.id + "-status");
  }

  if (flow.id) Id.childNodes[0].nodeValue = flow.id;
  if (flow.method) method.childNodes[0].nodeValue = flow.method;
  if (flow.url) url.childNodes[0].nodeValue = flow.url;
  if (flow.status) HTTPstatus.childNodes[0].nodeValue = flow.status;

  if (flow.status >= 400) {
    HTTPstatus.style.color = "#ff0000";
  } else if (flow.status >= 300) {
    HTTPstatus.style.color = "#ffa500";
  } else if (flow.status >= 200) {
    HTTPstatus.style.color = "#00ff00";
  }

  div.onclick = () => {
    inspectFlow(flow.id);
    document.getElementById("selected-flow").innerHTML = `${flow.id} ${flow.method} ${flow.url} ${flow.status}`;
    document.getElementById("repeat-request").value = atob(flows.get(flow.id).request);
  };
}

function autoPopulateIntercept(flow) {
  interceptedFlowId = flow.id;
  const textarea = document.querySelector('#intercept-view textarea');
  textarea.value = atob(flow.request);
  // Optional: auto-switch to intercept tab
  // showTab('intercept');
}

const es = new EventSource("/events");
es.onmessage = (e) => {
  const flow = JSON.parse(e.data);

  renderFlowItem(flow);

  // If a request has no response yet, it might be an intercepted one
  if (!flow.response && document.getElementById("enable-intercept").style.display === "none") {
    autoPopulateIntercept(flow);
  }
  if (currentFlowId == flow.id) {
    renderDetails(flow);
  }


};
es.onerror = (e) => {
  console.error('EventSource error:', e);
};

function buildSitemap() {
  const sitemapView = document.getElementById('sitemap-view');
  // Clear previous view but keep the header if you have one
  sitemapView.innerHTML = '';

  const tree = {};

  // 1. Build the tree structure from flows
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
        // If it's the last part of the path, it's a "leaf" (file/endpoint)
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

  // 2. Recursively render the tree using details/summary
  const container = document.createElement('div');
  // container.style.padding = "1ex";

  Object.keys(tree).sort().forEach(host => {
    container.appendChild(renderNode(host, tree[host]));
  });

  sitemapView.appendChild(container);
}

function renderNode(name, data) {
  // 1. If it's a leaf node, don't use <details>/<summary>
  if (data._isLeaf && Object.keys(data.children).length === 0) {
    const leafDiv = document.createElement('div');
    leafDiv.className = 'sitemap-leaf';
    // leafDiv.textContent = "📄 " + name;
    leafDiv.textContent = name + " | " + data.method + " " + data.status;
    leafDiv.style.cursor = "pointer";
    leafDiv.style.padding = "2px 5px";
    leafDiv.style.marginLeft = "3.5ex"; // Offset to align with parent text
    // leafDiv.style.color = "#85e89d";
    if (data.status >= 400) {
      leafDiv.style.color = "#ff0000";
    } else if (data.status >= 300) {
      leafDiv.style.color = "#ffa500";
    } else if (data.status >= 200) {
      leafDiv.style.color = "#00ff00";
    }

    leafDiv.onclick = () => {
      inspectFlow(data._flowId);
      // showTab('inspect');
    };
    return leafDiv;
  }

  // 2. If it's a directory, use <details> and <summary>
  const details = document.createElement('details');
  details.style.marginLeft = "1.5ex";

  const summary = document.createElement('summary');
  summary.style.cursor = "pointer";
  summary.style.padding = "2px 5px";
  // summary.textContent = "📁 " + name;
  summary.textContent = name;

  details.appendChild(summary);

  // 3. Recursively add children
  const childKeys = Object.keys(data.children).sort();
  childKeys.forEach(key => {
    details.appendChild(renderNode(key, data.children[key]));
  });

  // Auto-expand the root domains
  if (name.includes('.')) details.open = true;

  return details;
}

async function loadCATab() {
  const caView = document.getElementById('ca-view');
  caView.innerHTML = '';

  try {
    const res = await fetch('/ca/list');
    const files = await res.json();

    // 1. Root CA Section (The important one for the user)
    const rootSection = document.createElement('div');
    rootSection.innerHTML = `
            <div style="background:#2d2d2d; padding:15px; margin:10px; border-radius:4px; border-left:4px solid #f1e05a;">
                <h4>Primary CA Certificate</h4>
                <p>Download and install this certificate in your browser/OS to decrypt HTTPS traffic.</p>
                <button onclick="window.location='/ca/download?name=HITM-Proxy.pem'" class="btn">Download HITM-Proxy.pem (Public)</button>
            </div>
        `;
    caView.appendChild(rootSection);

    // 2. Generated Certificates Section
    const listSection = document.createElement('div');
    listSection.style.padding = "10px";
    listSection.innerHTML = `<h4>Generated Site Certificates</h4>`;

    const table = document.createElement('table');
    table.style.width = "100%";
    table.innerHTML = `<tr><th align="left">Filename</th><th align="right">Action</th></tr>`;

    files.forEach(file => {
      // Filter out keys if you only want them to download public certs
      // or leave them if you want full access.
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

  // Format: #tabName/flowId (e.g., #inspect/15)
  window.location.hash = flowId ? `${tab}/${flowId}` : `${tab}`;

  console.log("updated url")
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

  // 3. Tab-Specific Logic
  if (tab === 'repeat') {
    const textarea = document.getElementById('repeat-request');

    if (!currentFlowId) {
      textarea.placeholder = 'Select a flow from the sidebar to populate the repeat...';
      return;
    }

    const flow = flows.get(currentFlowId);
    if (flow && flow.request) {
      // Use .value for textareas, not .innerHTML
      textarea.value = atob(flow.request);
    }
  }

  if (tab === 'intercept') {
    const textarea = document.getElementById('intercept-request');

    if (!currentFlowId) {
      textarea.placeholder = 'Enable interception and send a request to the proxy';
      return;
    }

    const flow = flows.get(currentFlowId);
    if (flow && flow.request) {
      // Use .value for textareas, not .innerHTML
      textarea.value = atob(flow.request);
    }
  }

  if (tab === "sitemap") buildSitemap();
  if (tab === 'ca') loadCATab();
}

function addElement(tag, text, parent, className = "", id = "") {
  const el = document.createElement(tag);
  if (className) el.className = className;
  if (id) el.id = id;
  if (text) el.textContent = text; // Use textContent for safety
  if (parent) parent.appendChild(el);
  return el;
}

function renderDetails(flow) {
  const details = document.getElementById('details');

  const safeAtob = (str) => {
    try {
      return str ? atob(str) : null;
    } catch (e) {
      return "Error decoding data: " + e.message;
    }
  };

  const req = safeAtob(flow.request);
  const reqBody = safeAtob(flow.requestBody);
  const res = safeAtob(flow.responseBody);
  const resBody = safeAtob(flow.response);

  addElement("div", `Request ${flow.requestTime}`, details, "h3", "inspect-request")
  addElement("pre", req, details)
  addElement("pre", reqBody, details)
  addElement("div", `Response ${flow.responseTime}`, details, "h3", "inspect-response")
  if (res) {
    addElement("pre", resBody, details)
    addElement("pre", res, details)
  } else {
    addElement("p", "Waiting for response...", details)
  }
}

async function inspectFlow(id) {
  const details = document.getElementById('details');

  document.querySelectorAll('.flow-item').forEach(el => el.classList.remove('active-flow'));
  document.getElementById("flow-item-" + id).classList.add('active-flow');
  currentFlowId = id;

  try {
    details.innerHTML = "Loading...";
    const raw = await fetch('/flow?id=' + id);
    const flow = await raw.json();
    details.innerHTML = "";

    renderDetails(flow)

  } catch (err) {
    details.innerHTML = "Error loading flow: " + err;
  }

  updateUrlState();
}

// Helper to push an existing request into the Repeater tab
function copyToRepeater(base64Req) {
  document.getElementById('repeat-request').value = atob(base64Req);
  showTab('repeat'); // Switch tabs automatically
}

async function sendRepeat() {
  document.getElementById('repeat-response').innerText = "Loading...";
  const raw = document.getElementById('repeat-request').value;
  const res = await fetch('/repeat', { method: 'POST', body: raw });
  const resp = await res.text();
  document.getElementById('repeat-response').innerText = resp;
}

function loadStateFromUrl() {
  console.log("loading state from url")
  const hash = window.location.hash.substring(1); // Remove the '#'
  if (!hash) return;

  const [tab, flowId] = hash.split('/');

  console.log("tab: ", tab)
  console.log("flowid: ", flowId)

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
  document.getElementById("header").style = "background: #ff0000; color: white;";
  const res = await fetch('/intercept?enable=true', { method: 'POST', body: '' });
}

async function disableIntercept() {
  document.getElementById("enable-intercept").style.display = "";
  document.getElementById("disable-intercept").style.display = "none";
  document.getElementById("header").style = "background: #dddddd;";
  const res = await fetch('/intercept?enable=false', { method: 'POST', body: '' });
}

async function forwardRequest() {
  // Make sure interceptedFlowId was set when the flow arrived via SSE
  if (!interceptedFlowId) {
    console.error("No flow ID to forward");
    return;
  }

  const raw = document.getElementById('intercept-request').value;
  // Note the template literal for the ID
  const res = await fetch(`/forward?id=${interceptedFlowId}`, {
    method: 'POST',
    body: raw
  });

  if (res.ok) {
    interceptedFlowId = null; // Clear it for the next one
    document.getElementById('intercept-request').value = "";
  }
}

async function dropRequest() {
  if (!interceptedFlowId) return;

  await fetch(`/drop?id=${interceptedFlowId}`, { method: 'POST' });
  document.querySelector('#intercept-view textarea').value = "";
  interceptedFlowId = null;
}
