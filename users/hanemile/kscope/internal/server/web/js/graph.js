// js/graph.js — High-quality force-directed graph renderer.
//
// Architecture:
//   - Each graph panel gets its OWN GraphInstance (no shared global state).
//   - Canvas 2D with correct devicePixelRatio scaling (no blur).
//   - Simulation stops automatically when kinetic energy drops below threshold.
//   - Individual nodes are draggable; pan on background; pinch-zoom on touch.
//   - Click a node to select (shows info overlay); click again for context menu.
//   - Double-click a node to navigate (file → open, symbol → XRef panel).
//   - Zoom towards cursor on wheel.
//
// Part of github.com/hanemile/kscope
"use strict";

// ── Kind visual config ────────────────────────────────────────────────────────
const GK = {
  kernel:     { col: "#00ff7f", r: 7  },
  uapi:       { col: "#ffe000", r: 7  },
  duplicated: { col: "#ff3333", r: 9  },
  exported:   { col: "#cc44ff", r: 7  },
  syscall:    { col: "#00e5ff", r: 8  },
  file:       { col: "#2299ee", r: 5  },
  dir:        { col: "#555555", r: 10 },
  function:   { col: "#4488ff", r: 6  },
  unknown:    { col: "#333333", r: 5  },
};
const gkind = k => GK[k] || GK.unknown;

// ── GraphInstance — one per panel ─────────────────────────────────────────────
class GraphInstance {
  constructor(canvas, slot) {
    this.canvas    = canvas;
    this.slot      = slot;
    this.nodes     = [];
    this.edges     = [];
    this.ox = 0; this.oy = 0; this.zoom = 1;
    this.dpr       = Math.max(1, window.devicePixelRatio || 1);
    this.animId    = null;
    this.running   = false;
    this.energy    = Infinity;
    this.mode      = "full";
    this.centerSym = null;
    this.hover     = null;
    this.dragNode  = null;
    this.dragOff   = null;
    this.panning   = false;
    this.panStart  = null;
    this.panOrigin = null;
    this.selected  = null;
    this._downPos  = null;
    this._bound    = false;
  }

  // ── DPR-correct sizing ────────────────────────────────────────────────────
  resize() {
    const c = this.canvas, dpr = this.dpr;
    const w = c.offsetWidth || 600, h = c.offsetHeight || 400;
    if (c.width !== w * dpr || c.height !== h * dpr) {
      c.width = w * dpr; c.height = h * dpr;
      c.style.width = w + "px"; c.style.height = h + "px";
    }
  }
  get W() { return this.canvas.width  / this.dpr; }
  get H() { return this.canvas.height / this.dpr; }

  // ── Build from API data ───────────────────────────────────────────────────
  build(d) {
    const W = this.W, H = this.H;
    const raw = d.nodes || [];
    const N   = raw.length;
    const focused = this.mode === "xref" || this.mode === "callgraph";

    this.nodes = raw.map((n, i) => {
      let x, y;
      if (focused && i === 0) {
        x = W / 2; y = H / 2;
      } else {
        // Fibonacci spiral — uniform density, no clustering
        const idx  = focused ? i - 1 : i;
        const tot  = Math.max(1, N - (focused ? 1 : 0));
        const frac = tot < 2 ? 0.5 : idx / (tot - 1);
        const r    = Math.sqrt(frac) * Math.min(W, H) * 0.42;
        const ang  = idx * 2.399963; // golden angle rad
        x = W / 2 + Math.cos(ang) * r;
        y = H / 2 + Math.sin(ang) * r;
      }
      const vis = gkind(n.kind);
      return { ...n, x, y, vx: 0, vy: 0, r: vis.r,
               mass: vis.r * vis.r,
               pinned: focused && i === 0 };
    });

    this.edges = (d.edges || []).filter(e =>
      e && typeof e.source === "number" && typeof e.target === "number" &&
      e.source >= 0 && e.source < this.nodes.length &&
      e.target >= 0 && e.target < this.nodes.length &&
      e.source !== e.target
    );
    this.energy = Infinity;
    this.selected = null;
    this.hover    = null;
  }

