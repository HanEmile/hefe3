// NAT topology visualization for the status board.
// Reads /etc/nat-flows.json (written by ops/machines/x86/medano/networking.nix
// from the same natFlows attrset that generates the iptables rules) and
// renders an SVG diagram.
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

// buildNATSVG renders the NAT topology as an SVG. Six columns:
//   Internet | eno1 (pubv4) | PREROUTING | virbr0 (bridge) | backend VM | (legend)
// Plus a "VM subnet" lane that joins at PREROUTING for hairpin flows.
func buildNATSVG() template.HTML {
	c, err := loadNATConfig()
	if err != nil {
		return template.HTML(fmt.Sprintf(
			`<div class="sb-banner bad">NAT config unavailable: %s</div>`,
			template.HTMLEscapeString(err.Error())))
	}

	const (
		colW     = 150
		rowH     = 46
		topPad   = 40
		botPad   = 16
		boxH     = 28
		boxR     = 4
		fontSize = 11
	)
	// columns: 0=Internet, 1=eno1, 2=PREROUTING, 3=virbr0, 4=backend
	colX := []int{30, 200, 380, 580, 780}
	colLabels := []string{"Internet", c.ExternalIface + "\n" + c.PublicIp, "PREROUTING (DNAT)", c.Bridge, "backend VM"}

	rows := len(c.Flows)
	if rows < 1 {
		rows = 1
	}
	// extra row for the hairpin subnet lane
	totalH := topPad + (rows+1)*rowH + botPad
	totalW := colX[len(colX)-1] + colW + 30

	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg viewBox="0 0 %d %d" width="%d" height="%d" xmlns="http://www.w3.org/2000/svg" style="min-width:%dpx; background:#181818; border-radius:4px;">`,
		totalW, totalH, totalW, totalH, totalW)

	// column headers
	for i, label := range colLabels {
		x := colX[i] + colW/2
		for li, line := range strings.Split(label, "\n") {
			fmt.Fprintf(&sb, `<text x="%d" y="%d" text-anchor="middle" font-family="monospace" font-size="%d" fill="#ddd">%s</text>`,
				x, 18+li*13, fontSize+1, template.HTMLEscapeString(line))
		}
	}

	// VM subnet lane (bottom)
	subnetY := topPad + rows*rowH + rowH/2
	fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="%d" height="%d" rx="%d" fill="#1f3030" stroke="#3a6a6a"/>`,
		colX[0], subnetY-boxH/2, colW, boxH, boxR)
	fmt.Fprintf(&sb, `<text x="%d" y="%d" text-anchor="middle" font-family="monospace" font-size="%d" fill="#9cd">%s</text>`,
		colX[0]+colW/2, subnetY+4, fontSize, template.HTMLEscapeString("VMs ("+c.VmSubnet+")"))

	// render each flow as a row
	for i, f := range c.Flows {
		y := topPad + i*rowH + rowH/2

		destParts := strings.SplitN(f.Dest, ":", 2)
		destIp := destParts[0]
		destPort := ""
		if len(destParts) > 1 {
			destPort = destParts[1]
		}

		// boxes per column, with the flow label inside
		drawBox := func(cidx int, label, sub string, fill, stroke string) {
			x := colX[cidx]
			fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="%d" height="%d" rx="%d" fill="%s" stroke="%s"/>`,
				x, y-boxH/2, colW, boxH, boxR, fill, stroke)
			fmt.Fprintf(&sb, `<text x="%d" y="%d" text-anchor="middle" font-family="monospace" font-size="%d" fill="#eee">%s</text>`,
				x+colW/2, y-2, fontSize, template.HTMLEscapeString(label))
			if sub != "" {
				fmt.Fprintf(&sb, `<text x="%d" y="%d" text-anchor="middle" font-family="monospace" font-size="%d" fill="#aaa">%s</text>`,
					x+colW/2, y+10, fontSize-1, template.HTMLEscapeString(sub))
			}
		}

		// Internet origin box
		drawBox(0, "client", fmt.Sprintf("→ %s:%d/%s", c.PublicIp, f.Dport, f.Proto), "#222", "#444")
		// eno1
		drawBox(1, c.ExternalIface, c.PublicIp, "#243044", "#3a5176")
		// PREROUTING dnat
		drawBox(2, f.Name, fmt.Sprintf("dport %d → %s", f.Dport, f.Dest), "#1a2e1a", "#3a6a3a")
		// virbr0
		drawBox(3, c.Bridge, "", "#2a2a3a", "#4a4a6a")
		// backend
		drawBox(4, destIp, "port "+destPort, "#3a2a1a", "#7a5a3a")

		// connector arrows
		arrow := func(x1, x2 int, dashed bool, color string) {
			dash := ""
			if dashed {
				dash = ` stroke-dasharray="4,3"`
			}
			fmt.Fprintf(&sb, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="%s" stroke-width="1.4"%s/>`,
				x1, y, x2, y, color, dash)
		}
		for k := 0; k < 4; k++ {
			arrow(colX[k]+colW, colX[k+1], false, "#6c8")
		}
		// hairpin: dashed line from subnet lane up to PREROUTING column for this row
		if f.Hairpin {
			// curve from (colX[0]+colW, subnetY) up to (colX[2], y)
			fmt.Fprintf(&sb, `<path d="M %d %d C %d %d, %d %d, %d %d" stroke="#c84" stroke-width="1.2" stroke-dasharray="3,3" fill="none"/>`,
				colX[0]+colW, subnetY,
				colX[1], subnetY,
				colX[1]+colW/2, y,
				colX[2], y)
		}
		// SNAT return: a thin dashed yellow line going back from backend → eno1
		if f.SnatReturn {
			fmt.Fprintf(&sb, `<path d="M %d %d C %d %d, %d %d, %d %d" stroke="#ca6" stroke-width="1" stroke-dasharray="2,3" fill="none"/>`,
				colX[4], y+boxH/2+2,
				colX[3], y+boxH/2+10,
				colX[2], y+boxH/2+10,
				colX[1]+colW, y+boxH/2+2)
		}

		// flow description (small italic, above row)
		fmt.Fprintf(&sb, `<text x="%d" y="%d" font-family="monospace" font-size="%d" fill="#888" font-style="italic">%s</text>`,
			colX[0], y-boxH/2-3, fontSize-1, template.HTMLEscapeString(f.Desc))
	}

	sb.WriteString(`</svg>`)
	return template.HTML(sb.String())
}
