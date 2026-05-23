// status-board: tiny dashboard for the medano fleet.
//
// Polls each VM's node-exporter, asks virsh for libvirt state, walks the
// storagebox restic dirs for backup freshness, renders one HTML page.
//
// Env:
//   STATUS_BOARD_INVENTORY -- path to JSON file with [{name, ip, bridge}]
//   STATUS_BOARD_LISTEN    -- bind address:port (default 127.0.0.1:8090)
//   STATUS_BOARD_STORAGEBOX -- base path of restic repos (default /mnt/storagebox-bx11/backup)
package main

import (
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type VM struct {
	Name   string `json:"name"`
	IP     string `json:"ip"`
	Bridge string `json:"bridge"`
}

type Probe struct {
	Name     string
	URL      string
	Up       bool
	HTTPCode int
}

type Restic struct {
	Exists    string
	Snapshots int
	AgeHours  float64
	Size      string
}

type VirshInfo struct {
	State      string
	MaxMemKiB  int64
	UsedMemKiB int64
	VCPUs      string
}

type VMStat struct {
	VM
	Reachable bool
	Probes    []Probe
	MemTotal  int64
	MemUsed   int64
	Virsh     VirshInfo
	Restic    Restic
}

var (
	inventory     []VM
	listenAddr    = "127.0.0.1:8090"
	storageboxDir = "/mnt/storagebox-bx11/backup"
	metricLine    = regexp.MustCompile(`^([a-zA-Z_:][a-zA-Z0-9_:]*)(\{[^}]*\})?\s+([-0-9.eE+]+|NaN|\+Inf|-Inf)\s*$`)
	labelKV       = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)="((?:[^"\\]|\\.)*)"`)
	httpClient    = &http.Client{Timeout: 3 * time.Second}
)

func loadInventory() error {
	path := os.Getenv("STATUS_BOARD_INVENTORY")
	if path == "" {
		return fmt.Errorf("STATUS_BOARD_INVENTORY not set")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, &inventory)
}

type metricRow struct {
	Labels map[string]string
	Value  float64
}

func fetchMetrics(ip string) (map[string][]metricRow, error) {
	url := fmt.Sprintf("http://%s:9100/metrics", ip)
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	out := map[string][]metricRow{}
	for _, line := range strings.Split(string(body), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := metricLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name, rawLabels, value := m[1], m[2], m[3]
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			continue
		}
		labels := map[string]string{}
		if rawLabels != "" {
			for _, lm := range labelKV.FindAllStringSubmatch(rawLabels, -1) {
				labels[lm[1]] = lm[2]
			}
		}
		out[name] = append(out[name], metricRow{Labels: labels, Value: v})
	}
	return out, nil
}

func virshState() map[string]VirshInfo {
	state := map[string]VirshInfo{}
	out, err := exec.Command("virsh", "list", "--all", "--name").Output()
	if err != nil {
		return state
	}
	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		info, err := exec.Command("virsh", "dominfo", name).Output()
		if err != nil {
			continue
		}
		vi := VirshInfo{}
		for _, line := range strings.Split(string(info), "\n") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			k := strings.ToLower(strings.TrimSpace(parts[0]))
			v := strings.TrimSpace(parts[1])
			switch k {
			case "state":
				vi.State = v
			case "max memory":
				fields := strings.Fields(v)
				if len(fields) > 0 {
					if n, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
						vi.MaxMemKiB = n
					}
				}
			case "used memory":
				fields := strings.Fields(v)
				if len(fields) > 0 {
					if n, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
						vi.UsedMemKiB = n
					}
				}
			case "cpu(s)":
				vi.VCPUs = v
			}
		}
		state[name] = vi
	}
	return state
}

func resticFor(vm string) Restic {
	repo := filepath.Join(storageboxDir, vm)
	if _, err := os.Stat(repo); err != nil {
		return Restic{Exists: "no"}
	}
	snapsDir := filepath.Join(repo, "snapshots")
	entries, err := os.ReadDir(snapsDir)
	if err != nil {
		return Restic{Exists: "no-snaps"}
	}
	var latest time.Time
	count := 0
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		count++
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	r := Restic{Exists: "yes", Snapshots: count}
	if count > 0 {
		r.AgeHours = time.Since(latest).Hours()
	}
	// best-effort size
	if out, err := exec.Command("du", "-shx", filepath.Join(repo, "data")).Output(); err == nil {
		r.Size = strings.Fields(string(out))[0]
	}
	return r
}