  // ── Force step ────────────────────────────────────────────────────────────
  step() {
    const N = this.nodes.length; if (N === 0) return;
    const { W, H, nodes, edges } = this;
    const focused = this.mode === "xref" || this.mode === "callgraph";
    const dk = N < 15 && focused ? 0.4 : N < 40 && focused ? 0.6 : 0.9;
    const k  = Math.sqrt(W * H / (N + 1)) * dk;
    const k2 = k * k;

    for (let i = 0; i < N; i++) { nodes[i].vx = 0; nodes[i].vy = 0; }

    // Grid-bucketed repulsion — O(N) expected
    const cs   = k * 2.5;
    const cols = Math.max(1, Math.ceil(W / cs) | 0);
    const rows = Math.max(1, Math.ceil(H / cs) | 0);
    const grid = new Array(cols * rows).fill(null).map(() => []);
    for (let i = 0; i < N; i++) {
      const cx = Math.min(cols-1, Math.max(0, (nodes[i].x / cs) | 0));
      const cy = Math.min(rows-1, Math.max(0, (nodes[i].y / cs) | 0));
      grid[cy * cols + cx].push(i);
    }
    for (let i = 0; i < N; i++) {
      const ni = nodes[i];
      const cx = Math.min(cols-1, Math.max(0, (ni.x / cs) | 0));
      const cy = Math.min(rows-1, Math.max(0, (ni.y / cs) | 0));
      for (let dy = -2; dy <= 2; dy++) {
        for (let dx = -2; dx <= 2; dx++) {
          const bx = cx+dx, by = cy+dy;
          if (bx < 0 || bx >= cols || by < 0 || by >= rows) continue;
          for (const j of grid[by * cols + bx]) {
            if (j <= i) continue;
            const nj = nodes[j];
            const ddx = ni.x - nj.x, ddy = ni.y - nj.y;
            const d2  = ddx*ddx + ddy*ddy || 1e-4;
            const d   = Math.sqrt(d2);
            const f   = k2 / d;
            const fx  = ddx/d*f, fy = ddy/d*f;
            ni.vx += fx/ni.mass; ni.vy += fy/ni.mass;
            nj.vx -= fx/nj.mass; nj.vy -= fy/nj.mass;
          }
        }
      }
    }

    // Spring attraction along edges
    for (const e of edges) {
      const s = nodes[e.source], t = nodes[e.target];
      const dx = t.x-s.x, dy = t.y-s.y;
      const d  = Math.sqrt(dx*dx+dy*dy) || 1e-4;
      const wt = Math.min(3, e.weight || 1);
      const f  = ((d - k) / d) * 0.4 * wt;
      if (!s.pinned) { s.vx += dx*f/s.mass; s.vy += dy*f/s.mass; }
      if (!t.pinned) { t.vx -= dx*f/t.mass; t.vy -= dy*f/t.mass; }
    }

    // Centering gravity
    for (const n of nodes) {
      n.vx += (W/2 - n.x) * 0.003;
      n.vy += (H/2 - n.y) * 0.003;
    }

    // Integrate
    let ke = 0;
    for (const n of nodes) {
      if (n.pinned) continue;
      n.vx *= 0.85; n.vy *= 0.85;
      n.x = Math.max(n.r+1, Math.min(W-n.r-1, n.x + n.vx));
      n.y = Math.max(n.r+1, Math.min(H-n.r-1, n.y + n.vy));
      ke  += n.vx*n.vx + n.vy*n.vy;
    }
    this.energy = ke / Math.max(1, N);
  }

