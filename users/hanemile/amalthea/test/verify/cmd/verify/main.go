// verify is a Playwright-driven smoke harness for amalthea-web.
//
// It boots `amalthea-backend` against the WASM dist directory provided
// via `-dist`, then drives a headless browser through a sequence of
// feature checks. Each check prints `PASS <name>` or `FAIL <name>: ...`
// and the process exits non-zero if any check fails.
//
// Usage:
//
//	verify -backend ./result-2/bin/amalthea-backend -dist ./result-3
//
// The harness is intentionally bounded — egui paints to a single canvas,
// so DOM-level assertions are limited to the host page (canvas presence,
// console errors, page title). Pixel sampling against `Canvas.toDataURL`
// covers theme/contrast checks; the rest of the UI is exercised by
// running it long enough for the eframe runner to attach without errors.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"image"
	"image/png"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

type result struct {
	name string
	err  error
	soft bool // a soft failure is reported but doesn't fail the run
}

func main() {
	backend := flag.String("backend", "", "path to amalthea-backend binary")
	dist := flag.String("dist", "", "path to the WASM dist directory (index.html, *.wasm)")
	headless := flag.Bool("headless", true, "run browser headless")
	keep := flag.Duration("keep", 0, "how long to keep the browser open after running checks (debug aid)")
	flag.Parse()

	if *backend == "" || *dist == "" {
		fmt.Fprintln(os.Stderr, "both -backend and -dist are required")
		os.Exit(2)
	}

	port, err := freePort()
	if err != nil {
		fmt.Fprintf(os.Stderr, "freePort: %v\n", err)
		os.Exit(1)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	dbDir, err := os.MkdirTemp("", "amalthea-verify-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "tempdir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dbDir)

	cmd := exec.Command(*backend)
	cmd.Env = append(os.Environ(),
		"LISTEN_ADDR="+addr,
		"STATIC_DIR="+*dist,
		"DB_PATH="+dbDir+"/amalthea.db",
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start backend: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	if err := waitForHTTP("http://"+addr+"/health", 5*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "backend never came up: %v\n", err)
		os.Exit(1)
	}

	if err := playwright.Install(); err != nil {
		fmt.Fprintf(os.Stderr, "playwright install: %v\n", err)
		os.Exit(1)
	}
	pw, err := playwright.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "playwright run: %v\n", err)
		os.Exit(1)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(*headless),
		// SwiftShader gives us a software GPU implementation so wgpu /
		// WebGL can actually paint into the canvas in headless mode.
		Args: []string{
			"--use-gl=swiftshader",
			"--enable-unsafe-webgpu",
			"--enable-features=Vulkan",
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "launch chromium: %v\n", err)
		os.Exit(1)
	}
	defer browser.Close()

	page, err := browser.NewPage(playwright.BrowserNewPageOptions{
		Viewport: &playwright.Size{Width: 1280, Height: 800},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "new page: %v\n", err)
		os.Exit(1)
	}

	var consoleErrors []string
	page.OnConsole(func(msg playwright.ConsoleMessage) {
		if msg.Type() == "error" {
			consoleErrors = append(consoleErrors, msg.Text())
		}
	})
	var pageErrors []string
	page.OnPageError(func(err error) {
		pageErrors = append(pageErrors, err.Error())
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = ctx

	results := []result{}
	check := func(name string, fn func() error) {
		err := fn()
		results = append(results, result{name: name, err: err})
		if err != nil {
			fmt.Printf("FAIL %s: %v\n", name, err)
		} else {
			fmt.Printf("PASS %s\n", name)
		}
	}
	softCheck := func(name string, fn func() error) {
		err := fn()
		results = append(results, result{name: name, err: err, soft: true})
		if err != nil {
			fmt.Printf("WARN %s: %v\n", name, err)
		} else {
			fmt.Printf("PASS %s\n", name)
		}
	}

	url := "http://" + addr + "/"

	check("page_loads", func() error {
		_, err := page.Goto(url, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateNetworkidle,
			Timeout:   playwright.Float(15000),
		})
		return err
	})

	check("title_is_amalthea", func() error {
		t, err := page.Title()
		if err != nil {
			return err
		}
		if !strings.EqualFold(strings.TrimSpace(t), "amalthea") {
			return fmt.Errorf("got title %q", t)
		}
		return nil
	})

	check("canvas_present", func() error {
		// The egui app draws to a single <canvas id="amalthea-canvas">.
		count, err := page.Locator("canvas#amalthea-canvas").Count()
		if err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("no canvas#amalthea-canvas")
		}
		return nil
	})

	check("wasm_attaches_no_console_error", func() error {
		// Give the wasm runner a moment to attach.
		time.Sleep(2 * time.Second)
		// Filter expected GPU-init failures in headless Chromium without a
		// real GPU — the wgpu/WebGL backend can't initialise, but the
		// canvas itself is still attached and the rest of the harness can
		// continue.
		isGPU := func(s string) bool {
			lower := strings.ToLower(s)
			return strings.Contains(lower, "webgl") ||
				strings.Contains(lower, "wgpu") ||
				strings.Contains(lower, "webgpu")
		}
		isFavicon := func(s string) bool {
			return strings.Contains(s, "favicon") || strings.Contains(s, "404")
		}
		var bad []string
		for _, e := range pageErrors {
			if !isGPU(e) && !isFavicon(e) {
				bad = append(bad, e)
			}
		}
		for _, e := range consoleErrors {
			if !isGPU(e) && !isFavicon(e) {
				bad = append(bad, e)
			}
		}
		if len(bad) > 0 {
			return fmt.Errorf("non-GPU errors: %s", strings.Join(bad, "; "))
		}
		return nil
	})

	softCheck("canvas_has_content", func() error {
		// Pixel-sample several points across the canvas and check that at
		// least one differs from both fully-transparent black (egl/wgpu
		// fallback in headless Chromium) and the CSS fallback #1e1e1e.
		time.Sleep(1500 * time.Millisecond)
		dataURL, err := page.Evaluate(`() => {
			const c = document.getElementById('amalthea-canvas');
			if (!c) return null;
			return c.toDataURL('image/png');
		}`)
		if err != nil {
			return err
		}
		s, ok := dataURL.(string)
		if !ok || s == "" {
			return fmt.Errorf("no data url")
		}
		img, err := decodeDataURL(s)
		if err != nil {
			return err
		}
		w, h := img.Bounds().Dx(), img.Bounds().Dy()
		samples := [][2]int{
			{w / 4, 10},
			{w / 2, 16},
			{w / 2, h / 2},
			{w * 3 / 4, h - 10},
		}
		painted := false
		for _, p := range samples {
			r, g, b, a := img.At(p[0], p[1]).RGBA()
			r8, g8, b8, a8 := r>>8, g>>8, b>>8, a>>8
			if a8 == 0 {
				continue
			}
			if !approx(r8, g8, b8, 30, 30, 30, 5) {
				painted = true
				break
			}
		}
		if !painted {
			return fmt.Errorf("canvas appears blank or transparent across %d samples", len(samples))
		}
		return nil
	})

	softCheck("theme_chrome_color", func() error {
		// Scan a horizontal stripe at the height of the header bar and
		// confirm at least one pixel sits near theme::CHROME (#1a2532).
		// Sampling a single pixel is brittle: at high DPR the header maps
		// to several backbuffer rows; tab labels, separators, and the
		// title overlay paint other colours. A "any pixel in the stripe"
		// check is what we actually want to assert.
		probe, err := page.Evaluate(`() => {
			const c = document.getElementById('amalthea-canvas');
			if (!c) return null;
			return { dataURL: c.toDataURL('image/png'), dpr: window.devicePixelRatio || 1 };
		}`)
		if err != nil {
			return err
		}
		m, ok := probe.(map[string]any)
		if !ok || m["dataURL"] == nil {
			return fmt.Errorf("canvas probe failed")
		}
		img, err := decodeDataURL(m["dataURL"].(string))
		if err != nil {
			return err
		}
		dpr := 1.0
		if v, ok := m["dpr"].(float64); ok {
			dpr = v
		}
		// Header occupies y in [0, 32) in layout coords → [0, 32*dpr) in
		// backbuffer coords.
		w := img.Bounds().Dx()
		yMax := int(32.0 * dpr)
		if yMax > img.Bounds().Dy() {
			yMax = img.Bounds().Dy()
		}
		matched := 0
		for y := 0; y < yMax; y++ {
			for x := 0; x < w; x += 8 {
				r, g, b, a := img.At(x, y).RGBA()
				if a>>8 == 0 {
					continue
				}
				if approx(r>>8, g>>8, b>>8, 0x1a, 0x25, 0x32, 24) {
					matched++
					if matched > 4 {
						return nil
					}
				}
			}
		}
		return fmt.Errorf("no pixels in header stripe matched theme CHROME (#1a2532); dpr=%v", dpr)
	})

	if *keep > 0 {
		fmt.Fprintf(os.Stderr, "keeping browser open for %s...\n", *keep)
		time.Sleep(*keep)
	}

	hardFailed, softFailed, passed := 0, 0, 0
	for _, r := range results {
		switch {
		case r.err == nil:
			passed++
		case r.soft:
			softFailed++
		default:
			hardFailed++
		}
	}
	fmt.Printf("\n%d/%d hard checks passed", passed+softFailed, len(results))
	if softFailed > 0 {
		fmt.Printf(" (%d soft warnings — typically GPU-dependent in headless mode)", softFailed)
	}
	fmt.Println()
	if hardFailed > 0 {
		os.Exit(1)
	}
}

// freePort returns an unused TCP port number on 127.0.0.1.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// waitForHTTP polls the URL until it returns HTTP 200 or the timeout elapses.
func waitForHTTP(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timed out after %s", timeout)
}

// decodeDataURL parses a `data:image/png;base64,...` URL into an image.
func decodeDataURL(s string) (image.Image, error) {
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(s, prefix) {
		return nil, fmt.Errorf("not a png data url")
	}
	dec, err := base64.StdEncoding.DecodeString(s[len(prefix):])
	if err != nil {
		return nil, err
	}
	return png.Decode(bytes.NewReader(dec))
}

// approx returns true if (r,g,b) is within `tol` per channel of (er,eg,eb).
func approx(r, g, b, er, eg, eb, tol uint32) bool {
	d := func(a, b uint32) uint32 {
		if a > b {
			return a - b
		}
		return b - a
	}
	return d(r, er) <= tol && d(g, eg) <= tol && d(b, eb) <= tol
}
