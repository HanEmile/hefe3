// Dynamic force-directed graph for the status board.
//
// Topology is sourced entirely from /etc/status-board-graph.json (written
// by tools/status-board/default.nix from per-VM nix evaluation), so adding
// a VM to ops.ipam — or a relationship to the relationships attrset —
// shows up automatically after a deploy.
//
// Layers visualised in one graph:
//   external -> dns -> public IP -> iface (eno1/tailscale0) -> naraj (or host)
//   -> bridge -> VM -> open ports
// plus service-to-service edges (OIDC, NFS, restic) overlaid on the VM
// layer with their own colour + label.
//
// The browser-side does force simulation, drag-pin, scroll-zoom (TODO),
// and click-to-focus dimming.
package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"strconv"
)

type vmEntry struct {
	Name           string         `json:"name"`
	IP             string         `json:"ip"`
	Bridge         string         `json:"bridge"`
	Ports          []int          `json:"ports"`
	BackupsEnabled bool           `json:"backupsEnabled"`
	BackupPaths    []string       `json:"backupPaths"`
	BackupTargets  []targetEntry  `json:"backupTargets"`
}

// targetEntry: per-VM backup target. List-shaped to allow adding a second
// target (e.g. cold archive) without code changes.
type targetEntry struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Repo  string `json:"repo"`
}
type relEntry struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
	Via  string `json:"via"`
}
type ingressEntry struct {
	Host    string `json:"host"`
	VM      string `json:"vm"`
	Service string `json:"service"`
	Port    int    `json:"port"`
	TLS     bool   `json:"tls"`
}
type graphPayload struct {
	VMs           []vmEntry      `json:"vms"`
	Relationships []relEntry     `json:"relationships"`
	Ingress       []ingressEntry `json:"ingress"`
	ExternalIp    string         `json:"externalIp"`
	Zpools        []string       `json:"zpools"`
}