  // ── Draw ──────────────────────────────────────────────────────────────────
  draw() {
    const canvas = this.canvas;
    const ctx    = canvas.getContext("2d");
    const dpr    = this.dpr;
    const { W, H, nodes, edges } = this;

    ctx.setTransform(1,0,0,1,0,0);
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.setTransform(dpr,0,0,dpr,0,0);
    ctx.save();
    ctx.translate(this.ox, this.oy);
    ctx.scale(this.zoom, this.zoom);

    // Edges
    for (const e of edges) {
      const s = nodes[e.source], t = nodes[e.target];
      if (!s || !t) continue;
      const isDup   = e.label && e.label.startsWith("duplicate:");
      const isCalls = e.label === "calls";
      const isUses  = e.label === "uses";
      const lit = s === this.hover || t === this.hover ||
                  s === this.selected || t === this.selected;
      const al  = lit ? 0.85 : 0.18;
      const lw  = isDup ? 1.8 : isCalls || isUses ? 1.1 : 0.7;
      const col = isDup   ? "rgba(255,51,51," +al+")"
                : isCalls ? "rgba(0,136,255," +al+")"
                : isUses  ? "rgba(0,200,220," +al+")"
                :            "rgba(55,55,55," +al+")";
      ctx.beginPath(); ctx.moveTo(s.x, s.y); ctx.lineTo(t.x, t.y);
      ctx.strokeStyle = col; ctx.lineWidth = lw; ctx.stroke();
      if (isCalls || isUses) {
        const ang = Math.atan2(t.y-s.y, t.x-s.x);
        const tx2 = t.x - Math.cos(ang)*(t.r+2);
        const ty2 = t.y - Math.sin(ang)*(t.r+2);
        ctx.beginPath();
        ctx.moveTo(tx2, ty2);
        ctx.lineTo(tx2 - Math.cos(ang-0.4)*7, ty2 - Math.sin(ang-0.4)*7);
        ctx.lineTo(tx2 - Math.cos(ang+0.4)*7, ty2 - Math.sin(ang+0.4)*7);
        ctx.closePath(); ctx.fillStyle = col; ctx.fill();
      }
    }

    // Nodes
    const focused = this.mode === "xref" || this.mode === "callgraph";
    for (let i = 0; i < nodes.length; i++) {
      const n = nodes[i];
      const vis      = gkind(n.kind);
      const isCenter = focused && i === 0;
      const isHov    = n === this.hover;
      const isSel    = n === this.selected;
      const r = n.r + (isCenter?4:0) + (isHov||isSel?2:0);

      if (isHov || isSel || isCenter) {
        ctx.shadowColor = vis.col;
        ctx.shadowBlur  = isCenter ? 20 : 10;
      }
      ctx.beginPath(); ctx.arc(n.x, n.y, r, 0, Math.PI*2);
      ctx.fillStyle   = vis.col;
      ctx.globalAlpha = isHov||isSel||isCenter ? 1 : 0.78;
      ctx.fill();
      ctx.globalAlpha = 1; ctx.shadowBlur = 0;

      if (isSel || isCenter) {
        ctx.strokeStyle = "#fff";
        ctx.lineWidth   = isCenter ? 2.5 : 2;
        ctx.stroke();
      }
      if (isCenter || isSel || isHov) this._label(ctx, n, r, isCenter||isSel);
    }

    // Selected info panel
    if (this.selected) this._infoPanel(ctx, W, H);
    ctx.restore();
  }

  _label(ctx, n, r, prominent) {
    const txt = n.label.length > 44 ? n.label.slice(0,42)+"…" : n.label;
    ctx.font = prominent ? "bold 12px monospace" : "10px monospace";
    ctx.textBaseline = "middle";
    const tw = ctx.measureText(txt).width;
    const lx = n.x + r + 5, ly = n.y;
    const fh = prominent ? 18 : 14, pd = prominent ? 4 : 3;
    ctx.fillStyle = "rgba(6,6,6,0.84)";
    if (ctx.roundRect) {
      ctx.beginPath(); ctx.roundRect(lx-pd, ly-fh/2, tw+pd*2, fh, 3); ctx.fill();
    } else {
      ctx.fillRect(lx-pd, ly-fh/2, tw+pd*2, fh);
    }
    ctx.fillStyle = prominent ? "#fff" : "#ccc";
    ctx.fillText(txt, lx, ly);
    ctx.textBaseline = "alphabetic";
  }

