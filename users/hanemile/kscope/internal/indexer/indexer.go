// internal/indexer/indexer.go — Source tree walker, symbol/xref/callgraph
// extractor, and auto-annotation generator. Runs as a worker job.
// Emits progress messages via the pool emit hook.
//
// Part of github.com/hanemile/kscope
package indexer

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hanemile/kscope/internal/protocol"
	"github.com/hanemile/kscope/internal/store"
	"github.com/hanemile/kscope/internal/worker"
)

// ── Compiled regexes ──────────────────────────────────────────────────────────

var (
	reStruct      = regexp.MustCompile(`(?m)^(?:typedef\s+)?struct\s+(\w+)`)
	reEnum        = regexp.MustCompile(`(?m)^(?:typedef\s+)?enum\s+(\w+)`)
	reTypeDef     = regexp.MustCompile(`(?m)^typedef\s+\S.*?\s+(\w+)\s*;`)
	reMacroDefine = regexp.MustCompile(`(?m)^#define\s+(\w+)\s+(.+)`)
	reExport      = regexp.MustCompile(`EXPORT_SYMBOL(?:_GPL)?\((\w+)\)`)
	reSyscall     = regexp.MustCompile(`(?:SYSCALL_DEFINE\d+|__x64_sys_)(\w+)`)
	// Function definition: captures function name
	reFuncDef     = regexp.MustCompile(`(?m)^(?:static\s+)?(?:inline\s+)?(?:\w[\w\s*]+)\s+(\w+)\s*\([^;)]*\)\s*\{`)
	// Call expression inside body: foo( with word boundary
	reFuncCall    = regexp.MustCompile(`\b(\w+)\s*\(`)
	// copy_from/to_user detection
	reCopyFromUser = regexp.MustCompile(`\bcopy_from_user\s*\(`)
	reCopyToUser   = regexp.MustCompile(`\bcopy_to_user\s*\(`)
	// sizeof(struct_name)
	reSizeof      = regexp.MustCompile(`\bsizeof\s*\(\s*(?:struct\s+)?(\w+)\s*\)`)
	// Identifiers for xref (used in function bodies)
	reIdent       = regexp.MustCompile(`\b([A-Za-z_]\w{3,})\b`)
	// Integer constant value
	reIntVal      = regexp.MustCompile(`^(0x[0-9a-fA-F]+|\d+)[UuLl]*$`)
)

// ── Handler ───────────────────────────────────────────────────────────────────

// terminalProgress prints a live-updating progress bar to stderr.
// It overwrites the same line using \r so the terminal stays clean.
func terminalProgress(label, phase string, files, total, syms int64, elapsed float64) {
	const width = 28
	pct := 0.0
	if total > 0 { pct = float64(files) / float64(total) }
	filled := int(pct * width)
	if filled > width { filled = width }
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)

	var line string
	switch phase {
	case "walk":
		line = fmt.Sprintf("\r\033[K  [%s] walking…  %.1fs", label, elapsed)
	case "parse":
		line = fmt.Sprintf("\r\033[K  [%s]  [%s]  %3.0f%%  %s / %s files  %s syms  %.1fs",
			label, bar, pct*100,
			commaf(files), commaf(total), commaf(syms), elapsed)
	case "dedup":
		line = fmt.Sprintf("\r\033[K  [%s] deduplication…  %s syms  %.1fs", label, commaf(syms), elapsed)
	case "done":
		// Final line — print with newline so it stays visible
		fmt.Fprintf(os.Stderr, "\r\033[K  [%s] ✓ done  %s syms  %s files  %.1fs\n",
			label, commaf(syms), commaf(files), elapsed)
		return
	case "error":
		fmt.Fprintf(os.Stderr, "\r\033[K  [%s] ✗ error after %.1fs\n", label, elapsed)
		return
	default:
		line = fmt.Sprintf("\r\033[K  [%s] %s…", label, phase)
	}
	fmt.Fprint(os.Stderr, line)
}

func commaf(n int64) string {
	s := fmt.Sprintf("%d", n)
	out := make([]byte, 0, len(s)+len(s)/3)
	for i, ch := range s {
		if i > 0 && (len(s)-i)%3 == 0 { out = append(out, ',') }
		out = append(out, byte(ch))
	}
	return string(out)
}

