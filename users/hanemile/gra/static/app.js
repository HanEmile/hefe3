let selectedNode = null;
let nodeCounter = 0; // 2 nodes are set by default
let jobCounter = 0;
let nodes = [];
let links = [];
const existingNodeIds = new Set();
const existingLinkKeys = new Set();

let socket;
let activeJobOutput = null; // Track which job we are watching

function connectWS() {
  socket = new WebSocket(`ws://${window.location.host}/ws`);

  console.log("Connected to websocket server")

  socket.onmessage = (event) => {
    const data = JSON.parse(event.data);

    if (data.type === "time") {
      document.getElementById('time').textContent = data.data;
      console.log(data.data)
    }

    if (data.type === 'fuzz_update') {
      updateJobUI(data.job_id, data.progress);

      // Append to terminal if this is the job we are watching
      const terminal = document.getElementById('terminal');
      if (activeJobOutput === data.job_id) {
        const logEntry = document.createElement('div');
        logEntry.className = 'log-entry';
        logEntry.textContent = data.log;
        terminal.appendChild(logEntry);
        terminal.scrollTop = terminal.scrollHeight;
      }
    }
  };
}

// Real-time thread adjustment via WebSocket
function adjustThreadsWS(jobId) {
  const threads = prompt('Enter new thread count:');
  if (!threads) return;

  const action = {
    action: 'set_threads',
    job_id: jobId,
    value: parseInt(threads)
  };
  socket.send(JSON.stringify(action));
}

function updateJobUI(jobId, progress) {
  // Update progress bar and percentage without full re-render
  const jobCard = document.querySelector(`[data-job-id="${jobId}"]`);
  if (jobCard) {
    const bar = jobCard.querySelector('.progress-fill');
    const text = jobCard.querySelector('.progress-text');
    if (bar) bar.style.width = progress + '%';
    if (text) text.innerText = progress + '%';
  }
}

// Call this when the page loads
connectWS();

const elem = document.getElementById('graph');
const graph = new ForceGraph()(elem)
  .backgroundColor('#ffffff00')
  .nodeColor(node => {
    const colors = {
      domain: 'blue',
      ip: '#ff6b6b',
      endpoint: '#51cf66',
      subdomain: '#ffd43b',
      port: '#ff8787',
      service: '#a78bfa',
      path: 'green',
    };
    return colors[node.type] || '#e0e0e0';
  })
  .nodeLabel(node => node.name + ' (' + node.type + ')')
  .nodeRelSize(6)

  // .linkAutoColorBy(d => nodes[d.source].type)
  .linkAutoColorBy(d => {
    const sourceNode = typeof d.source === 'object' ? d.source : nodes[d.source];
    return sourceNode ? sourceNode.type : 'default';
  })

  .linkDirectionalParticles(2)
  .linkDirectionalArrowLength(4)

  .onNodeClick(node => {
    selectedNode = node;
    openSidePanel(node);
  })

  .nodeCanvasObject((node, ctx) => nodePaint(node, getColor(node.id), ctx))
  .nodeCanvasObject((node, ctx, globalScale) => {
    const label = node.name;
    const fontSize = 12 / globalScale;
    ctx.font = `${fontSize}px Sans-Serif`;
    const textWidth = ctx.measureText(label).width;
    const bckgDimensions = [textWidth, fontSize].map(n => n + fontSize * 0.2); // some padding

    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillStyle = node.color;
    ctx.fillText(label, node.x, node.y);

    node.__bckgDimensions = bckgDimensions; // to re-use in nodePointerAreaPaint
  })
  .nodePointerAreaPaint((node, color, ctx) => {
    ctx.fillStyle = color;
    const bckgDimensions = node.__bckgDimensions;
    bckgDimensions && ctx.fillRect(node.x - bckgDimensions[0] / 2, node.y - bckgDimensions[1] / 2, ...bckgDimensions);
  });

function switchTab(tabName) {
  document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
  document.querySelectorAll('.tab-content').forEach(t => t.classList.remove('active'));

  event.target.classList.add('active');
  document.getElementById(tabName + 'Tab').classList.add('active');

  if (tabName === 'jobs' || tabName === 'queue') {
    loadJobs();
  } else if (tabName === 'config') {
    loadConfig();
  }
}

