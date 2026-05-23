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

func buildSVG(vms []VMStat) template.HTML {
	byBridge := map[string][]VMStat{}
	for _, vm := range vms {
		byBridge[vm.Bridge] = append(byBridge[vm.Bridge], vm)
	}
	bridges := make([]string, 0, len(byBridge))
	for k := range byBridge {
		bridges = append(bridges, k)
	}
	sort.Strings(bridges)
	for _, b := range bridges {
		sort.Slice(byBridge[b], func(i, j int) bool { return byBridge[b][i].Name < byBridge[b][j].Name })
	}

	const W = 1100
	const LH = 60
	const PAD = 20
	maxPer := 1
	for _, b := range byBridge {
		if len(b) > maxPer {
			maxPer = len(b)
		}
	}
	H := 240 + maxPer*LH + 40

	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg viewBox="0 0 %d %d" width="100%%" height="%d" xmlns="http://www.w3.org/2000/svg">`, W, H, H)
	sb.WriteString(`<defs><style>
text { font-family: -apple-system, sans-serif; font-size: 12px; fill: #eee; }
.dim { fill: #888; font-size: 10px; }
.box-host { fill: #1f3d5a; stroke: #4d9fff; stroke-width: 1; }
.box-bridge { fill: #2a2a2a; stroke: #666; stroke-width: 1; }
.box-vm-ok { fill: #182b1c; stroke: #2ea043; stroke-width: 1; }
.box-vm-bad { fill: #2b1818; stroke: #f85149; stroke-width: 1; }
.box-vm-warn { fill: #2a2418; stroke: #d29922; stroke-width: 1; }
.edge { stroke: #555; stroke-width: 1; fill: none; }
.edge-active { stroke: #4d9fff; stroke-width: 1.5; fill: none; }
.label-link { fill: #888; font-size: 9px; }
</style>
<marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto">
  <path d="M0,0 L10,5 L0,10 z" fill="#555"/>
</marker>
</defs>`)
	cx := W / 2
	// internet
	fmt.Fprintf(&sb, `<rect x="%d" y="10" width="180" height="36" rx="4" class="box-host"/>`, cx-90)
	fmt.Fprintf(&sb, `<text x="%d" y="33" text-anchor="middle">internet</text>`, cx)
	// arrow down
	fmt.Fprintf(&sb, `<line x1="%d" y1="46" x2="%d" y2="70" class="edge-active" marker-end="url(#arrow)"/>`, cx, cx)
	fmt.Fprintf(&sb, `<text class="label-link" x="%d" y="60">443/tcp</text>`, cx+5)
	// medano
	fmt.Fprintf(&sb, `<rect x="%d" y="74" width="240" height="40" rx="4" class="box-host"/>`, cx-120)
	fmt.Fprintf(&sb, `<text x="%d" y="92" text-anchor="middle">medano (eno1)</text>`, cx)
	fmt.Fprintf(&sb, `<text x="%d" y="106" text-anchor="middle" class="dim">95.217.35.60 — nginx + virsh + nfs</text>`, cx)

	nB := len(bridges)
	bridgeW := 200
	bridgeXs := map[string]int{}
	bridgeY := 150
	for i, b := range bridges {
		var x int
		if nB > 1 {
			spacing := (W - PAD*2 - bridgeW*nB) / (nB - 1)
			x = PAD + i*(bridgeW+spacing)
		} else {
			x = (W - bridgeW) / 2
		}
		bridgeXs[b] = x + bridgeW/2
		fmt.Fprintf(&sb, `<line x1="%d" y1="114" x2="%d" y2="%d" class="edge" marker-end="url(#arrow)"/>`, cx, x+bridgeW/2, bridgeY)
		fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="%d" height="36" rx="4" class="box-bridge"/>`, x, bridgeY, bridgeW)
		fmt.Fprintf(&sb, `<text x="%d" y="%d" text-anchor="middle">virbr %s</text>`, x+bridgeW/2, bridgeY+22, html.EscapeString(b))
	}

	vmTop := bridgeY + 60
	boxW := 130
	boxH := 32
	for _, b := range bridges {
		bx := bridgeXs[b]
		for i, vm := range byBridge[b] {
			y := vmTop + i*LH
			x := bx - boxW/2
			cls := "box-vm-warn"
			if len(vm.Probes) > 0 {
				allUp := true
				for _, p := range vm.Probes {
					if !p.Up {
						allUp = false
						break
					}
				}
				if allUp {
					cls = "box-vm-ok"
				} else {
					cls = "box-vm-bad"
				}
			} else if vm.Reachable {
				cls = "box-vm-warn"
			} else {
				cls = "box-vm-bad"
			}
			fmt.Fprintf(&sb, `<line x1="%d" y1="%d" x2="%d" y2="%d" class="edge"/>`, bx, bridgeY+36, bx, y)
			fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="%d" height="%d" rx="3" class="%s"/>`, x, y, boxW, boxH, cls)
			fmt.Fprintf(&sb, `<text x="%d" y="%d" text-anchor="middle">%s</text>`, bx, y+13, html.EscapeString(vm.Name))
			fmt.Fprintf(&sb, `<text x="%d" y="%d" text-anchor="middle" class="dim">%s</text>`, bx, y+26, html.EscapeString(vm.IP))
		}
	}
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