func collectVM(vm VM) VMStat {
	st := VMStat{VM: vm}
	metrics, err := fetchMetrics(vm.IP)
	if err == nil {
		st.Reachable = true
		// probes
		codes := map[string]int{}
		for _, row := range metrics["health_probe_http_code"] {
			key := row.Labels["name"] + "|" + row.Labels["url"]
			codes[key] = int(row.Value)
		}
		for _, row := range metrics["health_probe_up"] {
			key := row.Labels["name"] + "|" + row.Labels["url"]
			st.Probes = append(st.Probes, Probe{
				Name:     row.Labels["name"],
				URL:      row.Labels["url"],
				Up:       row.Value == 1,
				HTTPCode: codes[key],
			})
		}
		sort.Slice(st.Probes, func(i, j int) bool { return st.Probes[i].Name < st.Probes[j].Name })
		if rs := metrics["node_memory_MemTotal_bytes"]; len(rs) > 0 {
			st.MemTotal = int64(rs[0].Value)
		}
		if rs := metrics["node_memory_MemAvailable_bytes"]; len(rs) > 0 {
			st.MemUsed = st.MemTotal - int64(rs[0].Value)
		}
	}
	st.Restic = resticFor(vm.Name)
	return st
}

func collectAll() ([]VMStat, map[string]VirshInfo) {
	virsh := virshState()
	var wg sync.WaitGroup
	results := make([]VMStat, len(inventory))
	for i, vm := range inventory {
		wg.Add(1)
		go func(i int, vm VM) {
			defer wg.Done()
			results[i] = collectVM(vm)
			results[i].Virsh = virsh[vm.Name]
		}(i, vm)
	}
	wg.Wait()
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	return results, virsh
}

// --------- SVG network flow ----------

// flowEntry describes one DNS-fronted (or tailscale-fronted, or VM-only)
// path through the medano network. Hard-coded because the mappings live in
// medano's nginx vhost configs and would otherwise need scraping.
type flowEntry struct {
	DNS     string // public DNS name, "" for VM-only
	PubIP   string // public IP / tailscale IP, "" if N/A
	Iface   string // eno1, tailscale0, internal
	TLS     bool   // https terminated at medano nginx?
	Nginx   bool   // does this path traverse the medano nginx?
	Bridge  string // virbr0/virbr1/virbr2, or "" for host-local
	VM      string // VM name (must match a VMStat.Name) or "" if host-local
	Service string // backend service name
	Port    string // backend port (with /proto if useful)
	Notes   string // small annotation, optional
}