  _infoPanel(ctx, W, H) {
    const n = this.selected, vis = gkind(n.kind);
    const pw = Math.min(290, W-20), ph = 82;
    const px = 10, py = H-ph-10;
    ctx.fillStyle = "rgba(8,8,8,0.9)";
    ctx.strokeStyle = vis.col; ctx.lineWidth = 1;
    ctx.beginPath();
    if (ctx.roundRect) { ctx.roundRect(px,py,pw,ph,5); }
    else { ctx.rect(px,py,pw,ph); }
    ctx.fill(); ctx.stroke();
    ctx.fillStyle = vis.col; ctx.font = "bold 11px monospace";
    ctx.textBaseline = "top";
    ctx.fillText(n.label.slice(0,34), px+8, py+8);
    ctx.fillStyle = "#888"; ctx.font = "9px sans-serif";
    ctx.fillText(n.kind, px+8, py+26);
    ctx.fillStyle = "#555"; ctx.font = "9px monospace";
    const sid = n.id.length>44 ? n.id.slice(0,42)+"…" : n.id;
    ctx.fillText(sid, px+8, py+42);
    ctx.fillStyle = "#444"; ctx.font = "9px sans-serif";
    ctx.fillText("Click again → menu  ·  Dbl-click → navigate  ·  Esc → deselect", px+8, py+60);
    ctx.textBaseline = "alphabetic";
  }

  // ── Sim loop ──────────────────────────────────────────────────────────────
  start() {
    if (this.running) return;
    this.running = true;
    let idle = 0;
    const THRESH = 0.008;
    const tick = () => {
      if (!this.running) return;
      this.resize();
      if (this.energy > THRESH) {
        const steps = this.energy > 5 ? 4 : this.energy > 0.5 ? 2 : 1;
        for (let s = 0; s < steps; s++) this.step();
        idle = 0;
      } else {
        idle++;
        if (idle > 80) {
          this.draw();
          this.running = false; // sleep
          return;
        }
      }
      this.draw();
      this.animId = requestAnimationFrame(tick);
    };
    this.animId = requestAnimationFrame(tick);
  }
  wake() {
    if (this.running) return;
    this.running = true;
    this.start();
  }
  stop() {
    this.running = false;
    if (this.animId) { cancelAnimationFrame(this.animId); this.animId = null; }
  }

  // ── Coords ────────────────────────────────────────────────────────────────
  toWorld(cx, cy) {
    const r = this.canvas.getBoundingClientRect();
    return { x: (cx-r.left-this.ox)/this.zoom, y: (cy-r.top-this.oy)/this.zoom };
  }
  nodeAt(wx, wy) {
    for (let i = this.nodes.length-1; i >= 0; i--) {
      const n = this.nodes[i];
      if ((n.x-wx)**2 + (n.y-wy)**2 < (n.r+8)**2) return n;
    }
    return null;
  }

