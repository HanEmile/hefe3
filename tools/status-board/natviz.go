// NAT topology visualization for the status board.
//
// Reads /etc/nat-flows.json (written by medano/networking.nix from the
// declarative natFlows attrset that also generates the iptables rules)
// and renders a force-directed graph: client -> eno1/pubIP ->
// PREROUTING DNAT -> bridge -> backend VM:port, plus hairpin edges from
// the VM subnet and SNAT-return arrows where applicable.
package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"strings"
)

type natFlow struct {
	Name       string `json:"name"`
	Desc       string `json:"desc"`
	Proto      string `json:"proto"`
	Dport      int    `json:"dport"`
	Dest       string `json:"dest"`
	Hairpin    bool   `json:"hairpin"`
	SnatReturn bool   `json:"snatReturn"`
}

type natConfig struct {
	PublicIp      string    `json:"publicIp"`
	VmSubnet      string    `json:"vmSubnet"`
	ExternalIface string    `json:"externalIface"`
	Bridge        string    `json:"bridge"`
	Flows         []natFlow `json:"flows"`
}

func loadNATConfig() (*natConfig, error) {
	path := os.Getenv("STATUS_BOARD_NAT_JSON")
	if path == "" {
		path = "/etc/nat-flows.json"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := &natConfig{}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, err
	}
	return c, nil
}