async function loadGraph() {
  try {
    const res = await fetch('/api/graph');
    const rawdata = await res.json();

    let hasUpdates = false;

    // 2. Process Nodes
    // Assuming rawdata.nodes is a map/object: { "1": {id: 1, ...} }
    Object.values(rawdata.nodes).forEach(newNode => {
      const id = parseInt(newNode.id);
      if (!existingNodeIds.has(id)) {
        nodes.push(newNode);
        existingNodeIds.add(id);
        hasUpdates = true;
      }
    });

    // 3. Process Links
    // Assuming rawdata.links is: { "1": [2, 3] }
    Object.entries(rawdata.links).forEach(([sourceId, targets]) => {
      const s = parseInt(sourceId);
      targets.forEach(targetId => {
        const t = parseInt(targetId);
        const linkKey = `${s}-${t}`;

        if (!existingLinkKeys.has(linkKey)) {
          links.push({ source: s, target: t });
          existingLinkKeys.add(linkKey);
          hasUpdates = true;
        }
      });
    });

    // 4. Only notify the graph if new data was actually added
    if (hasUpdates) {
      nodeCounter = nodes.length + 1;

      // Pass the original references back.
      // force-graph will see the new entries and animate them in
      // without disturbing the existing nodes' positions.
      graph.graphData({ nodes, links })
    }
  } catch (err) {
    console.error("Sync error:", err);
  }
}

setInterval(() => {
  loadGraph()
}, 1000);

async function addNode() {
  const input = document.getElementById('nodeInput');
  const name = input.value.trim();
  if (!name) return;

  const node = {
    id: nodeCounter++,
    name: name,
    type: 'domain'
  };

  const res = await fetch('/api/nodes', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(node)
  });

  if (res.ok) {
    input.value = '';
    loadGraph();
  }
}

async function addManualNode() {
  const input = document.getElementById('nodeManualInput');
  const name = input.value.trim();
  if (!name) return;

  console.log("linking ", selectedNode, " to ", name)

  const node = {
    id: nodeCounter++,
    name: name,
    type: document.getElementById('nodeType').value,
  };

  const resNode = await fetch('/api/nodes', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(node)
  });
  if (resNode.ok) {
    input.value = '';
    loadGraph();
  }

  const link = {
    source: selectedNode.id,
    target: node.id,
  };

  const resLink = await fetch('/api/links', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(link)
  });
  if (resLink.ok) {
    input.value = '';
    loadGraph();
  }
}

function openSidePanel(node) {
  document.getElementById('nodeName').value = node.name;
  document.getElementById('nodeBase').value = node.base;
  document.getElementById('nodeType').value = node.type;
  document.getElementById('sidePanel').classList.add('open');
}

function openConfigPanel(type) {
  document.getElementById('configType').innerHTML = type + " config";

  const panel = document.getElementById('configPanel');
  panel.classList.add('open');

  if (type === "spider") {
    const maxDepthField = document.createElement("div");
    maxDepthField.classList.add("field");

    const label = document.createElement("label");
    label.innerHTML = "Max Depth 10";

    const input = document.createElement("input");
    input.type = "range";
    input.min = 1;
    input.max = 20;
    input.default = 10;
    input.oninput = function () {
      label.innerHTML = "Max Depth: " + this.value;
    };

    const submitField = document.createElement("div");
    submitField.classList.add("actions");

    const submit = document.createElement("button")
    submit.innerText = "Spider Now";
    submit.classList.add("action-btn")
    submit.onclick = function () {
      fetch('/spider', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ node: selectedNode, maxDepth: parseInt(input.value - 1, 10) })
      });
    };
    submitField.appendChild(submit);

    maxDepthField.appendChild(label);
    maxDepthField.appendChild(input);
    maxDepthField.appendChild(submitField);

    panel.querySelector('.panel-content').appendChild(maxDepthField);
  }
}

function closePanel(name) {
  document.getElementById(name).classList.remove('open');
}

async function updateNode() {
  if (!selectedNode) return;

  selectedNode.name = document.getElementById('nodeName').value;
  selectedNode.type = document.getElementById('nodeType').value;

  await fetch('/api/nodes', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(selectedNode)
  });

  loadGraph();
}

async function runJob(type) {
  if (!selectedNode) return;

  const jobNames = {
    fuzz: 'Fuzzing',
    spider: 'Web Spider',
    portscan: 'Port Scan',
    subdomain: 'Subdomain Enumeration'
  };

  const job = {
    id: 'job-' + jobCounter++,
    node_id: selectedNode.id,
    node_name: selectedNode.name,
    type: type,
    status: 'queued',
    progress: 0,
    threads: 10
  };

  console.log(job);

  await fetch('/api/jobs', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(job)
  });

  openConfigPanel(type);

  console.log('Started ' + jobNames[type] + ' on ' + selectedNode.name);
}

async function loadJobs() {
  const res = await fetch('/api/jobs');
  const jobs = await res.json();

  const activeJobs = jobs.filter(j => j.status === 'running' || j.status === 'paused');
  const queuedJobs = jobs.filter(j => j.status === 'queued');

  renderJobs(activeJobs, 'jobsList');
  renderJobs(queuedJobs, 'queueList');
}