  // ── Events ────────────────────────────────────────────────────────────────
  bindEvents() {
    if (this._bound) return;
    this._bound = true;
    const c = this.canvas;

    c.addEventListener("pointerdown", e => {
      if (e.button === 1) e.preventDefault();
      c.setPointerCapture(e.pointerId);
      this._downPos = { x: e.clientX, y: e.clientY };
      const w = this.toWorld(e.clientX, e.clientY);
      const n = this.nodeAt(w.x, w.y);
      if (n) {
        this.dragNode = n; this.dragOff = { x: w.x-n.x, y: w.y-n.y };
        n.pinned = true; this.energy = 0.5;
      } else {
        this.panning = true;
        this.panStart  = { x: e.clientX, y: e.clientY };
        this.panOrigin = { x: this.ox, y: this.oy };
      }
    });

    c.addEventListener("pointermove", e => {
      const w = this.toWorld(e.clientX, e.clientY);
      if (this.dragNode) {
        this.dragNode.x  = w.x - this.dragOff.x;
        this.dragNode.y  = w.y - this.dragOff.y;
        this.dragNode.vx = 0; this.dragNode.vy = 0;
        this.energy = 0.5; this.wake(); return;
      }
      if (this.panning) {
        this.ox = this.panOrigin.x + (e.clientX - this.panStart.x);
        this.oy = this.panOrigin.y + (e.clientY - this.panStart.y);
        this.wake(); return;
      }
      const hit = this.nodeAt(w.x, w.y);
      if (hit !== this.hover) { this.hover = hit; this.wake(); }
      const tip = this.slot && qs(".g-tip", this.slot);
      if (tip) {
        if (hit) {
          tip.style.cssText = "display:block;left:"+(e.clientX+14)+"px;top:"+(e.clientY-10)+"px";
          tip.innerHTML = "<b>"+esc(hit.label)+"</b><br><span style=\"color:var(--text3)\">"+esc(hit.kind)+"</span>";
        } else { tip.style.display = "none"; }
      }
    });

    c.addEventListener("pointerup", e => {
      const dn = this.dragNode, pan = this.panning;
      if (this.dragNode) {
        const focused = this.mode==="xref"||this.mode==="callgraph";
        if (!(focused && this.dragNode===this.nodes[0])) this.dragNode.pinned = false;
        this.dragNode = null;
      }
      this.panning = false;
      const dp = this._downPos;
      const isClick = dp && Math.hypot(e.clientX-dp.x, e.clientY-dp.y) < 5;
      if (isClick && !dn) {
        const w = this.toWorld(e.clientX, e.clientY);
        const n = this.nodeAt(w.x, w.y);
        if (n) {
          if (this.selected === n) {
            this.selected = null; this.wake();
            ctxTarget = { sym: n.label, kind: n.kind,
              file: n.id.startsWith("file:") ? n.id.slice(5) : "", line: 0 };
            showCtxMenu(e.clientX, e.clientY, [
              { title: n.label }, "-",
              { label:"Symbol detail",   action:"symbol_detail",  icon:"ℹ"  },
              { label:"XRef usages",     action:"xref",           icon:"🔍" },
              { label:"Call graph",      action:"callgraph",      icon:"↕"  },
              { label:"Send to plugin…", action:"send_to_plugin", icon:"⌁"  },
              { label:"Add to nodes",    action:"addnode",        icon:"◆"  },
              { label:"Copy name",       action:"copyname",       icon:"📋" },
            ]);
          } else { this.selected = n; this.wake(); }
        } else if (this.selected) { this.selected = null; this.wake(); }
      }
    });

    c.addEventListener("dblclick", e => {
      const w = this.toWorld(e.clientX, e.clientY);
      const n = this.nodeAt(w.x, w.y);
      if (!n) return;
      this.selected = null;
      if (n.id.startsWith("file:")) openFile(n.id.slice(5));
      else openPanelWithState("xref", { sym: n.label });
    });

    c.addEventListener("wheel", e => {
      e.preventDefault();
      const f = e.deltaY < 0 ? 1.12 : 1/1.12;
      const r = c.getBoundingClientRect();
      const mx = e.clientX-r.left, my = e.clientY-r.top;
      const nz = Math.max(0.04, Math.min(16, this.zoom*f));
      this.ox = mx - (mx-this.ox)*(nz/this.zoom);
      this.oy = my - (my-this.oy)*(nz/this.zoom);
      this.zoom = nz; this.wake();
    }, { passive:false });

    document.addEventListener("keydown", e => {
      if (e.key==="Escape"&&this.selected) { this.selected=null; this.wake(); }
    });

    // Touch pinch-zoom + pan
    let tc = {}, initD=0, initZ=1, initM=null, initO={x:0,y:0};
    c.addEventListener("touchstart", e => {
      e.preventDefault();
      for (const t of e.changedTouches) tc[t.identifier]={x:t.clientX,y:t.clientY};
      const ids=Object.keys(tc);
      if (ids.length===2){
        const a=tc[ids[0]],b=tc[ids[1]];
        initD=Math.hypot(b.x-a.x,b.y-a.y); initZ=this.zoom;
        initM={(x:(a.x+b.x)/2,y:(a.y+b.y)/2)};
        initO={x:this.ox,y:this.oy};
      }
    },{passive:false});
    c.addEventListener("touchmove", e => {
      e.preventDefault();
      for (const t of e.changedTouches) tc[t.identifier]={x:t.clientX,y:t.clientY};
      const ids=Object.keys(tc);
      if (ids.length===1){
        const t=tc[ids[0]];
        if(!this.panning){this.panning=true;this.panStart=t;this.panOrigin={x:this.ox,y:this.oy};}
        this.ox=this.panOrigin.x+(t.x-this.panStart.x);
        this.oy=this.panOrigin.y+(t.y-this.panStart.y); this.wake();
      } else if (ids.length===2){
        const a=tc[ids[0]],b=tc[ids[1]];
        const d=Math.hypot(b.x-a.x,b.y-a.y), sc=d/(initD||1);
        const nz=Math.max(0.04,Math.min(16,initZ*sc));
        const r=c.getBoundingClientRect(), mx=initM.x-r.left, my=initM.y-r.top;
        this.zoom=nz; this.ox=mx-(mx-initO.x)*(nz/initZ);
        this.oy=my-(my-initO.y)*(nz/initZ); this.wake();
      }
    },{passive:false});
    c.addEventListener("touchend", e=>{
      for(const t of e.changedTouches) delete tc[t.identifier];
      if(Object.keys(tc).length<2) this.panning=false;
    },{passive:false});
  }