// Handler returns a worker.HandlerFunc that indexes a kernel source tree.
func Handler(swap func(*store.Index), annStore *store.AnnotationStore) worker.HandlerFunc {
	return func(ctx context.Context, job *worker.Job, emit func(*protocol.Msg)) error {
		p, ok := job.Payload.(protocol.IndexStartPayload)
		if !ok { return nil }

		root    := p.Root
		label   := p.Label
		start   := time.Now()
		workers := p.Workers
		if workers < 1 { workers = 8 }

		var totalFiles int64
		go func() {
			var n int64
			filepath.WalkDir(root, func(_ string, d os.DirEntry, _ error) error {
				if !d.IsDir() && isSource(d.Name()) { n++ }
				return nil
			})
			atomic.StoreInt64(&totalFiles, n)
		}()

		// Build directory skip set.
		// Default: skip well-known non-essential trees AND drivers/.
		// drivers/ can be indexed separately via POST /api/index-dir.
		defaultSkip := []string{
			".git", "Documentation", "tools", "scripts", "samples", "drivers",
		}
		skipSet := make(map[string]bool)
		if len(p.SkipDirs) > 0 {
			for _, d := range p.SkipDirs { skipSet[d] = true }
		} else {
			for _, d := range defaultSkip { skipSet[d] = true }
		}
		// If indexing a SubDir, root is already scoped; don't skip anything extra.
		if p.SubDir != "" {
			for k := range skipSet { delete(skipSet, k) }
			skipSet[".git"] = true // always skip git metadata
		}

		// Announce what we're skipping
		skipped := make([]string, 0, len(skipSet))
		for d := range skipSet { if d != ".git" { skipped = append(skipped, d) } }
		if len(skipped) > 0 {
			fmt.Fprintf(os.Stderr, "  [%s] skipping: %s\n", label, strings.Join(skipped, ", "))
		}

		terminalProgress(label, "walk", 0, 0, 0, 0)
		emit(protocol.MustNew(protocol.KindIndexProgress, protocol.IndexProgressPayload{
			Label: label, Phase: "walk", Message: "Walking source tree…",
		}))

		newIdx := store.NewIndex()
		var mu sync.Mutex
		var filesWalked, symsFound, xrefsFound, callEdges int64
		var autoAnns []store.Annotation

		sem := make(chan struct{}, workers)
		var wg sync.WaitGroup

		walkRoot := root
		if p.SubDir != "" {
			walkRoot = filepath.Join(root, filepath.Clean(p.SubDir))
		}
		walkErr := filepath.WalkDir(walkRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil { return nil }
			if ctx.Err() != nil { return context.Canceled }
			base := d.Name()
			if d.IsDir() {
				if skipSet[base] {
					return filepath.SkipDir
				}
				return nil
			}
			if !isSource(base) { return nil }

			rel, _ := filepath.Rel(root, path)
			wg.Add(1); sem <- struct{}{}
			go func(full, rel string) {
				defer wg.Done(); defer func() { <-sem }()

				syms, xrefs, edges, consts, anns := parseFile(full, rel, label)

				mu.Lock()
				for _, s := range syms {
					newIdx.Symbols[s.Name] = append(newIdx.Symbols[s.Name], s)
					newIdx.ByFile[rel]      = append(newIdx.ByFile[rel], s)
				}
				for _, x := range xrefs   { newIdx.XRefs[x.Symbol]        = append(newIdx.XRefs[x.Symbol], x) }
				for _, e := range edges    { newIdx.CallEdges[e.Caller]     = append(newIdx.CallEdges[e.Caller], e) }
				for _, c := range consts   { newIdx.Constants[c.Name]       = c }
				autoAnns = append(autoAnns, anns...)
				mu.Unlock()

				atomic.AddInt64(&filesWalked, 1)
				atomic.AddInt64(&symsFound, int64(len(syms)))
				atomic.AddInt64(&xrefsFound, int64(len(xrefs)))
				atomic.AddInt64(&callEdges, int64(len(edges)))

				f   := atomic.LoadInt64(&filesWalked)
				tot := atomic.LoadInt64(&totalFiles)
				pct := 0.0; if tot > 0 { pct = float64(f)/float64(tot)*100 }
				job.SetProgress(pct)

				if f%200 == 0 {
					el := time.Since(start).Seconds()
					sym := atomic.LoadInt64(&symsFound)
					terminalProgress(label, "parse", f, tot, sym, el)
					emit(protocol.MustNew(protocol.KindIndexProgress, protocol.IndexProgressPayload{
						Label: label, Phase: "parse",
						Files: f, Total: tot,
						Symbols: sym,
						XRefs: atomic.LoadInt64(&xrefsFound),
						Elapsed: el, Pct: pct,
					}))
				}
			}(path, rel)
			return nil
		})
		wg.Wait()
		if walkErr != nil && walkErr != context.Canceled { return walkErr }

		terminalProgress(label, "dedup", atomic.LoadInt64(&filesWalked), atomic.LoadInt64(&totalFiles), atomic.LoadInt64(&symsFound), time.Since(start).Seconds())
		emit(protocol.MustNew(protocol.KindIndexProgress, protocol.IndexProgressPayload{
			Label: label, Phase: "dedup", Message: "Detecting duplicates…",
		}))
		newIdx.Dups = detectDups(newIdx.Symbols)

		// Persist auto-annotations
		if annStore != nil { annStore.AddBatch(autoAnns) }

		swap(newIdx)

		el  := time.Since(start).Seconds()
		fw  := atomic.LoadInt64(&filesWalked)
		sym := atomic.LoadInt64(&symsFound)
		terminalProgress(label, "done", fw, atomic.LoadInt64(&totalFiles), sym, el)
		emit(protocol.MustNew(protocol.KindIndexProgress, protocol.IndexProgressPayload{
			Label: label, Phase: "done",
			Files: fw, Total: atomic.LoadInt64(&totalFiles),
			Symbols: sym, XRefs: atomic.LoadInt64(&xrefsFound),
			Elapsed: el, Pct: 100, Message: "Index complete",
		}))
		return nil
	}
}