function renderJobs(jobs, containerId) {
  const container = document.getElementById(containerId);

  if (jobs.length === 0) {
    container.innerHTML = '<p style="color: #9ca3af; text-align: center; padding: 40px;">No jobs to display</p>';
    return;
  }

  container.innerHTML = jobs.map(job => `
        <div class="job-card">
            <div class="job-header">
                <div class="job-title">${getJobTypeName(job.type)} - ${job.node_name}</div>
                <div class="job-status status-${job.status}">${job.status.toUpperCase()}</div>
            </div>
            <div class="job-info">
                <div class="job-info-item">Threads: <strong>${job.threads}</strong></div>
                <div class="job-info-item">Machine: <strong>${job.machine}</strong></div>
                <div class="job-info-item">Started: <strong>${formatTime(job.start_time)}</strong></div>
                <div class="job-info-item">Progress: <strong>${job.progress}%</strong></div>
            </div>
            <div class="progress-bar">
                <div class="progress-fill" style="width: ${job.progress}%"></div>
            </div>
            <div class="job-controls">
                <button onclick="adjustThreads('${job.id}')">Adjust Threads</button>
                ${job.status === 'running' ?
      `<button class="btn-warning" onclick="pauseJob('${job.id}')">Pause</button>` :
      `<button onclick="resumeJob('${job.id}')">Resume</button>`
    }
                <button class="btn-danger" onclick="cancelJob('${job.id}')">Cancel</button>
            </div>
        </div>
    `).join('');
}

function getJobTypeName(type) {
  const names = {
    fuzz: 'Fuzzing',
    spider: 'Web Spider',
    portscan: 'Port Scan',
    subdomain: 'Subdomain Enumeration'
  };
  return names[type] || type;
}

function formatTime(timestamp) {
  if (!timestamp) return 'N/A';
  const date = new Date(timestamp);
  return date.toLocaleTimeString();
}

async function adjustThreads(jobId) {
  const threads = prompt('Enter number of threads (1-100):');
  if (!threads) return;

  await fetch('/api/jobs/' + jobId, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ threads: parseInt(threads) })
  });

  loadJobs();
}

async function pauseJob(jobId) {
  await fetch('/api/jobs/' + jobId, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ status: 'paused' })
  });
  loadJobs();
}

async function resumeJob(jobId) {
  await fetch('/api/jobs/' + jobId, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ status: 'running' })
  });
  loadJobs();
}

async function cancelJob(jobId) {
  if (!confirm('Are you sure you want to cancel this job?')) return;

  await fetch('/api/jobs/' + jobId, {
    method: 'DELETE'
  });
  loadJobs();
}

async function loadConfig() {
  const res = await fetch('/api/config');
  const config = await res.json();

  document.getElementById('maxConcurrent').value = config.max_concurrent;

  const workerList = document.getElementById('workerList');
  workerList.innerHTML = config.workers.map(w => `
        <div class="worker-card">
            <div class="worker-info">
                <div class="worker-name">${w.name}</div>
                <div class="worker-details">${w.ip} • Capacity: ${w.capacity}</div>
            </div>
            <div class="worker-status status-${w.status}">${w.status.toUpperCase()}</div>
        </div>
    `).join('');
}

async function saveConfig() {
  const config = {
    max_concurrent: parseInt(document.getElementById('maxConcurrent').value),
    workers: [] // Will be loaded from server
  };

  const res = await fetch('/api/config');
  const currentConfig = await res.json();
  config.workers = currentConfig.workers;

  await fetch('/api/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config)
  });

  alert('Configuration saved successfully!');
}

async function addWorker() {
  const name = document.getElementById('workerName').value.trim();
  const ip = document.getElementById('workerIP').value.trim();
  const capacity = parseInt(document.getElementById('workerCapacity').value);

  if (!name || !ip || !capacity) {
    alert('Please fill in all fields');
    return;
  }

  const worker = {
    id: 'worker-' + Date.now(),
    name: name,
    ip: ip,
    status: 'inactive',
    capacity: capacity
  };

  await fetch('/api/workers', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(worker)
  });

  document.getElementById('workerName').value = '';
  document.getElementById('workerIP').value = '';
  document.getElementById('workerCapacity').value = '10';

  loadConfig();
}

function exportNode() {
  console.log('Exporting node data:', selectedNode);
  const data = JSON.stringify(selectedNode, null, 2);
  alert('Node data:\n' + data);
}

document.getElementById('nodeInput').addEventListener('keypress', (e) => {
  if (e.key === 'Enter') addNode();
});

// Auto-refresh jobs every 3 seconds when on jobs/queue tabs
setInterval(() => {
  const activeTab = document.querySelector('.tab.active').textContent.toLowerCase();
  if (activeTab === 'jobs' || activeTab === 'queue') {
    loadJobs();
  }
}, 3000);

loadGraph();