  // ── Controls ──────────────────────────────────────────────────────────────
  resetView() { this.zoom=1; this.ox=0; this.oy=0; this.wake(); }
  zoomBy(f) {
    const mx=this.W/2, my=this.H/2;
    const nz=Math.max(0.04,Math.min(16,this.zoom*f));
    this.ox=mx-(mx-this.ox)*(nz/this.zoom);
    this.oy=my-(my-this.oy)*(nz/this.zoom);
    this.zoom=nz; this.wake();
  }
  reheat() {
    for(const n of this.nodes){n.vx=0;n.vy=0;}
    this.energy=Infinity; this.wake();
  }
  fitView() {
    if(!this.nodes.length) return;
    let x0=Infinity,y0=Infinity,x1=-Infinity,y1=-Infinity;
    for(const n of this.nodes){
      x0=Math.min(x0,n.x-n.r); y0=Math.min(y0,n.y-n.r);
      x1=Math.max(x1,n.x+n.r); y1=Math.max(y1,n.y+n.r);
    }
    const W=this.W,H=this.H,pad=40;
    const nz=Math.min((W-pad*2)/(x1-x0||1),(H-pad*2)/(y1-y0||1),4);
    this.zoom=nz;
    this.ox=W/2-((x0+x1)/2)*nz;
    this.oy=H/2-((y0+y1)/2)*nz;
    this.wake();
  }
  exportSVG() {
    const W=this.W,H=this.H;
    let svg=`<svg xmlns="http://www.w3.org/2000/svg" width="${W}" height="${H}" style="background:#000">`;
    for(const e of this.edges){
      const s=this.nodes[e.source],t=this.nodes[e.target]; if(!s||!t) continue;
      const col=e.label?.startsWith("duplicate:")?"#f33":e.label==="calls"?"#08f":"#333";
      svg+=`<line x1="${(s.x*this.zoom+this.ox).toFixed(1)}" y1="${(s.y*this.zoom+this.oy).toFixed(1)}" x2="${(t.x*this.zoom+this.ox).toFixed(1)}" y2="${(t.y*this.zoom+this.oy).toFixed(1)}" stroke="${col}" stroke-width="0.8"/>`;
    }
    for(const n of this.nodes){
      const v=gkind(n.kind);
      const cx=(n.x*this.zoom+this.ox).toFixed(1),cy=(n.y*this.zoom+this.oy).toFixed(1),r=(n.r*this.zoom).toFixed(1);
      svg+=`<circle cx="${cx}" cy="${cy}" r="${r}" fill="${v.col}" opacity="0.85"/>`;
      if(n.r*this.zoom>5) svg+=`<text x="${(+cx+ +r+3).toFixed(1)}" y="${(+cy+4).toFixed(1)}" font-size="10" fill="#aaa">${esc(n.label.slice(0,24))}</text>`;
    }
    svg+="</svg>";
    const a=Object.assign(document.createElement("a"),{
      href:URL.createObjectURL(new Blob([svg],{type:"image/svg+xml"})),
      download:"kscope-graph-"+Date.now()+".svg"
    });
    a.click(); URL.revokeObjectURL(a.href);
  }
}

// ── Instance registry (WeakMap so GC handles dead canvases) ───────────────────
const _GI = new WeakMap();