// ── File parser ───────────────────────────────────────────────────────────────

func parseFile(fullPath, relPath, version string) (
	syms  []*store.Symbol,
	xrefs []*store.XRef,
	edges []*store.CallEdge,
	consts []*store.Constant,
	anns  []store.Annotation,
) {
	f, err := os.Open(fullPath)
	if err != nil { return }
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var lines []string
	for sc.Scan() { lines = append(lines, sc.Text()) }
	content := strings.Join(lines, "\n")

	baseKind := store.ClassifyPath(relPath)
	seen     := map[string]bool{}
	lineOf   := func(pos int) int { return strings.Count(content[:pos], "\n") + 1 }

	addSym := func(name, typ string, line int, kind store.SymbolKind) {
		key := name + ":" + typ
		if seen[key] { return }
		seen[key] = true
		syms = append(syms, &store.Symbol{
			Name: name, Kind: kind, KindStr: kind.String(),
			File: relPath, Line: line, Type: typ,
		})
	}

	// Struct / enum / typedef
	for _, m := range reStruct.FindAllStringSubmatchIndex(content, -1) {
		addSym(content[m[2]:m[3]], "struct", lineOf(m[0]), baseKind)
	}
	for _, m := range reEnum.FindAllStringSubmatchIndex(content, -1) {
		addSym(content[m[2]:m[3]], "enum", lineOf(m[0]), baseKind)
	}
	for _, m := range reTypeDef.FindAllStringSubmatchIndex(content, -1) {
		addSym(content[m[2]:m[3]], "typedef", lineOf(m[0]), baseKind)
	}
	// Macros → also extract as constants if value looks like int
	for _, m := range reMacroDefine.FindAllStringSubmatchIndex(content, -1) {
		name  := content[m[2]:m[3]]
		value := strings.TrimSpace(content[m[4]:m[5]])
		ln    := lineOf(m[0])
		addSym(name, "macro", ln, baseKind)
		// Classify constant
		kind := "expr"
		if reIntVal.MatchString(value)     { kind = "int" }
		if strings.HasPrefix(value, "\"")  { kind = "string" }
		if strings.Contains(value, "BIT(") { kind = "flag" }
		consts = append(consts, &store.Constant{
			Name: name, Value: value, File: relPath, Line: ln, Kind: kind, KindStr: kind,
		})
	}
	// Export / syscall
	for _, m := range reExport.FindAllStringSubmatchIndex(content, -1) {
		addSym(content[m[2]:m[3]], "func", lineOf(m[0]), store.KindExported)
	}
	for _, m := range reSyscall.FindAllStringSubmatchIndex(content, -1) {
		addSym(content[m[2]:m[3]], "func", lineOf(m[0]), store.KindSyscall)
	}

	// Function bodies → xrefs, call edges, auto-annotations (only .c files)
	if strings.HasSuffix(relPath, ".c") {
		funcs := reFuncDef.FindAllStringSubmatchIndex(content, -1)
		for fi, fm := range funcs {
			fnName    := content[fm[2]:fm[3]]
			bodyStart := fm[1]
			bodyEnd   := len(content)
			if fi+1 < len(funcs) { bodyEnd = funcs[fi+1][0] }
			body := content[bodyStart:bodyEnd]

			// Call edges
			for _, cm := range reFuncCall.FindAllStringSubmatchIndex(body, -1) {
				callee := body[cm[2]:cm[3]]
				if callee == fnName || isCKeyword(callee) { continue }
				ln := lineOf(bodyStart + cm[0])
				edges = append(edges, &store.CallEdge{
					Caller: fnName, Callee: callee,
					File: relPath, Line: ln,
				})
			}

			// XRefs (known symbols used in body)
			xrefSeen := map[string]bool{}
			for _, im := range reIdent.FindAllStringSubmatchIndex(body, -1) {
				ident := body[im[2]:im[3]]
				if ident == fnName || isCKeyword(ident) { continue }
				if !strings.Contains(ident, "_") && ident == strings.ToLower(ident) { continue }
				if xrefSeen[ident] { continue }
				xrefSeen[ident] = true
				ln  := lineOf(bodyStart + im[0])
				ctx := ""
				if ln-1 < len(lines) { ctx = strings.TrimSpace(lines[ln-1]) }
				xrefs = append(xrefs, &store.XRef{
					Symbol: ident, UsedIn: fnName,
					File: relPath, Line: ln, Context: ctx,
				})
			}

			// copy_from_user / copy_to_user annotations
			for _, cm := range reCopyFromUser.FindAllStringSubmatchIndex(body, -1) {
				ln := lineOf(bodyStart + cm[0])
				ctx := ""; if ln-1 < len(lines) { ctx = strings.TrimSpace(lines[ln-1]) }
				anns = append(anns, store.Annotation{
					Version: version, Symbol: fnName, File: relPath, Line: ln,
					Key: store.AnnCopyFromUser, Value: "true", ValueType: "bool",
					Note: "copy_from_user() in " + fnName + ": " + ctx, Source: "auto",
				})
			}
			for _, cm := range reCopyToUser.FindAllStringSubmatchIndex(body, -1) {
				ln := lineOf(bodyStart + cm[0])
				ctx := ""; if ln-1 < len(lines) { ctx = strings.TrimSpace(lines[ln-1]) }
				anns = append(anns, store.Annotation{
					Version: version, Symbol: fnName, File: relPath, Line: ln,
					Key: store.AnnCopyToUser, Value: "true", ValueType: "bool",
					Note: ctx, Source: "auto",
				})
			}

			// sizeof(X) annotations
			for _, sm := range reSizeof.FindAllStringSubmatchIndex(body, -1) {
				structName := body[sm[2]:sm[3]]
				ln := lineOf(bodyStart + sm[0])
				anns = append(anns, store.Annotation{
					Version: version, Symbol: structName, File: relPath, Line: ln,
					Key: store.AnnSizeofCall, Value: fnName, ValueType: "string",
					Note: "sizeof(" + structName + ") called in " + fnName, Source: "auto",
				})
			}
		}
	}
	return
}

