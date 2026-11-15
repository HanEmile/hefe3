package main

import (
	"math"
	"sort"
)

// ============================================================================
// maplayout.go — deterministic layout for the Map v2 cluster tree.
//
// We lay out the ClusterTree with a recursive squarified treemap: each internal
// node owns a rectangle (the root owns the world box); its children partition
// that rectangle with area proportional to sqrt(LeafCount) (so a cluster with
// 100 leaves is not 100x the area of a 1-leaf cluster — sqrt keeps the huge
// long-tail clusters from swamping everything). Within each node's rectangle we
// recurse; the final X,Y is the rectangle center and R is half the smaller side
// (a disc that fits the cell), used both as the cluster-bubble radius and the
// WebGL LOD radius.
//
// Fully deterministic: no RNG. Children are processed in their (already sorted)
// trie order. Complexity is O(N) rectangle assignments with an O(k log k) sort
// of siblings by weight inside the squarify pass, so O(N log N) overall.
// ============================================================================

// rect is an axis-aligned rectangle in world units.
type rect struct {
	x, y, w, h float64
}

// LayoutTree assigns X,Y,R to every node of the tree within a world box of the
// given size (origin at 0,0). It returns the world bounding box
// [x0,y0,x1,y1]. Deterministic and O(N log N).
func LayoutTree(t *ClusterTree, worldW, worldH float64) [4]float32 {
	if t == nil || t.Root == nil {
		return [4]float32{0, 0, float32(worldW), float32(worldH)}
	}
	root := rect{0, 0, worldW, worldH}
	layoutChildren(t.Root, root)

	// The root itself gets the full box.
	t.Root.X = float32(worldW / 2)
	t.Root.Y = float32(worldH / 2)
	t.Root.R = float32(math.Min(worldW, worldH) / 2)

	return [4]float32{0, 0, float32(worldW), float32(worldH)}
}

// layoutChildren partitions parent's rect r among parent.Children using a
// squarified treemap, sets each child's X,Y,R, then recurses.
func layoutChildren(parent *ClusterNode, r rect) {
	kids := parent.Children
	if len(kids) == 0 {
		return
	}

	// Weight per child: sqrt(leafCount) (min 1) to damp the long tail.
	type wkid struct {
		node   *ClusterNode
		weight float64
	}
	ws := make([]wkid, 0, len(kids))
	total := 0.0
	for _, c := range kids {
		lc := c.LeafCount
		if lc < 1 {
			lc = 1
		}
		w := math.Sqrt(float64(lc))
		ws = append(ws, wkid{c, w})
		total += w
	}
	if total <= 0 {
		total = 1
	}

	// Squarify wants weights as areas summing to the rect area. Scale so that
	// sum(area) == r.w*r.h.
	area := r.w * r.h
	scale := area / total
	for i := range ws {
		ws[i].weight *= scale
	}

	// Stable sort by descending weight for the squarify aspect-ratio heuristic.
	// We keep a parallel index so the final placement order is deterministic.
	sort.SliceStable(ws, func(i, j int) bool { return ws[i].weight > ws[j].weight })

	areas := make([]float64, len(ws))
	for i := range ws {
		areas[i] = ws[i].weight
	}
	rects := squarify(areas, r)

	for i, c := range ws {
		rc := rects[i]
		// Inset slightly so sibling bubbles don't touch (purely cosmetic, keeps
		// the disc radius from implying overlap).
		inset := 0.06 * math.Min(rc.w, rc.h)
		ix := rc.x + inset
		iy := rc.y + inset
		iw := rc.w - 2*inset
		ih := rc.h - 2*inset
		if iw < 0 {
			iw = rc.w
			ix = rc.x
		}
		if ih < 0 {
			ih = rc.h
			iy = rc.y
		}
		c.node.X = float32(ix + iw/2)
		c.node.Y = float32(iy + ih/2)
		c.node.R = float32(math.Min(iw, ih) / 2)
		// Recurse into the un-inset rect so descendants fill the cell.
		layoutChildren(c.node, rc)
	}
}

// squarify partitions rect r into len(areas) sub-rectangles whose areas match
// `areas` (which already sum to r's area), using the classic squarified
// treemap algorithm (Bruls, Huizing, van Wijk). Output order matches input
// order. Deterministic.
func squarify(areas []float64, r rect) []rect {
	out := make([]rect, len(areas))
	if len(areas) == 0 {
		return out
	}

	// We lay rows along the shorter side to keep aspect ratios near 1.
	remaining := r
	i := 0
	n := len(areas)
	for i < n {
		// Build a row [i, j) maximizing squareness.
		shortSide := math.Min(remaining.w, remaining.h)
		row := []int{i}
		rowSum := areas[i]
		bestWorst := worstAspect(areas[i:i+1], rowSum, shortSide)
		j := i + 1
		for j < n {
			newSum := rowSum + areas[j]
			worst := worstAspect(areas[i:j+1], newSum, shortSide)
			if worst > bestWorst {
				break // adding this one makes the row worse; stop the row
			}
			bestWorst = worst
			rowSum = newSum
			row = append(row, j)
			j++
		}

		// Place the row [i, j) inside `remaining`, consuming a strip along the
		// longer dimension.
		layoutRow(areas[i:j], rowSum, remaining, out[i:j])

		// Shrink `remaining` by the consumed strip.
		if remaining.w >= remaining.h {
			// strip occupied a vertical column of width = rowSum/h
			stripW := rowSum / remaining.h
			remaining.x += stripW
			remaining.w -= stripW
		} else {
			stripH := rowSum / remaining.w
			remaining.y += stripH
			remaining.h -= stripH
		}
		i = j
	}
	return out
}

// layoutRow places the given row of areas as a strip in `r`, writing into out.
// The strip runs along the shorter side of r.
func layoutRow(areas []float64, sum float64, r rect, out []rect) {
	if sum <= 0 {
		// Degenerate: distribute equally to avoid NaN.
		for k := range out {
			out[k] = rect{r.x, r.y, 0, 0}
		}
		return
	}
	if r.w >= r.h {
		// Vertical strip of width stripW spanning the full height; subdivide by
		// height proportionally.
		stripW := sum / r.h
		y := r.y
		for k, a := range areas {
			h := a / stripW
			out[k] = rect{r.x, y, stripW, h}
			y += h
		}
	} else {
		// Horizontal strip of height stripH spanning the full width.
		stripH := sum / r.w
		x := r.x
		for k, a := range areas {
			w := a / stripH
			out[k] = rect{x, r.y, w, stripH}
			x += w
		}
	}
}

// worstAspect returns the worst (largest) aspect ratio among rectangles whose
// areas are `areas`, packed into a strip of the given short side and total
// length = sum/shortSide. This is the `worst` function from the squarified
// treemap paper.
func worstAspect(areas []float64, sum, shortSide float64) float64 {
	if sum <= 0 || shortSide <= 0 {
		return math.Inf(1)
	}
	var rmin, rmax float64 = math.Inf(1), 0
	for _, a := range areas {
		if a < rmin {
			rmin = a
		}
		if a > rmax {
			rmax = a
		}
	}
	s2 := sum * sum
	side2 := shortSide * shortSide
	// max( side^2 * rmax / sum^2 , sum^2 / (side^2 * rmin) )
	w1 := side2 * rmax / s2
	w2 := s2 / (side2 * rmin)
	return math.Max(w1, w2)
}