// ── Public init ───────────────────────────────────────────────────────────────
async function initGraphSlot(slot, st) {
  const canvas = qs("canvas", slot); if (!canvas) return;
  const old = _GI.get(canvas); if (old) old.stop();

  const inst = new GraphInstance(canvas, slot);
  _GI.set(canvas, inst);
  inst.mode      = (st&&st.mode)  || "full";
  inst.centerSym = (st&&st.sym)   || null;
  inst.resize();

  const info = qs(".g-info", slot);
  if (info) info.textContent = "Loading…";

  let d = { nodes:[], edges:[] };
  try {
    if (inst.mode==="xref" && inst.centerSym) {
      const depth = (st&&st.depth)||1;
      d = await api("/xrefgraph/"+encodeURIComponent(inst.centerSym)
                  +"?version="+encodeURIComponent(A.activeVer)+"&depth="+depth);
    } else if (inst.mode==="callgraph" && inst.centerSym) {
      const depth = (st&&st.depth)||2;
      d = await api("/callgraph/"+encodeURIComponent(inst.centerSym)
                  +"?version="+encodeURIComponent(A.activeVer)+"&depth="+depth);
    } else {
      const max = Math.min(500, Math.max(80, (inst.W/1.8)|0));
      d = await api("/graph?max="+max+"&version="+encodeURIComponent(A.activeVer));
    }
  } catch(e) { if(info) info.textContent="✗ "+e.message; return; }

  inst.build(d); inst.bindEvents(); inst.start();
  if (info) info.textContent = inst.nodes.length+" nodes · "+inst.edges.length+" edges";

  // BSP wiring
  const leafId = bspLeafIdFromSlot(slot);
  if (leafId!=null && typeof bspSetTitle==="function") {
    const ml = inst.mode==="xref"?"XRef: ":inst.mode==="callgraph"?"CallGraph: ":"Graph";
    bspSetTitle(leafId, "<b>"+esc(ml)+"</b>"+esc(inst.centerSym||""));
    const bot = document.getElementById("bsp-bot-"+leafId);
    if (bot) {
      canvas._gInst = inst;
      bot.innerHTML = `
        <button class="bsp-bot-btn" onclick="gInstCtrl(this,'reset')">⟳ Reset</button>
        <button class="bsp-bot-btn" onclick="gInstCtrl(this,'fit')">⊡ Fit</button>
        <button class="bsp-bot-btn" onclick="gInstCtrl(this,'zoomin')">+</button>
        <button class="bsp-bot-btn" onclick="gInstCtrl(this,'zoomout')">−</button>
        <div class="bsp-bot-sep"></div>
        <button class="bsp-bot-btn" onclick="gInstCtrl(this,'reheat')">↺ Reheat</button>
        <div class="bsp-bot-sep"></div>
        <button class="bsp-bot-btn" onclick="gInstCtrl(this,'svg')">↓ SVG</button>`;
      bot.classList.add("has-content");
    }
  }
  setTimeout(()=>{ if(_GI.get(canvas)===inst) inst.fitView(); }, 1400);
}

// ── Bottom-bar button dispatch ─────────────────────────────────────────────────
function gInstCtrl(btn, action) {
  let el = btn.parentElement;
  while (el) {
    const c = el.querySelector("canvas");
    if (c && _GI.has(c)) {
      const inst = _GI.get(c);
      if (action==="reset")   inst.resetView();
      else if(action==="fit") inst.fitView();
      else if(action==="zoomin")  inst.zoomBy(1.3);
      else if(action==="zoomout") inst.zoomBy(1/1.3);
      else if(action==="reheat")  inst.reheat();
      else if(action==="svg")     inst.exportSVG();
      return;
    }
    el = el.parentElement;
  }
}
window.gInstCtrl = gInstCtrl;

// Context menu callgraph
Bus.on("ctx:action", ({action,target})=>{
  if(action!=="callgraph"||!target||!target.sym) return;
  openPanelWithState("graph",{mode:"callgraph",sym:target.sym,depth:2});
});

// Legacy compat stubs
function gReset(){}; function gZoom(){}; function gReheat(){}; function gToggleDup(){}; function gExport(){};
window.gReset=gReset; window.gZoom=gZoom; window.gReheat=gReheat; window.gToggleDup=gToggleDup; window.gExport=gExport;