func detectDups(symbols map[string][]*store.Symbol) []*store.DuplicateReport {
	var dups []*store.DuplicateReport
	for name, syms := range symbols {
		var kernel, uapi *store.Symbol
		for _, s := range syms {
			if s.Type != "struct" && s.Type != "enum" && s.Type != "typedef" { continue }
			if s.Kind == store.KindUAPI && uapi == nil     { uapi = s }
			if s.Kind == store.KindKernel && kernel == nil { kernel = s }
		}
		if kernel != nil && uapi != nil {
			risk := "medium"
			if strings.HasSuffix(kernel.File, ".h") { risk = "high" }
			dups = append(dups, &store.DuplicateReport{
				Name: name, KernelFile: kernel.File, UAPIFile: uapi.File,
				Risk: risk, Notes: "'" + name + "' in both kernel and uapi paths.",
			})
			kernel.Kind = store.KindDuplicated; kernel.KindStr = store.KindDuplicated.String(); kernel.UAPIPeer = uapi.File
			uapi.Kind   = store.KindDuplicated; uapi.KindStr   = store.KindDuplicated.String(); uapi.UAPIPeer   = kernel.File
		}
	}
	return dups
}

func isSource(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".c" || ext == ".h"
}

var cKeywords = map[string]bool{
	"if":true, "else":true, "for":true, "while":true, "do":true, "switch":true, "case":true,
	"return":true, "break":true, "continue":true, "goto":true, "sizeof":true, "typeof":true,
	"static":true, "inline":true, "const":true, "void":true, "int":true, "char":true, "long":true,
	"unsigned":true, "signed":true, "struct":true, "enum":true, "union":true, "typedef":true,
	"extern":true, "register":true, "volatile":true, "restrict":true,
	"NULL":true, "true":true, "false":true,
}

func isCKeyword(s string) bool { _, ok := cKeywords[s]; return ok }