func buildSVG(vms []VMStat) template.HTML {
	// ---- hard-coded medano flow table ----
	flows := []flowEntry{
		{DNS: "medano.emile.space", PubIP: "95.217.35.60", Iface: "eno1", TLS: true, Nginx: true, Bridge: "virbr0", VM: "naraj", Service: "nginx", Port: "80", Notes: "static"},
		{DNS: "tmp.medano.emile.space", PubIP: "95.217.35.60", Iface: "eno1", TLS: true, Nginx: true, Bridge: "virbr0", VM: "tmp", Service: "nginx", Port: "80", Notes: "autoindex"},
		{DNS: "md.medano.emile.space", PubIP: "95.217.35.60", Iface: "eno1", TLS: true, Nginx: true, Bridge: "virbr0", VM: "md", Service: "hedgedoc", Port: "9091"},
		{DNS: "auth.medano.emile.space", PubIP: "95.217.35.60", Iface: "eno1", TLS: true, Nginx: true, Bridge: "virbr0", VM: "auth", Service: "authelia", Port: "9091"},
		{DNS: "photo.medano.emile.space", PubIP: "95.217.35.60", Iface: "eno1", TLS: true, Nginx: true, Bridge: "virbr0", VM: "photo", Service: "immich", Port: "9091"},
		{DNS: "amaltheea.medano.emile.space", PubIP: "95.217.35.60", Iface: "eno1", TLS: true, Nginx: true, Bridge: "virbr0", VM: "amalthea", Service: "backend", Port: "8080"},
		{DNS: "status.medano.emile.space", PubIP: "95.217.35.60", Iface: "eno1", TLS: true, Nginx: true, Bridge: "", VM: "", Service: "status-board", Port: "8090", Notes: "127.0.0.1"},
		{DNS: "rss.pinto-pike.ts.net", PubIP: "100.x", Iface: "tailscale0", TLS: false, Nginx: false, Bridge: "virbr0", VM: "rss", Service: "miniflux", Port: "8080"},
		{DNS: "data.pinto-pike.ts.net", PubIP: "100.x", Iface: "tailscale0", TLS: false, Nginx: false, Bridge: "virbr0", VM: "data", Service: "sftpgo", Port: "8080/22"},
		{DNS: "arr (no DNS)", PubIP: "wg0", Iface: "wg0", TLS: false, Nginx: false, Bridge: "virbr1", VM: "arr", Service: "jellyfin", Port: "8096", Notes: "via rou"},
	}

	// VM-only entries (no DNS hostname). These appear as VM-layer boxes
	// with a small "internal" tag, dangling off their bridge.
	internalOnly := map[string]bool{
		"miki": true, "late": true, "social": true,
		"demo01": true, "sb1": true, "sb2": true, "sb3": true,
	}

	// Index VMStat by name so we can colour the VM boxes.
	vmByName := map[string]VMStat{}
	bridgeOf := map[string]string{}
	for _, v := range vms {
		vmByName[v.Name] = v
		if v.Bridge != "" {
			bridgeOf[v.Name] = "virbr" + v.Bridge
		}
	}

	vmCls := func(name string) string {
		v, ok := vmByName[name]
		if !ok {
			return "box-vm-warn"
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
				return "box-vm-ok"
			}
			return "box-vm-bad"
		}
		if v.Reachable {
			return "box-vm-warn"
		}
		return "box-vm-bad"
	}

	// ---- collect layer items ----
	// Layer 1: DNS hostnames (from flows where DNS != "")
	// Layer 2: public IPs / tailscale IPs (distinct)
	// Layer 3: interfaces (eno1, tailscale0, wg0, internal)
	// Layer 4: nginx vhost markers (one per flow that goes through nginx) + bypass marker
	// Layer 5: bridges (virbr0, virbr1, virbr2) + "host" pseudo-bridge
	// Layer 6: VMs (from flows + internal-only)
	// Layer 7 (tucked next to VM): service:port

	// We'll collapse layer 7 into the VM box (two lines: name/IP + service:port)
	// so we still have six logical layers as requested.

	// Distinct values per layer in stable order:
	type strset struct {
		order []string
		seen  map[string]bool
	}
	add := func(s *strset, v string) {
		if v == "" || s.seen[v] {
			return
		}
		if s.seen == nil {
			s.seen = map[string]bool{}
		}
		s.seen[v] = true
		s.order = append(s.order, v)
	}

	var dnsSet, pubIPSet, ifaceSet, vhostSet, bridgeSet, vmSet strset
	for _, f := range flows {
		add(&dnsSet, f.DNS)
		add(&pubIPSet, f.PubIP)
		add(&ifaceSet, f.Iface)
		if f.Nginx {
			add(&vhostSet, f.DNS) // one nginx vhost node per flow
		}
		if f.Bridge != "" {
			add(&bridgeSet, f.Bridge)
		} else {
			add(&bridgeSet, "host")
		}
		if f.VM != "" {
			add(&vmSet, f.VM)
		} else {
			add(&vmSet, "(host) "+f.Service)
		}
	}
	// add internal-only VMs to the VM layer
	for _, v := range vms {
		if internalOnly[v.Name] {
			add(&vmSet, v.Name)
			b := "virbr" + v.Bridge
			if v.Bridge == "" {
				b = "host"
			}
			add(&bridgeSet, b)
		}
	}
	// also include "bypass" marker in vhost layer for non-nginx flows so the
	// layer isn't empty for tailscale paths.
	hasBypass := false
	for _, f := range flows {
		if !f.Nginx {
			hasBypass = true
			break
		}
	}
	if hasBypass {
		add(&vhostSet, "(bypass nginx)")
	}

	// ---- layout ----
	const (
		layerCount = 6
		colW       = 220 // width of each layer column
		colGap     = 30  // gap between columns
		boxW       = 190
		boxH       = 38
		vboxH      = 50 // taller for VM boxes (two lines + service)
		rowH       = 18 // vertical spacing between rows within a layer
		topPad     = 70
		leftPad    = 30
		bottomPad  = 30
	)
	layerNames := []string{"DNS hostname", "public IP", "interface", "vhost / bypass", "bridge", "VM + service"}
	layerSets := []*strset{&dnsSet, &pubIPSet, &ifaceSet, &vhostSet, &bridgeSet, &vmSet}

	// compute row heights so each layer is vertically distributed
	maxRows := 1
	for _, s := range layerSets {
		if len(s.order) > maxRows {
			maxRows = len(s.order)
		}
	}
	// Use a fixed slot height; each layer centers its items vertically.
	const slotH = 56
	innerH := maxRows * slotH
	if innerH < 240 {
		innerH = 240
	}
	if innerH > 900 {
		innerH = 900
	}
	totalH := topPad + innerH + bottomPad
	totalW := leftPad + layerCount*colW + (layerCount-1)*colGap + leftPad

	// position helpers
	colX := func(layer int) int {
		return leftPad + layer*(colW+colGap)
	}
	rowY := func(layer, idx int) int {
		n := len(layerSets[layer].order)
		if n == 0 {
			return topPad + innerH/2
		}
		// distribute n items across innerH
		step := innerH / (n + 1)
		return topPad + (idx+1)*step
	}

	// box renderer
	esc := html.EscapeString

	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg viewBox="0 0 %d %d" width="%d" height="%d" xmlns="http://www.w3.org/2000/svg" style="min-width:%dpx">`, totalW, totalH, totalW, totalH, totalW)
	sb.WriteString(`<defs><style>