func loadGraphData() (*graphPayload, error) {
	path := os.Getenv("STATUS_BOARD_GRAPH")
	if path == "" {
		path = "/etc/status-board-graph.json"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	g := &graphPayload{}
	if err := json.Unmarshal(b, g); err != nil {
		return nil, err
	}
	return g, nil
}

func buildForceGraph(vms []VMStat) template.HTML {
	g, err := loadGraphData()
	if err != nil {
		return template.HTML(fmt.Sprintf(`<div class="sb-banner bad">graph data unavailable: %s</div>`,
			template.HTMLEscapeString(err.Error())))
	}

	vmByName := map[string]VMStat{}
	for _, v := range vms {
		vmByName[v.Name] = v
	}
	vmCls := func(name string) string {
		v, ok := vmByName[name]
		if !ok {
			return "warn"
		}
		if len(v.Probes) > 0 {
			allUp := true
			for _, p := range v.Probes {
				if !p.Up {
					allUp = false
					break
				}
			}
			if allUp {
				return "ok"
			}
			return "bad"
		}
		if v.Reachable {
			return "warn"
		}
		return "bad"
	}

	type fgNode struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		Group string `json:"group"`
		Sub   string `json:"sub,omitempty"`
		Class string `json:"class,omitempty"`
	}
	type fgEdge struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Style  string `json:"style,omitempty"` // tls | bypass | oidc | nfs | restic | forward-auth
		Label  string `json:"label,omitempty"`
	}

	nodes := []fgNode{}
	edges := []fgEdge{}
	seen := map[string]bool{}
	addNode := func(n fgNode) {
		if seen[n.ID] {
			return
		}
		seen[n.ID] = true
		nodes = append(nodes, n)
	}
	addEdge := func(s, t, style, label string) {
		edges = append(edges, fgEdge{Source: s, Target: t, Style: style, Label: label})
	}

	// External anchor + medano host.
	addNode(fgNode{ID: "ext", Label: "internet", Group: "external"})
	addNode(fgNode{ID: "host:medano", Label: "medano", Group: "vm", Class: "ok", Sub: "host"})

	// VM nodes + port leaves.
	for _, v := range g.VMs {
		id := "vm:" + v.Name
		addNode(fgNode{ID: id, Label: v.Name, Group: "vm", Sub: v.IP, Class: vmCls(v.Name)})
		brID := "br:" + v.Bridge
		addNode(fgNode{ID: brID, Label: v.Bridge, Group: "bridge"})
		addEdge(brID, id, "", "")
		for _, p := range v.Ports {
			pid := fmt.Sprintf("port:%s:%d", v.Name, p)
			addNode(fgNode{ID: pid, Label: ":" + strconv.Itoa(p), Group: "port"})
			addEdge(id, pid, "", "")
		}
	}

	// External anchor -> medano (covers ssh + DNAT to naraj at the IP layer).
	pubIPID := "ip:" + g.ExternalIp
	addNode(fgNode{ID: pubIPID, Label: g.ExternalIp, Group: "ip"})
	addNode(fgNode{ID: "if:eno1", Label: "eno1", Group: "iface"})
	addEdge("ext", pubIPID, "", "")
	addEdge(pubIPID, "if:eno1", "", "")
	addEdge("if:eno1", "host:medano", "", "")

	// Ingress: DNS -> pubIP -> eno1 -> naraj (or host for status) -> backend VM:port.
	addNode(fgNode{ID: "if:tailscale0", Label: "tailscale0", Group: "iface"})

	for _, ing := range g.Ingress {
		hostID := "host:" + ing.Host
		addNode(fgNode{ID: hostID, Label: ing.Host, Group: "dns"})
		style := "bypass"
		if ing.TLS {
			style = "tls"
		}

		if ing.TLS {
			addEdge(hostID, pubIPID, style, "")
			addEdge(pubIPID, "if:eno1", style, "")
			// 80/443 DNAT goes to naraj for everything except status (host).
			var hopID string
			if ing.VM == "medano" {
				hopID = "host:medano"
			} else if ing.VM == "naraj" {
				hopID = "vm:naraj"
			} else {
				hopID = "vm:naraj"
			}
			addEdge("if:eno1", hopID, style, "")
			if ing.VM != "naraj" && ing.VM != "medano" {
				targetVM := "vm:" + ing.VM
				addEdge(hopID, targetVM, style, "")
			}
			// Edge to backend port.
			portID := fmt.Sprintf("port:%s:%d", ing.VM, ing.Port)
			if ing.VM == "medano" {
				portID = fmt.Sprintf("port:medano:%d", ing.Port)
				addNode(fgNode{ID: portID, Label: ":" + strconv.Itoa(ing.Port), Group: "port"})
				addEdge("host:medano", portID, style, "")
			} else {
				// (port leaf was already added in the VM loop)
				addEdge("vm:"+ing.VM, portID, style, ing.Service)
			}
		} else {
			// tailscale path: dns -> tailscale0 -> backend VM:port
			addEdge(hostID, "if:tailscale0", style, "")
			addEdge("if:tailscale0", "vm:"+ing.VM, style, "")
			portID := fmt.Sprintf("port:%s:%d", ing.VM, ing.Port)
			addEdge("vm:"+ing.VM, portID, style, ing.Service)
		}
	}

	// Storagebox external endpoint.
	addNode(fgNode{ID: "ext:storagebox", Label: "storagebox", Group: "external", Sub: "u331921@hetzner"})

	// Service-to-service relationships.
	for _, r := range g.Relationships {
		src := "vm:" + r.From
		if r.From == "medano" {
			src = "host:medano"
		}
		dst := "vm:" + r.To
		if r.To == "medano" {
			dst = "host:medano"
		}
		if r.To == "storagebox" {
			dst = "ext:storagebox"
		}
		addEdge(src, dst, r.Kind, r.Kind)
	}

	payload, _ := json.Marshal(struct {
		Nodes []fgNode `json:"nodes"`
		Edges []fgEdge `json:"edges"`
	}{nodes, edges})

	html := `<div id="force-graph-wrap" style="background:#181818;border:1px solid var(--border);border-radius:4px;padding:8px;">
  <div style="color:#888;font-size:11px;margin-bottom:6px;display:flex;gap:1.5em;flex-wrap:wrap;align-items:center;">
    <span>drag to pin · dblclick to release · <b>click a node to focus</b> · click empty space to clear</span>
    <span style="margin-left:auto;display:inline-flex;gap:.8em;align-items:center;">
      <span style="color:#bb88dd">dns</span>
      <span style="color:#88aadd">ip</span>
      <span style="color:#88cc99">iface</span>
      <span style="color:#ffbb88">proxy</span>
      <span style="color:#aaaabb">bridge</span>
      <span style="color:#ccddaa">vm</span>
      <span style="color:#888">port</span>
      <span style="color:#e2b04a">external</span>
    </span>
  </div>
  <div style="color:#666;font-size:10px;margin-bottom:6px;">
    edges:
    <span style="color:#6aa9ff">— TLS via naraj</span>
    <span style="color:#b07cff">- - bypass (tailscale)</span>
    <span style="color:#ff8aa0">— oidc</span>
    <span style="color:#9ad17a">— nfs</span>
    <span style="color:#d3a44a">— restic</span>
    <span style="color:#aaa">— forward-auth</span>
  </div>
  <div class="fg-controls" id="fg-controls">
    <span style="color:var(--dim);">show:</span>
    <label class="node-toggle"><input type="checkbox" data-group="external" checked><span class="sw" style="background:#e2b04a"></span>external</label>
    <label class="node-toggle"><input type="checkbox" data-group="dns" checked><span class="sw" style="background:#bb88dd"></span>dns</label>
    <label class="node-toggle"><input type="checkbox" data-group="ip" checked><span class="sw" style="background:#88aadd"></span>ip</label>
    <label class="node-toggle"><input type="checkbox" data-group="iface" checked><span class="sw" style="background:#88cc99"></span>iface</label>
    <label class="node-toggle"><input type="checkbox" data-group="proxy" checked><span class="sw" style="background:#ffbb88"></span>proxy</label>
    <label class="node-toggle"><input type="checkbox" data-group="bridge" checked><span class="sw" style="background:#aaaabb"></span>bridge</label>
    <label class="node-toggle"><input type="checkbox" data-group="vm" checked><span class="sw" style="background:#ccddaa"></span>vm</label>
    <label class="node-toggle"><input type="checkbox" data-group="port" checked><span class="sw" style="background:#888"></span>port</label>
    <span class="sep"></span>
    <label class="explore" id="explore-toggle"><input type="checkbox" id="exploreMode"> explore mode <span style="color:var(--dimmer);font-size:10px;">(click to expand · dblclick to collapse)</span></label>
  </div>
  <svg id="force-graph" width="100%" height="720" viewBox="0 0 1400 720" preserveAspectRatio="xMidYMid meet" style="cursor:grab;display:block;"></svg>
</div>
<script id="force-graph-data" type="application/json">` + string(payload) + `</script>
<script>
(function(){
  const data = JSON.parse(document.getElementById("force-graph-data").textContent);
  const svg = document.getElementById("force-graph");
  const W = 1400, H = 720;
  const GROUP_COLOR = {
    external:"#e2b04a",
    dns:     "#bb88dd",
    ip:      "#88aadd",
    iface:   "#88cc99",
    proxy:   "#ffbb88",
    bridge:  "#aaaabb",
    vm:      "#ccddaa",
    port:    "#888888",
  };
  const STATUS_STROKE = { ok:"#48d97f", warn:"#e2b04a", bad:"#e25c5c" };
  const RADIUS = { external:13, dns:10, ip:10, iface:10, proxy:14, bridge:12, vm:14, port:6 };

  const EDGE_STYLE = {
    "":            { stroke:"#444", dash:"" },
    "tls":         { stroke:"#6aa9ff", dash:"" },
    "bypass":      { stroke:"#b07cff", dash:"4,3" },
    "oidc":        { stroke:"#ff8aa0", dash:"" },
    "nfs":         { stroke:"#9ad17a", dash:"" },
    "restic":      { stroke:"#d3a44a", dash:"2,4" },
    "forward-auth":{ stroke:"#aaaaaa", dash:"3,3" },
  };

  // Seed columns roughly so the simulation has a decent starting layout.
  const COL_X = {
    external: 80, dns: 220, ip: 360, iface: 480,
    proxy: 600, bridge: 760, vm: 940, port: 1280,
  };

  const nodes = data.nodes.map(n => Object.assign({}, n, {
    x: (COL_X[n.group] || W/2) + (Math.random()-0.5)*40,
    y: H/2 + (Math.random()-0.5)*H*0.5,
    vx: 0, vy: 0,
  }));
  const idx = new Map(nodes.map((n,i) => [n.id, i]));

  // Resolve edges to indices and build adjacency for focus mode.
  const edges = [];
  const adj = nodes.map(() => new Set());
  data.edges.forEach(e => {
    const s = idx.get(e.source), t = idx.get(e.target);
    if (s === undefined || t === undefined) return;
    edges.push({ source:s, target:t, style:e.style||"", label:e.label||"" });
    adj[s].add(t); adj[t].add(s);
  });

  let alpha = 1.0;
  const targetDist = 90;
  const repulse = 1500;
  const xPull = 0.05;
  const yCenter = 0.006;
  const damping = 0.84;

  function step() {
    if (alpha < 0.003) return;
    for (let i = 0; i < nodes.length; i++) {
      for (let j = i+1; j < nodes.length; j++) {
        const a = nodes[i], b = nodes[j];
        let dx = b.x - a.x, dy = b.y - a.y;
        let d2 = dx*dx + dy*dy;
        if (d2 < 1) d2 = 1;
        const d = Math.sqrt(d2);
        const f = repulse / d2;
        const fx = (dx/d) * f, fy = (dy/d) * f;
        if (!a.pinned) { a.vx -= fx; a.vy -= fy; }
        if (!b.pinned) { b.vx += fx; b.vy += fy; }
      }
    }
    for (const e of edges) {
      const a = nodes[e.source], b = nodes[e.target];
      const dx = b.x - a.x, dy = b.y - a.y;
      const d = Math.sqrt(dx*dx + dy*dy) || 0.001;
      const delta = d - targetDist;
      const fx = (dx/d) * delta * 0.05;
      const fy = (dy/d) * delta * 0.05;
      if (!a.pinned) { a.vx += fx; a.vy += fy; }
      if (!b.pinned) { b.vx -= fx; b.vy -= fy; }
    }
    for (const n of nodes) {
      if (n.pinned) continue;
      const tx = COL_X[n.group];
      if (tx !== undefined) n.vx += (tx - n.x) * xPull;
      n.vy += (H/2 - n.y) * yCenter;
    }
    for (const n of nodes) {
      if (n.pinned) continue;
      n.vx *= damping; n.vy *= damping;
      n.x += n.vx * alpha;
      n.y += n.vy * alpha;
      n.x = Math.max(20, Math.min(W-20, n.x));
      n.y = Math.max(20, Math.min(H-20, n.y));
    }
    alpha *= 0.988;
  }

  const ns = "http://www.w3.org/2000/svg";
  const gEdges = document.createElementNS(ns, "g");
  const gNodes = document.createElementNS(ns, "g");
  svg.appendChild(gEdges);
  svg.appendChild(gNodes);

  const edgeEls = edges.map(e => {
    const el = document.createElementNS(ns, "line");
    const st = EDGE_STYLE[e.style] || EDGE_STYLE[""];
    el.setAttribute("stroke", st.stroke);
    el.setAttribute("stroke-width", e.style && e.style !== "" ? "1.5" : "1");
    if (st.dash) el.setAttribute("stroke-dasharray", st.dash);
    el.setAttribute("opacity", "0.85");
    gEdges.appendChild(el);
    return el;
  });

  const nodeEls = nodes.map((n, i) => {
    const g = document.createElementNS(ns, "g");
    g.setAttribute("class", "fg-node");
    g.style.cursor = "pointer";
    const r = RADIUS[n.group] || 8;
    const c = document.createElementNS(ns, "circle");
    c.setAttribute("r", r);
    c.setAttribute("fill", GROUP_COLOR[n.group] || "#888");
    if (n.class && STATUS_STROKE[n.class]) {
      c.setAttribute("stroke", STATUS_STROKE[n.class]);
      c.setAttribute("stroke-width", "2.5");
    } else {
      c.setAttribute("stroke", "#222");
      c.setAttribute("stroke-width", "1");
    }
    g.appendChild(c);

    const t = document.createElementNS(ns, "text");
    t.setAttribute("font-family", "monospace");
    t.setAttribute("font-size", n.group === "vm" || n.group === "proxy" || n.group === "external" ? "11" : "10");
    t.setAttribute("fill", "#ddd");
    t.setAttribute("text-anchor", "middle");
    t.setAttribute("y", -(r+4));
    t.style.pointerEvents = "none";
    t.textContent = n.label;
    g.appendChild(t);

    if (n.sub) {
      const s = document.createElementNS(ns, "text");
      s.setAttribute("font-family", "monospace");
      s.setAttribute("font-size", "9");
      s.setAttribute("fill", "#888");
      s.setAttribute("text-anchor", "middle");
      s.setAttribute("y", r+11);
      s.style.pointerEvents = "none";
      s.textContent = n.sub;
      g.appendChild(s);
    }
    gNodes.appendChild(g);
    return g;
  });

  function render() {
    for (let i = 0; i < edges.length; i++) {
      const e = edges[i], a = nodes[e.source], b = nodes[e.target];
      edgeEls[i].setAttribute("x1", a.x);
      edgeEls[i].setAttribute("y1", a.y);
      edgeEls[i].setAttribute("x2", b.x);
      edgeEls[i].setAttribute("y2", b.y);
    }
    for (let i = 0; i < nodes.length; i++) {
      nodeEls[i].setAttribute("transform", "translate(" + nodes[i].x + "," + nodes[i].y + ")");
    }
  }

  function tick(){ step(); render(); requestAnimationFrame(tick); }
  tick();

  // --- Drag + click-focus ---
  let dragIdx = -1, dragMoved = false, dragStart = null;
  let focusedIdx = -1;

  function ptFromEvent(ev) {
    const rect = svg.getBoundingClientRect();
    const sx = W / rect.width, sy = H / rect.height;
    const cx = (ev.touches ? ev.touches[0].clientX : ev.clientX) - rect.left;
    const cy = (ev.touches ? ev.touches[0].clientY : ev.clientY) - rect.top;
    return [cx * sx, cy * sy];
  }
  function down(ev, i){
    ev.preventDefault(); ev.stopPropagation();
    dragIdx = i; dragMoved = false;
    nodes[i].pinned = true;
    dragStart = ptFromEvent(ev);
    alpha = Math.max(alpha, 0.3);
  }
  function move(ev){
    if (dragIdx < 0) return;
    const [x, y] = ptFromEvent(ev);
    if (dragStart) {
      const dx = x - dragStart[0], dy = y - dragStart[1];
      if (dx*dx + dy*dy > 16) dragMoved = true;
    }
    nodes[dragIdx].x = x; nodes[dragIdx].y = y;
    nodes[dragIdx].vx = 0; nodes[dragIdx].vy = 0;
    alpha = Math.max(alpha, 0.2);
  }
  function up(ev){
    if (dragIdx >= 0 && !dragMoved) {
      // click without drag => focus toggle
      focusedIdx = (focusedIdx === dragIdx) ? -1 : dragIdx;
      applyFocus();
    }
    dragIdx = -1; dragStart = null;
  }
  function applyFocus() {
    if (focusedIdx < 0) {
      // clear focus
      nodeEls.forEach(el => el.style.opacity = "1");
      edgeEls.forEach(el => el.style.opacity = "0.85");
      return;
    }
    const keep = new Set([focusedIdx]);
    // include direct neighbours
    adj[focusedIdx].forEach(n => keep.add(n));
    // also: if it's an ingress hostname (dns) include the whole path it touches
    // (already covered by adj because all chain hops are real edges).
    nodeEls.forEach((el, i) => {
      el.style.opacity = keep.has(i) ? "1" : "0.12";
    });
    edgeEls.forEach((el, i) => {
      const e = edges[i];
      el.style.opacity = (e.source === focusedIdx || e.target === focusedIdx) ? "1" : "0.05";
    });
  }
  nodeEls.forEach((el, i) => {
    el.addEventListener("mousedown", ev => down(ev, i));
    el.addEventListener("touchstart", ev => down(ev, i), {passive:false});
    el.addEventListener("dblclick", ev => {
      ev.stopPropagation();
      nodes[i].pinned = false;
      alpha = Math.max(alpha, 0.2);
    });
  });
  window.addEventListener("mousemove", move);
  window.addEventListener("touchmove", move, {passive:false});
  window.addEventListener("mouseup", up);
  window.addEventListener("touchend", up);
  // Click on empty SVG area clears focus.
  svg.addEventListener("click", ev => {
    if (ev.target === svg) {
      focusedIdx = -1;
      applyFocus();
    }
  });

  // ---- Node-group toggles + explore mode ----------------------------------
  // Hidden groups: user has unchecked them. They participate in layout (so
  // positions stay stable) but their DOM is display:none and edges to them
  // are hidden too.
  // Explore mode: when active, start with only 'external' visible; clicking
  // a node reveals its direct downstream neighbours; double-clicking collapses
  // anything that became visible *through* this node and is not still
  // reachable via another expanded chain. State persisted in localStorage.
  const LS_HIDDEN  = "status-board.graph.hidden";
  const LS_EXPLORE = "status-board.graph.exploreMode";
  const LS_OPENED  = "status-board.graph.exploreOpened";
  const hiddenGroups = new Set();
  try {
    const raw = localStorage.getItem(LS_HIDDEN);
    if (raw) JSON.parse(raw).forEach(g => hiddenGroups.add(g));
  } catch (e) {}
  let exploreMode = false;
  try { exploreMode = localStorage.getItem(LS_EXPLORE) === "1"; } catch (e) {}
  const opened = new Set(); // set of node ids that user has expanded in explore mode
  try {
    const raw = localStorage.getItem(LS_OPENED);
    if (raw) JSON.parse(raw).forEach(id => opened.add(id));
  } catch (e) {}

  // Build directed adjacency (downstream) from the original edge list — the
  // existing adj is undirected. We treat edges as going source -> target,
  // matching the natural left-to-right flow of the layered layout.
  const downstream = nodes.map(() => new Set());
  for (const e of edges) {
    downstream[e.source].add(e.target);
  }

  function persist() {
    try { localStorage.setItem(LS_HIDDEN, JSON.stringify([...hiddenGroups])); } catch (e) {}
    try { localStorage.setItem(LS_EXPLORE, exploreMode ? "1" : "0"); } catch (e) {}
    try { localStorage.setItem(LS_OPENED, JSON.stringify([...opened])); } catch (e) {}
  }

  function nodeVisible(i) {
    const n = nodes[i];
    if (hiddenGroups.has(n.group)) return false;
    if (!exploreMode) return true;
    if (n.group === "external") return true;
    return opened.has(n.id);
  }

  function applyVisibility() {
    for (let i = 0; i < nodes.length; i++) {
      const vis = nodeVisible(i);
      nodeEls[i].classList.toggle("fg-hidden", !vis);
    }
    for (let i = 0; i < edges.length; i++) {
      const e = edges[i];
      const vis = nodeVisible(e.source) && nodeVisible(e.target);
      edgeEls[i].classList.toggle("fg-hidden", !vis);
    }
    alpha = Math.max(alpha, 0.2);
  }

  // In explore mode: clicking a visible node reveals its downstream
  // neighbours (skipping hidden groups). Double-click on an opened node
  // collapses everything reachable *only* through it.
  function expandFrom(i) {
    const n = nodes[i];
    opened.add(n.id);
    for (const j of downstream[i]) {
      if (hiddenGroups.has(nodes[j].group)) continue;
      // Reveal the neighbour itself. The neighbour is "opened" only when the
      // user explicitly clicks it (so its grandchildren stay hidden) — but
      // we still need it visible. We achieve that by treating any node whose
      // parent is opened as visible too. Simpler: track an extra "revealed"
      // set. To keep state minimal, store the revealed nodes in opened.
      opened.add(nodes[j].id);
    }
  }
  function collapseFrom(i) {
    // Remove this node from opened, then recompute reachable closure.
    opened.delete(nodes[i].id);
    // Anything no longer reachable from any opened external should drop.
    const reachable = new Set();
    // Start frontier: opened externals + any explicitly opened node still in opened.
    const frontier = [];
    for (let k = 0; k < nodes.length; k++) {
      if (nodes[k].group === "external") {
        frontier.push(k);
        reachable.add(nodes[k].id);
      }
    }
    while (frontier.length) {
      const k = frontier.shift();
      if (!opened.has(nodes[k].id) && nodes[k].group !== "external") continue;
      for (const j of downstream[k]) {
        if (!opened.has(nodes[j].id)) continue;
        if (reachable.has(nodes[j].id)) continue;
        reachable.add(nodes[j].id);
        frontier.push(j);
      }
    }
    // Drop any opened ids that are not reachable from an external root.
    for (const id of [...opened]) {
      if (!reachable.has(id)) opened.delete(id);
    }
  }

  // Wire the toggle row.
  document.querySelectorAll("#fg-controls .node-toggle input").forEach(cb => {
    const g = cb.dataset.group;
    cb.checked = !hiddenGroups.has(g);
    cb.addEventListener("change", () => {
      if (cb.checked) hiddenGroups.delete(g);
      else hiddenGroups.add(g);
      persist();
      applyVisibility();
    });
  });
  const explCb = document.getElementById("exploreMode");
  const explLbl = document.getElementById("explore-toggle");
  function reflectExplore(){
    explCb.checked = exploreMode;
    explLbl.classList.toggle("active", exploreMode);
  }
  reflectExplore();
  explCb.addEventListener("change", () => {
    exploreMode = explCb.checked;
    if (!exploreMode) opened.clear();
    persist(); reflectExplore(); applyVisibility();
  });

  // Hook clicks: in explore mode, single-click expands; double-click collapses.
  // Single-click expansion is layered on top of the existing focus toggle —
  // we keep the focus-toggle behaviour for non-explore mode.
  const origUp = up;
  up = function(ev) {
    if (exploreMode && dragIdx >= 0 && !dragMoved) {
      const i = dragIdx;
      // expand on click
      expandFrom(i);
      persist();
      applyVisibility();
      dragIdx = -1; dragStart = null;
      return;
    }
    origUp(ev);
  };
  // Re-bind mouseup handlers to use new closure.
  window.removeEventListener("mouseup", up);
  window.addEventListener("mouseup", up);
  window.removeEventListener("touchend", up);
  window.addEventListener("touchend", up);

  // Double-click: in explore mode collapse; in normal mode existing behaviour
  // (unpin) remains via the per-node handler. Add a capture-phase handler
  // that wins in explore mode.
  nodeEls.forEach((el, i) => {
    el.addEventListener("dblclick", ev => {
      if (!exploreMode) return; // let the existing handler unpin
      ev.stopPropagation();
      ev.preventDefault();
      collapseFrom(i);
      persist();
      applyVisibility();
    }, true);
  });

  applyVisibility();
})();
</script>`
	return template.HTML(html)
}