// buildNATSVG renders the NAT topology as a force-directed graph. Each
// flow contributes a chain client -> external IP -> PREROUTING -> bridge
// -> backend, with hairpin/SNAT edges overlaid in different colors.
func buildNATSVG() template.HTML {
	c, err := loadNATConfig()
	if err != nil {
		return template.HTML(fmt.Sprintf(
			`<div class="sb-banner bad">NAT config unavailable: %s</div>`,
			template.HTMLEscapeString(err.Error())))
	}

	type ngNode struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		Group string `json:"group"`
		Sub   string `json:"sub,omitempty"`
	}
	type ngEdge struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Style  string `json:"style,omitempty"`
		Label  string `json:"label,omitempty"`
	}
	nodes := []ngNode{}
	edges := []ngEdge{}
	seen := map[string]bool{}
	addNode := func(n ngNode) {
		if seen[n.ID] {
			return
		}
		seen[n.ID] = true
		nodes = append(nodes, n)
	}
	addEdge := func(s, t, style, label string) {
		edges = append(edges, ngEdge{Source: s, Target: t, Style: style, Label: label})
	}

	addNode(ngNode{ID: "client", Label: "client", Group: "external", Sub: "internet"})
	addNode(ngNode{ID: "iface", Label: c.ExternalIface, Group: "iface", Sub: c.PublicIp})
	addNode(ngNode{ID: "prerouting", Label: "PREROUTING", Group: "chain", Sub: "DNAT"})
	addNode(ngNode{ID: "bridge", Label: c.Bridge, Group: "bridge"})
	addNode(ngNode{ID: "subnet", Label: "VM subnet", Group: "subnet", Sub: c.VmSubnet})

	addEdge("client", "iface", "", "")
	addEdge("iface", "prerouting", "", "")
	addEdge("prerouting", "bridge", "", "")

	for _, f := range c.Flows {
		parts := strings.SplitN(f.Dest, ":", 2)
		destIp, destPort := parts[0], ""
		if len(parts) > 1 {
			destPort = parts[1]
		}
		flowID := "flow:" + f.Name
		destID := "dest:" + destIp
		addNode(ngNode{ID: flowID, Label: f.Name, Group: "flow", Sub: fmt.Sprintf(":%d/%s", f.Dport, f.Proto)})
		addNode(ngNode{ID: destID, Label: destIp, Group: "backend", Sub: ":" + destPort})
		addEdge("prerouting", flowID, "", "")
		addEdge(flowID, destID, "dnat", f.Dest)
		addEdge("bridge", destID, "", "")
		if f.Hairpin {
			addEdge("subnet", flowID, "hairpin", "hairpin")
		}
		if f.SnatReturn {
			addEdge(destID, "iface", "snat", "SNAT")
		}
	}

	payload, _ := json.Marshal(struct {
		Nodes []ngNode `json:"nodes"`
		Edges []ngEdge `json:"edges"`
	}{nodes, edges})

	html := `<div id="nat-graph-wrap" style="background:#181818;border:1px solid var(--border);border-radius:4px;padding:8px;">
  <div style="color:#888;font-size:11px;margin-bottom:6px;">
    iptables flows (declarative source: medano/networking.nix natFlows) ·
    <span style="color:#c8d">chain</span> ·
    <span style="color:#fb8">flow</span> ·
    <span style="color:#9ad">iface</span> ·
    <span style="color:#aab">bridge</span> ·
    <span style="color:#cda">backend</span> ·
    <span style="color:#e2b04a">external</span> ·
    <span style="color:#9cd">subnet</span>
  </div>
  <div style="color:#666;font-size:10px;margin-bottom:6px;">
    edges:
    <span style="color:#aaa"> - chain</span>
    <span style="color:#ff8aa0"> - DNAT</span>
    <span style="color:#c84">- - hairpin (VM-originated)</span>
    <span style="color:#ca6">- - SNAT return</span>
  </div>
  <svg id="nat-graph" width="100%" height="420" viewBox="0 0 1200 420" preserveAspectRatio="xMidYMid meet" style="cursor:grab;display:block;"></svg>
</div>
<script id="nat-graph-data" type="application/json">` + string(payload) + `</script>
<script>
(function(){
  const data = JSON.parse(document.getElementById("nat-graph-data").textContent);
  const svg = document.getElementById("nat-graph");
  const W = 1200, H = 420;
  const GROUP_COLOR = {
    external:"#e2b04a",
    iface:   "#88aadd",
    chain:   "#bb88dd",
    flow:    "#ffbb88",
    bridge:  "#aaaabb",
    backend: "#ccddaa",
    subnet:  "#88cccc",
  };
  const EDGE_STYLE = {
    "":        { stroke:"#666", dash:"" },
    "dnat":    { stroke:"#ff8aa0", dash:"" },
    "hairpin": { stroke:"#c84",    dash:"4,3" },
    "snat":    { stroke:"#ca6",    dash:"2,3" },
  };
  const COL_X = {
    external: 100, iface: 280, chain: 460, flow: 620, bridge: 800, backend: 1080, subnet: 100,
  };
  const COL_Y = { subnet: 360 };

  const nodes = data.nodes.map(n => ({
    ...n,
    x: (COL_X[n.group] || W/2) + (Math.random()-0.5)*40,
    y: (COL_Y[n.group] || H/2) + (Math.random()-0.5)*80,
    vx: 0, vy: 0,
  }));
  const idx = new Map(nodes.map((n,i) => [n.id, i]));
  const adj = nodes.map(() => new Set());
  const edges = [];
  data.edges.forEach(e => {
    const s = idx.get(e.source), t = idx.get(e.target);
    if (s === undefined || t === undefined) return;
    edges.push({source:s, target:t, style:e.style||"", label:e.label||""});
    adj[s].add(t); adj[t].add(s);
  });

  let alpha = 1.0;
  const targetDist = 90, repulse = 1400, xPull = 0.06, yCenter = 0.006, damping = 0.85;

  function step() {
    if (alpha < 0.003) return;
    for (let i = 0; i < nodes.length; i++) {
      for (let j = i+1; j < nodes.length; j++) {
        const a = nodes[i], b = nodes[j];
        let dx = b.x-a.x, dy = b.y-a.y, d2 = dx*dx+dy*dy;
        if (d2 < 1) d2 = 1;
        const d = Math.sqrt(d2), f = repulse/d2;
        const fx=(dx/d)*f, fy=(dy/d)*f;
        if (!a.pinned) { a.vx -= fx; a.vy -= fy; }
        if (!b.pinned) { b.vx += fx; b.vy += fy; }
      }
    }
    for (const e of edges) {
      const a = nodes[e.source], b = nodes[e.target];
      const dx = b.x-a.x, dy = b.y-a.y;
      const d = Math.sqrt(dx*dx+dy*dy) || 0.001;
      const delta = d - targetDist;
      const fx=(dx/d)*delta*0.05, fy=(dy/d)*delta*0.05;
      if (!a.pinned) { a.vx += fx; a.vy += fy; }
      if (!b.pinned) { b.vx -= fx; b.vy -= fy; }
    }
    for (const n of nodes) {
      if (n.pinned) continue;
      const tx = COL_X[n.group], ty = COL_Y[n.group];
      if (tx !== undefined) n.vx += (tx - n.x) * xPull;
      if (ty !== undefined) n.vy += (ty - n.y) * yCenter * 8;
      else n.vy += (H/2 - n.y) * yCenter;
    }
    for (const n of nodes) {
      if (n.pinned) continue;
      n.vx *= damping; n.vy *= damping;
      n.x += n.vx * alpha; n.y += n.vy * alpha;
      n.x = Math.max(20, Math.min(W-20, n.x));
      n.y = Math.max(20, Math.min(H-20, n.y));
    }
    alpha *= 0.988;
  }

  const ns = "http://www.w3.org/2000/svg";
  const gEdges = document.createElementNS(ns, "g");
  const gNodes = document.createElementNS(ns, "g");
  svg.appendChild(gEdges); svg.appendChild(gNodes);

  const edgeEls = edges.map(e => {
    const el = document.createElementNS(ns, "line");
    const st = EDGE_STYLE[e.style] || EDGE_STYLE[""];
    el.setAttribute("stroke", st.stroke);
    el.setAttribute("stroke-width", "1.4");
    if (st.dash) el.setAttribute("stroke-dasharray", st.dash);
    el.setAttribute("opacity", "0.85");
    gEdges.appendChild(el);
    return el;
  });

  const nodeEls = nodes.map(n => {
    const g = document.createElementNS(ns, "g");
    g.style.cursor = "pointer";
    const c = document.createElementNS(ns, "circle");
    c.setAttribute("r", n.group === "flow" || n.group === "chain" ? 13 : 11);
    c.setAttribute("fill", GROUP_COLOR[n.group] || "#888");
    c.setAttribute("stroke", "#222"); c.setAttribute("stroke-width", "1");
    g.appendChild(c);
    const t = document.createElementNS(ns, "text");
    t.setAttribute("font-family","monospace"); t.setAttribute("font-size","10");
    t.setAttribute("fill","#ddd"); t.setAttribute("text-anchor","middle"); t.setAttribute("y","-16");
    t.style.pointerEvents = "none";
    t.textContent = n.label;
    g.appendChild(t);
    if (n.sub) {
      const sub = document.createElementNS(ns, "text");
      sub.setAttribute("font-family","monospace"); sub.setAttribute("font-size","9");
      sub.setAttribute("fill","#888"); sub.setAttribute("text-anchor","middle"); sub.setAttribute("y","22");
      sub.style.pointerEvents = "none";
      sub.textContent = n.sub;
      g.appendChild(sub);
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
      nodeEls[i].setAttribute("transform", "translate("+nodes[i].x+","+nodes[i].y+")");
    }
  }
  function tick(){ step(); render(); requestAnimationFrame(tick); }
  tick();

  // drag + focus
  let dragIdx = -1, dragMoved = false, dragStart = null, focusedIdx = -1;
  function ptFromEvent(ev) {
    const rect = svg.getBoundingClientRect();
    const sx = W/rect.width, sy = H/rect.height;
    const cx = (ev.touches ? ev.touches[0].clientX : ev.clientX) - rect.left;
    const cy = (ev.touches ? ev.touches[0].clientY : ev.clientY) - rect.top;
    return [cx*sx, cy*sy];
  }
  function down(ev, i){
    ev.preventDefault(); ev.stopPropagation();
    dragIdx = i; dragMoved = false; nodes[i].pinned = true;
    dragStart = ptFromEvent(ev); alpha = Math.max(alpha, 0.3);
  }
  function move(ev){
    if (dragIdx < 0) return;
    const [x,y] = ptFromEvent(ev);
    if (dragStart) {
      const dx = x-dragStart[0], dy = y-dragStart[1];
      if (dx*dx+dy*dy > 16) dragMoved = true;
    }
    nodes[dragIdx].x = x; nodes[dragIdx].y = y;
    nodes[dragIdx].vx = 0; nodes[dragIdx].vy = 0;
    alpha = Math.max(alpha, 0.2);
  }
  function up(){
    if (dragIdx >= 0 && !dragMoved) {
      focusedIdx = (focusedIdx === dragIdx) ? -1 : dragIdx;
      applyFocus();
    }
    dragIdx = -1; dragStart = null;
  }
  function applyFocus() {
    if (focusedIdx < 0) {
      nodeEls.forEach(el => el.style.opacity = "1");
      edgeEls.forEach(el => el.style.opacity = "0.85");
      return;
    }
    const keep = new Set([focusedIdx]);
    adj[focusedIdx].forEach(n => keep.add(n));
    nodeEls.forEach((el, i) => el.style.opacity = keep.has(i) ? "1" : "0.12");
    edgeEls.forEach((el, i) => {
      const e = edges[i];
      el.style.opacity = (e.source === focusedIdx || e.target === focusedIdx) ? "1" : "0.05";
    });
  }
  nodeEls.forEach((el, i) => {
    el.addEventListener("mousedown", ev => down(ev, i));
    el.addEventListener("touchstart", ev => down(ev, i), {passive:false});
    el.addEventListener("dblclick", ev => { ev.stopPropagation(); nodes[i].pinned = false; alpha = Math.max(alpha, 0.2); });
  });
  window.addEventListener("mousemove", move);
  window.addEventListener("touchmove", move, {passive:false});
  window.addEventListener("mouseup", up);
  window.addEventListener("touchend", up);
  svg.addEventListener("click", ev => { if (ev.target === svg) { focusedIdx = -1; applyFocus(); } });
})();
</script>`
	return template.HTML(html)
}