text { font-family: -apple-system, system-ui, sans-serif; font-size: 12px; fill: #eee; }
text.mono { font-family: ui-monospace, Menlo, monospace; }
.dim { fill: #888; font-size: 10px; }
.layer-title { fill: #6aa9ff; font-size: 11px; letter-spacing: .5px; text-transform: uppercase; }
.box-host { fill: #1f3d5a; stroke: #4d9fff; stroke-width: 1; }
.box-iface { fill: #1a2c45; stroke: #6aa9ff; stroke-width: 1; }
.box-dns { fill: #20283a; stroke: #88a; stroke-width: 1; }
.box-vhost { fill: #2a2138; stroke: #b07cff; stroke-width: 1; }
.box-vhost-bypass { fill: #2a2a2a; stroke: #888; stroke-dasharray: 3 2; stroke-width: 1; }
.box-bridge { fill: #2a2a2a; stroke: #888; stroke-width: 1; }
.box-vm-ok { fill: #182b1c; stroke: #2ea043; stroke-width: 1; }
.box-vm-bad { fill: #2b1818; stroke: #f85149; stroke-width: 1; }
.box-vm-warn { fill: #2a2418; stroke: #d29922; stroke-width: 1; }
.box-host-svc { fill: #182233; stroke: #4d9fff; stroke-dasharray: 3 2; stroke-width: 1; }
.edge { stroke: #444; stroke-width: 1; fill: none; }
.edge-tls { stroke: #6aa9ff; stroke-width: 1.2; fill: none; }
.edge-bypass { stroke: #b07cff; stroke-width: 1.2; stroke-dasharray: 4 2; fill: none; }
.tag { font-size: 9px; fill: #b07cff; }
.tag-tls { font-size: 9px; fill: #2ea043; }
.tag-int { font-size: 9px; fill: #d29922; }
.legend-bg { fill: #181818; stroke: #333; }
</style>
<marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto">
  <path d="M0,0 L10,5 L0,10 z" fill="#444"/>
</marker>
<marker id="arrow-tls" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto">
  <path d="M0,0 L10,5 L0,10 z" fill="#6aa9ff"/>
</marker>
<marker id="arrow-bypass" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto">
  <path d="M0,0 L10,5 L0,10 z" fill="#b07cff"/>
</marker>
</defs>`)

	// background
	fmt.Fprintf(&sb, `<rect x="0" y="0" width="%d" height="%d" fill="#141414"/>`, totalW, totalH)

	// layer headers
	for i, name := range layerNames {
		x := colX(i) + boxW/2
		fmt.Fprintf(&sb, `<text class="layer-title" x="%d" y="%d" text-anchor="middle">%d. %s</text>`, x, 30, i+1, esc(name))
		// faint column divider
		fmt.Fprintf(&sb, `<line x1="%d" y1="40" x2="%d" y2="%d" stroke="#1f1f1f" stroke-width="1"/>`, x, x, totalH-bottomPad+10)
	}

	// position lookups
	pos := make([]map[string]struct{ x, y int }, layerCount)
	for li := 0; li < layerCount; li++ {
		pos[li] = map[string]struct{ x, y int }{}
		for i, item := range layerSets[li].order {
			pos[li][item] = struct{ x, y int }{colX(li), rowY(li, i)}
		}
	}

	// draw boxes
	drawBox := func(li int, item string) {
		p := pos[li][item]
		x, y := p.x, p.y-boxH/2
		h := boxH
		switch li {
		case 0: // DNS
			fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="%d" height="%d" rx="4" class="box-dns"/>`, x, y, boxW, h)
			fmt.Fprintf(&sb, `<text class="mono" x="%d" y="%d" text-anchor="middle">%s</text>`, x+boxW/2, y+22, esc(item))
		case 1: // pub IP
			fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="%d" height="%d" rx="4" class="box-host"/>`, x, y, boxW, h)
			fmt.Fprintf(&sb, `<text class="mono" x="%d" y="%d" text-anchor="middle">%s</text>`, x+boxW/2, y+22, esc(item))
		case 2: // iface
			fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="%d" height="%d" rx="4" class="box-iface"/>`, x, y, boxW, h)
			fmt.Fprintf(&sb, `<text x="%d" y="%d" text-anchor="middle">%s</text>`, x+boxW/2, y+22, esc(item))
		case 3: // vhost
			cls := "box-vhost"
			if item == "(bypass nginx)" {
				cls = "box-vhost-bypass"
			}
			fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="%d" height="%d" rx="4" class="%s"/>`, x, y, boxW, h, cls)
			label := item
			if item != "(bypass nginx)" {
				label = "nginx: " + item
			}
			fmt.Fprintf(&sb, `<text class="mono" x="%d" y="%d" text-anchor="middle">%s</text>`, x+boxW/2, y+16, esc(label))
			if item != "(bypass nginx)" {
				fmt.Fprintf(&sb, `<text class="tag-tls" x="%d" y="%d" text-anchor="middle">TLS</text>`, x+boxW/2, y+30)
			}
		case 4: // bridge
			fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="%d" height="%d" rx="4" class="box-bridge"/>`, x, y, boxW, h)
			fmt.Fprintf(&sb, `<text x="%d" y="%d" text-anchor="middle">%s</text>`, x+boxW/2, y+22, esc(item))
		case 5: // VM + service
			// look up the flow(s) for this VM to pull service:port
			var svc, port, ip string
			isHost := strings.HasPrefix(item, "(host) ")
			for _, f := range flows {
				if f.VM == item || (isHost && f.VM == "" && "(host) "+f.Service == item) {
					svc = f.Service
					port = f.Port
				}
			}
			if v, ok := vmByName[item]; ok {
				ip = v.IP
			}
			cls := vmCls(item)
			if isHost {
				cls = "box-host-svc"
			}
			h = vboxH
			y = p.y - h/2
			fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="%d" height="%d" rx="4" class="%s"/>`, x, y, boxW, h, cls)
			fmt.Fprintf(&sb, `<text x="%d" y="%d" text-anchor="middle">%s</text>`, x+boxW/2, y+14, esc(strings.TrimPrefix(item, "(host) ")))
			if ip != "" {
				fmt.Fprintf(&sb, `<text class="mono dim" x="%d" y="%d" text-anchor="middle">%s</text>`, x+boxW/2, y+28, esc(ip))
			}
			if svc != "" {
				fmt.Fprintf(&sb, `<text class="mono" x="%d" y="%d" text-anchor="middle" style="font-size:10px;fill:#aac">%s:%s</text>`, x+boxW/2, y+42, esc(svc), esc(port))
			} else if internalOnly[item] {
				fmt.Fprintf(&sb, `<text class="tag-int" x="%d" y="%d" text-anchor="middle">internal</text>`, x+boxW/2, y+42)
			}
		}
	}

	for li := 0; li < layerCount; li++ {
		for _, item := range layerSets[li].order {
			drawBox(li, item)
		}
	}

	// draw edges by walking each flow through the six layers
	edge := func(li int, from, to string, style string) {
		pf, ok1 := pos[li][from]
		pt, ok2 := pos[li+1][to]
		if !ok1 || !ok2 {
			return
		}
		x1 := pf.x + boxW
		y1 := pf.y
		x2 := pt.x
		y2 := pt.y
		// cubic bezier between layers for a clean fan-out
		c1x := x1 + (x2-x1)/2
		c2x := x2 - (x2-x1)/2
		cls := "edge"
		marker := "arrow"
		switch style {
		case "tls":
			cls = "edge-tls"
			marker = "arrow-tls"
		case "bypass":
			cls = "edge-bypass"
			marker = "arrow-bypass"
		}
		fmt.Fprintf(&sb, `<path d="M%d,%d C%d,%d %d,%d %d,%d" class="%s" marker-end="url(#%s)"/>`, x1, y1, c1x, y1, c2x, y2, x2, y2, cls, marker)
	}

	for _, f := range flows {
		style := "default"
		if f.TLS {
			style = "tls"
		}
		if !f.Nginx {
			style = "bypass"
		}
		// layer 0->1
		edge(0, f.DNS, f.PubIP, style)
		// layer 1->2
		edge(1, f.PubIP, f.Iface, style)
		// layer 2->3
		var vh string
		if f.Nginx {
			vh = f.DNS
		} else {
			vh = "(bypass nginx)"
		}
		edge(2, f.Iface, vh, style)
		// layer 3->4
		br := f.Bridge
		if br == "" {
			br = "host"
		}
		edge(3, vh, br, style)
		// layer 4->5
		vm := f.VM
		if vm == "" {
			vm = "(host) " + f.Service
		}
		edge(4, br, vm, style)
	}
	// internal-only VMs: draw a single edge from their bridge so they don't float
	for _, v := range vms {
		if !internalOnly[v.Name] {
			continue
		}
		br := "virbr" + v.Bridge
		if v.Bridge == "" {
			br = "host"
		}
		edge(4, br, v.Name, "default")
	}

	// ---- legend ----
	lx := totalW - 220
	ly := totalH - 110
	fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="200" height="92" rx="4" class="legend-bg"/>`, lx, ly)
	fmt.Fprintf(&sb, `<text x="%d" y="%d" class="layer-title">legend</text>`, lx+10, ly+16)
	// VM colour swatches
	fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="14" height="10" class="box-vm-ok"/>`, lx+10, ly+24)
	fmt.Fprintf(&sb, `<text x="%d" y="%d">all probes up</text>`, lx+30, ly+33)
	fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="14" height="10" class="box-vm-warn"/>`, lx+10, ly+38)
	fmt.Fprintf(&sb, `<text x="%d" y="%d">reachable, no probes</text>`, lx+30, ly+47)
	fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="14" height="10" class="box-vm-bad"/>`, lx+10, ly+52)
	fmt.Fprintf(&sb, `<text x="%d" y="%d">unreachable / probe fail</text>`, lx+30, ly+61)
	// edge styles
	fmt.Fprintf(&sb, `<line x1="%d" y1="%d" x2="%d" y2="%d" class="edge-tls"/>`, lx+10, ly+72, lx+24, ly+72)
	fmt.Fprintf(&sb, `<text x="%d" y="%d">TLS via medano nginx</text>`, lx+30, ly+75)
	fmt.Fprintf(&sb, `<line x1="%d" y1="%d" x2="%d" y2="%d" class="edge-bypass"/>`, lx+10, ly+84, lx+24, ly+84)
	fmt.Fprintf(&sb, `<text x="%d" y="%d">tailscale / bypass</text>`, lx+30, ly+87)

	sb.WriteString(`</svg>`)
	return template.HTML(sb.String())
}

// --------- template ----------

var page = template.Must(template.New("page").Funcs(template.FuncMap{
	"div": func(a, b float64) float64 { return a / b },
	"mb":  func(b int64) float64 { return float64(b) / 1024 / 1024 },
	"gb":  func(b int64) float64 { return float64(b) / 1024 / 1024 / 1024 },
	"kib2gb": func(k int64) float64 { return float64(k) / 1024 / 1024 },
	"resticCls": func(r Restic) string {
		if r.Exists == "no" {
			return "bad"
		}
		if r.AgeHours == 0 {
			return "warn"
		}
		if r.AgeHours > 48 {
			return "bad"
		}
		if r.AgeHours > 26 {
			return "warn"
		}
		return "ok"
	},
	"resticText": func(r Restic) string {
		if r.Exists == "no" {
			return "missing"
		}
		if r.AgeHours == 0 {
			return "no snapshots yet"
		}
		return fmt.Sprintf("%d snaps, %.1fh old", r.Snapshots, r.AgeHours)
	},
	"probeCls": func(p Probe) string {
		if p.Up {
			return "ok"
		}
		return "bad"
	},
	"reachCls": func(b bool) string {
		if b {
			return "ok"
		}
		return "bad"
	},
}).Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>medano fleet</title>
<style>
  body { font-family: -apple-system, sans-serif; background: #111; color: #eee; padding: 1.5em; margin: 0; }
  h1 { margin: 0 0 .5em; font-weight: 400; }
  h2 { margin: 1.5em 0 .5em; font-weight: 400; color: #aaa; font-size: 1.1em; }
  table { border-collapse: collapse; width: 100%; font-size: 13px; }
  th, td { text-align: left; padding: 6px 10px; border-bottom: 1px solid #333; vertical-align: top; }
  th { color: #888; font-weight: 500; }
  td.name { font-weight: 600; }
  td.ip { font-family: monospace; color: #88a; }
  .dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; vertical-align: middle; margin-right: 4px; }
  .ok    { background: #2ea043; color: #2ea043 !important; }
  .warn  { background: #d29922; color: #d29922 !important; }
  .bad   { background: #f85149; color: #f85149 !important; }
  .unknown { background: #666; color: #aaa !important; }
  .probe { display: inline-block; padding: 2px 6px; margin: 1px 2px 1px 0; border-radius: 3px; font-size: 11px; background: #222; }
  .probe.ok   { color: #2ea043; }
  .probe.bad  { color: #f85149; }
  td.restic { font-family: monospace; font-size: 12px; }
  td.restic.ok   { color: #2ea043; }
  td.restic.warn { color: #d29922; }
  td.restic.bad  { color: #f85149; }
  .footer { color: #555; font-size: 11px; margin-top: 1em; }
  svg { background: #181818; border-radius: 4px; }
</style></head>
<body>
  <h1>medano fleet</h1>
  <p style="color:#888;font-size:13px;">live snapshot, scraped at {{ .Now }} — refresh in 60s</p>
  <h2>traffic flow</h2>
  {{ .SVG }}
  <h2>vm inventory ({{ len .VMs }} VMs)</h2>
  <table>
    <thead><tr><th>name</th><th>bridge</th><th>ip</th><th>libvirt</th><th>memory</th><th>probes</th><th>last backup</th></tr></thead>
    <tbody>
    {{- range .VMs }}
      <tr>
        <td class="name">{{ .Name }}</td>
        <td>{{ .Bridge }}</td>
        <td class="ip">{{ .IP }}</td>
        <td>{{ .Virsh.State }}</td>
        <td>{{ printf "%.1fG alloc" (kib2gb .Virsh.MaxMemKiB) }}<br>{{ printf "%.1fG used" (gb .MemUsed) }}</td>
        <td>
          <span class="dot {{ reachCls .Reachable }}"></span>
          {{- if .Probes -}}
            {{- range .Probes }}
              <span class="probe {{ probeCls . }}" title="{{ .URL }}">{{ .Name }} {{ .HTTPCode }}</span>
            {{- end -}}
          {{- else -}}
            <span class="probe">no probes</span>
          {{- end -}}
        </td>
        <td class="restic {{ resticCls .Restic }}">{{ resticText .Restic }}</td>
      </tr>
    {{- end }}
    </tbody>
  </table>
  <div class="footer">data: virsh + each VM's node-exporter:9100 + storagebox restic dirs</div>
<script>setTimeout(()=>location.reload(), 60000);</script>
</body></html>`))

func handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}
	vms, _ := collectAll()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page.Execute(w, struct {
		Now string
		SVG template.HTML
		VMs []VMStat
	}{
		Now: time.Now().Format("2006-01-02 15:04:05"),
		SVG: buildSVG(vms),
		VMs: vms,
	})
}

func main() {
	if env := os.Getenv("STATUS_BOARD_LISTEN"); env != "" {
		listenAddr = env
	}
	if env := os.Getenv("STATUS_BOARD_STORAGEBOX"); env != "" {
		storageboxDir = env
	}
	if err := loadInventory(); err != nil {
		log.Fatalf("inventory: %v", err)
	}
	log.Printf("status-board listening on %s (inventory: %d VMs)", listenAddr, len(inventory))
	http.HandleFunc("/", handle)
	log.Fatal(http.ListenAndServe(listenAddr, nil))
}
