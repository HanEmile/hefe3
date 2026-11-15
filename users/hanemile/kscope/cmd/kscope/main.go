// cmd/kscope/main.go — KScope entry point. Parses flags, validates the
// kernel source root, and starts the HTTP server.
//
// Part of github.com/hanemile/kscope
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"

	"github.com/hanemile/kscope/internal/server"
)

const banner = `
  ██╗  ██╗███████╗ ██████╗ ██████╗ ██████╗ ███████╗
  ██║ ██╔╝██╔════╝██╔════╝██╔═══██╗██╔══██╗██╔════╝
  █████╔╝ ███████╗██║     ██║   ██║██████╔╝█████╗
  ██╔═██╗ ╚════██║██║     ██║   ██║██╔═══╝ ██╔══╝
  ██║  ██╗███████║╚██████╗╚██████╔╝██║     ███████╗
  ╚═╝  ╚═╝╚══════╝ ╚═════╝ ╚═════╝ ╚═╝     ╚══════╝
  Linux kernel source browser  github.com/hanemile/kscope
`

func main() {
	root    := flag.String("root",    ".",           "Linux kernel source root (git checkout)")
	addr    := flag.String("addr",    ":8080",       "HTTP listen address")
	plugins := flag.String("plugins", "./plugins",   "Plugin directory (.so files)")
	workers := flag.Int  ("workers", runtime.NumCPU()*2, "Worker goroutine count")
	verbose := flag.Bool ("v",       false,          "Verbose logging")
	flag.Parse()

	fmt.Fprint(os.Stderr, banner)

	if *verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}
	if _, err := os.Stat(*root); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "  ✗ Kernel source root not found: %s\n\n", *root)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "  root     %s\n", *root)
	fmt.Fprintf(os.Stderr, "  listen   http://localhost%s\n", *addr)
	fmt.Fprintf(os.Stderr, "  workers  %d\n", *workers)
	fmt.Fprintf(os.Stderr, "  plugins  %s\n\n", *plugins)
	fmt.Fprintf(os.Stderr, "  Indexing — progress shown below:\n\n")

	srv, err := server.New(server.Config{
		KernelRoot: *root, Addr: *addr,
		PluginDir: *plugins, Workers: *workers, Verbose: *verbose,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n  ✗ Failed to start: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "\n  Open http://localhost%s in your browser\n\n", *addr)
	if err := srv.Run(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
