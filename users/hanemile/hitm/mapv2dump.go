package main

import (
	"fmt"
	"time"
)

// dumpMapV2 builds the Map v2 snapshot from the DB and prints a summary, then
// returns. Diagnostic for the -mapv2dump flag: exercises the snapshot/tree/
// layout/cluster-columns path at runtime against the real DB with NO ports bound.
func dumpMapV2(h *HITM) {
	t0 := time.Now()
	snap, ready := h.mapSnapshot()
	build := time.Since(t0)
	if !ready || snap == nil {
		fmt.Printf("mapv2dump: snapshot not ready (ready=%v snap=%v) after %v\n", ready, snap != nil, build)
		// try once more after a short wait in case build is async
		time.Sleep(2 * time.Second)
		snap, ready = h.mapSnapshot()
		if !ready || snap == nil {
			fmt.Println("mapv2dump: still not ready; aborting")
			return
		}
	}

	meta := snap.Meta()
	fmt.Printf("=== Map v2 snapshot (built in %v) ===\n", build)
	fmt.Printf("gen=%d nodeN=%d edgeN=%d clusterN=%d maxDepth=%d worldBBox=%v\n",
		meta.Gen, meta.NodeN, meta.EdgeN, meta.ClusterN, meta.MaxDepth, meta.WorldBBox)

	rows := snap.RootRows()
	fmt.Printf("--- root rows (%d) ---\n", len(rows))
	for i, r := range rows {
		if i >= 12 {
			fmt.Printf("  ... (%d more)\n", len(rows)-12)
			break
		}
		fmt.Printf("  %-28s leaves=%-6d flows=%-6d err=%.0f%% kids=%-4d hasKids=%v\n",
			r.Path, r.LeafCount, r.FlowCount, r.ErrorRate*100, r.ChildCount, r.HasChildren)
	}

	// Drill one level into the first expandable root.
	for _, r := range rows {
		if r.HasChildren {
			kids := snap.ChildrenOf(r.Path)
			fmt.Printf("--- ChildrenOf(%q): %d rows ---\n", r.Path, len(kids))
			for i, k := range kids {
				if i >= 6 {
					break
				}
				fmt.Printf("  %-28s leaves=%d flows=%d\n", k.Path, k.LeafCount, k.FlowCount)
			}
			break
		}
	}

	// Cluster columns (the WebGL Overview data) at depth 2.
	tc := time.Now()
	cc := snap.ClusterColumns(2)
	fmt.Printf("--- ClusterColumns(depth=2) in %v: %d bubbles, %d edges ---\n",
		time.Since(tc), len(cc.X), len(cc.ESrc))

	// Search smoke.
	m, exp := snap.SearchLeaves("blog")
	fmt.Printf("--- SearchLeaves(\"blog\"): %d matches, %d expand paths ---\n", len(m), len(exp))

	// Second snapshot call should be the cached fast path.
	t2 := time.Now()
	snap.RootRows()
	fmt.Printf("--- second RootRows() (cache path) in %v ---\n", time.Since(t2))
}
