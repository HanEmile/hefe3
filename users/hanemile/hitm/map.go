package main

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// ============================================================================
// Source-less "Fuzz Map": build a state graph from observed HTTP flows.
//
// We have no source code and therefore no branch coverage. We substitute a
// RESPONSE FINGERPRINT for Fuzz Map's hit vector:
//
//   node identity = normalized URL path template
//                 + status class
//                 + content-type class
//                 + structural shape hash (HTML tag skeleton, or JSON key shape)
//
// Two renders of the same screen with different data (prices, names, tokens)
// hash to the same node; genuinely different screens differ. Edges connect
// consecutive flows within a session; the edge label is the action inferred
// from the request (form fields for POST, path for GET, async for XHR).
// ============================================================================

var (
	reNumericSeg = regexp.MustCompile(`^\d+$`)
	reHexSeg     = regexp.MustCompile(`^[0-9a-fA-F]{8,}$`)
	reUUIDSeg    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	reLongToken  = regexp.MustCompile(`^[A-Za-z0-9_\-]{24,}$`)
)

// pathTemplate normalizes a URL path so variable segments collapse to ":id".
// e.g. /order/4821/item/9f3a -> /order/:id/item/:id
func pathTemplate(rawurl string) string {
	u, err := url.Parse(rawurl)
	if err != nil {
		return rawurl
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, s := range segs {
		switch {
		case s == "":
			continue
		case reUUIDSeg.MatchString(s), reNumericSeg.MatchString(s),
			reHexSeg.MatchString(s), reLongToken.MatchString(s):
			segs[i] = ":id"
		}
	}
	p := "/" + strings.Join(segs, "/")
	p = strings.ReplaceAll(p, "//", "/")
	if p == "" {
		p = "/"
	}
	return p
}

func statusClass(status int) string {
	switch {
	case status == 0:
		return "err"
	case status >= 200 && status < 300:
		return "2xx"
	case status >= 300 && status < 400:
		return "3xx"
	case status >= 400 && status < 500:
		return "4xx"
	default:
		return "5xx"
	}
}

func contentTypeClass(ct string) string {
	ct = strings.ToLower(ct)
	switch {
	case strings.Contains(ct, "html"):
		return "html"
	case strings.Contains(ct, "json"):
		return "json"
	case strings.Contains(ct, "javascript"), strings.Contains(ct, "ecmascript"):
		return "js"
	case strings.Contains(ct, "xml"):
		return "xml"
	case strings.Contains(ct, "text/"):
		return "text"
	default:
		return "other"
	}
}

// htmlSkeletonHash hashes the DOM tag skeleton: tag names + nesting structure,
// dropping all text, attributes and values. Repeated siblings of the same tag
// are collapsed (branch-collapsing analogue) so a 2-row vs 3-row table match.
func htmlSkeletonHash(body []byte) string {
	z := html.NewTokenizer(strings.NewReader(string(body)))
	var sb strings.Builder
	depth := 0
	var lastSibling []string // last tag opened at each depth, to collapse repeats
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		switch tt {
		case html.StartTagToken:
			name, _ := z.TagName()
			tag := string(name)
			// Skip noise tags whose presence/repetition isn't structural.
			if tag == "script" || tag == "style" || tag == "br" || tag == "meta" || tag == "link" {
				continue
			}
			for len(lastSibling) <= depth {
				lastSibling = append(lastSibling, "")
			}
			if lastSibling[depth] == tag {
				// collapse consecutive same-tag siblings
				continue
			}
			lastSibling[depth] = tag
			sb.WriteByte('<')
			sb.WriteString(tag)
			sb.WriteByte(byte('0' + depth%10))
			depth++
			if depth >= len(lastSibling) {
				lastSibling = append(lastSibling, "")
			} else {
				lastSibling[depth] = ""
			}
		case html.EndTagToken:
			if depth > 0 {
				depth--
			}
		}
	}
	sum := sha1.Sum([]byte(sb.String()))
	return hex.EncodeToString(sum[:8])
}

// jsonShapeHash hashes the set of key paths and value types (not values).
func jsonShapeHash(body []byte) string {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		sum := sha1.Sum([]byte("invalid-json"))
		return hex.EncodeToString(sum[:8])
	}
	paths := map[string]bool{}
	var walk func(prefix string, val any)
	walk = func(prefix string, val any) {
		switch t := val.(type) {
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(prefix+"."+k, t[k])
			}
		case []any:
			// Collapse arrays: shape of first element only.
			if len(t) > 0 {
				walk(prefix+"[]", t[0])
			} else {
				paths[prefix+"[]:empty"] = true
			}
		case string:
			paths[prefix+":str"] = true
		case float64:
			paths[prefix+":num"] = true
		case bool:
			paths[prefix+":bool"] = true
		case nil:
			paths[prefix+":null"] = true
		}
	}
	walk("", v)
	keys := make([]string, 0, len(paths))
	for k := range paths {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sum := sha1.Sum([]byte(strings.Join(keys, "|")))
	return hex.EncodeToString(sum[:8])
}

// StateFingerprint computes the node identity for a flow's response.
func StateFingerprint(pf *PersistedFlow) string {
	return fingerprintResponse(pf.method, pf.url, pf.status, responseContentType(pf), pf.responseBody)
}

// packFingerprint computes the state identity AND the node kind/error flag for
// a flow and packs them into a single column value "fp|kind|err". Done once at
// persist time (SaveFlow) so the /map endpoint never has to re-parse the
// response dump/body. This is the single-parse path: responseContentType is
// read once here instead of up to 3x per flow per /map request.
func packFingerprint(pf *PersistedFlow) string {
	ct := responseContentType(pf)
	fp := fingerprintResponse(pf.method, pf.url, pf.status, ct, pf.responseBody)
	kind := contentTypeClass(ct)
	isErr := pf.status >= 400 || pf.status == 0 || htmlHasErrorMarkerCT(ct, pf.responseBody)
	if pf.status >= 300 && pf.status < 400 {
		kind = "redirect"
	}
	if isErr {
		kind = "error"
	}
	errFlag := "0"
	if isErr {
		errFlag = "1"
	}
	return fp + "|" + kind + "|" + errFlag
}

// unpackFingerprint splits a packed "fp|kind|err" column value back into its
// parts. If the value isn't packed (e.g. a freshly computed fallback that is
// just the raw fp), kind is derived as best-effort and err defaults to false.
func unpackFingerprint(packed string) (fp, kind string, isErr bool) {
	parts := strings.SplitN(packed, "|", 3)
	fp = parts[0]
	if len(parts) == 3 {
		return fp, parts[1], parts[2] == "1"
	}
	return fp, "other", false
}

// fingerprintResponse computes the state node identity from the raw pieces of a
// response. Factored out of StateFingerprint so the active map crawler can
// fingerprint a live response without first persisting it to a PersistedFlow.
// The behavior is identical to the previous inline StateFingerprint logic.
func fingerprintResponse(method, rawurl string, status int, ctype string, body []byte) string {
	_ = method // method is not part of the state identity (kept for API symmetry)
	ctc := contentTypeClass(ctype)
	tmpl := pathTemplate(rawurl)
	sc := statusClass(status)

	var shape string
	switch ctc {
	case "html":
		shape = htmlSkeletonHash(body)
	case "json":
		shape = jsonShapeHash(body)
	default:
		shape = "n/a"
	}
	raw := fmt.Sprintf("%s|%s|%s|%s", tmpl, sc, ctc, shape)
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:10])
}

// responseContentType extracts Content-Type from the persisted response dump.
func responseContentType(pf *PersistedFlow) string {
	if pf.hasResponse != 1 || len(pf.responseDump) == 0 {
		return ""
	}
	r := bufio.NewReader(strings.NewReader(string(pf.responseDump)))
	resp, err := http.ReadResponse(r, nil)
	if err != nil {
		return ""
	}
	resp.Body.Close()
	return resp.Header.Get("Content-Type")
}

// ============================================================================
// Sessionization: group flows into ordered sequences so consecutive pairs
// become edges. Cookie session id > Referer chain > per-IP time order.
// ============================================================================

var reSessionCookie = regexp.MustCompile(`(?i)(sessionid|session|sid|phpsessid|jsessionid|connect\.sid|_session)=([^;]+)`)

type reqMeta struct {
	cookieSession string
	referer       string
	remoteAddr    string
	method        string
	url           string
	headers       http.Header
	body          []byte
}

func parseRequestMeta(pf *PersistedFlow) reqMeta {
	m := reqMeta{method: pf.method, url: pf.url}
	if len(pf.requestDump) == 0 {
		return m
	}
	req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(string(pf.requestDump))))
	if err != nil {
		return m
	}
	m.headers = req.Header
	m.referer = req.Header.Get("Referer")
	if cookie := req.Header.Get("Cookie"); cookie != "" {
		if mt := reSessionCookie.FindStringSubmatch(cookie); mt != nil {
			m.cookieSession = mt[2]
		}
	}
	m.body = pf.requestBody
	return m
}

// ============================================================================
// Graph model
// ============================================================================

type MapNode struct {
	ID           string `json:"id"`
	Label        string `json:"label"`        // page: path template; status: the status code as text
	Type         string `json:"type"`         // "page" or "status"
	Kind         string `json:"kind"`         // html|json|redirect|error|other (page nodes)
	Status       int    `json:"status"`       // status code (status nodes) / representative (page nodes)
	SampleFlowID uint64 `json:"sampleFlowId"` // a flow that produced this state
	Error        bool   `json:"error"`
	Count        int    `json:"count"` // flows folded into this node
}

type MapEdge struct {
	From    string   `json:"from"`
	To      string   `json:"to"`
	Label   string   `json:"label"`
	Count   int      `json:"count"`
	Async   bool     `json:"async"`
	Error   bool     `json:"error"`
	FlowIDs []uint64 `json:"flowIds"`
}

type MapGraph struct {
	Nodes []*MapNode `json:"nodes"`
	Edges []*MapEdge `json:"edges"`
}

// BuildGraph folds a set of flows into nodes (by fingerprint) and edges
// (consecutive flow pairs within a session).
func BuildGraph(flows []*PersistedFlow) *MapGraph {
	// Stable order by id.
	sort.Slice(flows, func(i, j int) bool { return flows[i].id < flows[j].id })

	nodes := map[string]*MapNode{}
	edges := map[string]*MapEdge{}
	// Per flow we track both the page node id (status-agnostic) and the
	// per-page status node id (page + concrete status code).
	pageByFlow := map[uint64]string{}
	statusByFlow := map[uint64]string{}
	metaByFlow := map[uint64]reqMeta{}

	// pageID/statusID identities. The page is status-agnostic (method + path
	// template); the status node hangs off a specific page + status code so
	// "/login → 200" and "/login → 404" are distinct nodes, not shared globally.
	for _, pf := range flows {
		var kind string
		var isErr bool
		if pf.fingerprint != "" {
			_, kind, isErr = unpackFingerprint(pf.fingerprint)
		} else {
			ct := responseContentType(pf)
			kind = contentTypeClass(ct)
			isErr = pf.status >= 400 || pf.status == 0 || htmlHasErrorMarkerCT(ct, pf.responseBody)
			if pf.status >= 300 && pf.status < 400 {
				kind = "redirect"
			}
			if isErr {
				kind = "error"
			}
		}

		method := pf.method
		if method == "" {
			method = "GET"
		}
		tmpl := pathTemplate(pf.url)
		pageID := "page:" + method + " " + tmpl
		statusID := pageID + "#" + strconv.Itoa(pf.status)
		pageByFlow[pf.id] = pageID
		statusByFlow[pf.id] = statusID
		metaByFlow[pf.id] = parseRequestMeta(pf)

		// Page node (status-agnostic). Label is just the path (name only); the
		// frontend conveys kind/error via color and on hover.
		pn := nodes[pageID]
		if pn == nil {
			pn = &MapNode{
				ID:           pageID,
				Label:        tmpl,
				Type:         "page",
				Kind:         kind,
				SampleFlowID: pf.id,
			}
			nodes[pageID] = pn
		}
		pn.Count++
		if isErr {
			// a page is "error" only as a kind hint; the status node carries the
			// authoritative error state.
		}

		// Status node (per page). Label is just the status code.
		sn := nodes[statusID]
		if sn == nil {
			sn = &MapNode{
				ID:           statusID,
				Label:        statusLabel(pf.status),
				Type:         "status",
				Kind:         kind,
				Status:       pf.status,
				SampleFlowID: pf.id,
				Error:        isErr,
			}
			nodes[statusID] = sn
		}
		sn.Count++

		// "renders" edge: page -> its status node. Thin, unlabeled; shows which
		// outcomes a page produced.
		rkey := pageID + "=>" + statusID
		re := edges[rkey]
		if re == nil {
			re = &MapEdge{From: pageID, To: statusID, Label: "", Error: isErr}
			edges[rkey] = re
		}
		re.Count++
		re.Error = re.Error || isErr
		if len(re.FlowIDs) < 50 {
			re.FlowIDs = append(re.FlowIDs, pf.id)
		}
	}

	// Sessionize and connect consecutive pairs. The navigation/transition edge
	// goes from the previous flow's STATUS node to the current flow's STATUS
	// node, labelled with the action — so "clicking a link that returns 400" is
	// literally a distinct edge landing on that page's 400 node.
	sessions := sessionize(flows, metaByFlow)
	for _, seq := range sessions {
		for i := 1; i < len(seq); i++ {
			prev := seq[i-1]
			cur := seq[i]
			fromStatus := statusByFlow[prev.id]
			toStatus := statusByFlow[cur.id]
			meta := metaByFlow[cur.id]
			label := inferAction(meta)
			async := isAsync(meta)
			isErr := cur.status >= 400 || cur.status == 0

			key := fromStatus + "->" + toStatus + "|" + label + boolKey(async)
			e := edges[key]
			if e == nil {
				e = &MapEdge{From: fromStatus, To: toStatus, Label: label, Async: async, Error: isErr}
				edges[key] = e
			}
			e.Count++
			e.Error = e.Error || isErr
			if len(e.FlowIDs) < 50 {
				e.FlowIDs = append(e.FlowIDs, cur.id)
			}
		}
	}

	g := &MapGraph{}
	for _, n := range nodes {
		g.Nodes = append(g.Nodes, n)
	}
	for _, e := range edges {
		g.Edges = append(g.Edges, e)
	}
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].SampleFlowID < g.Nodes[j].SampleFlowID })
	sort.Slice(g.Edges, func(i, j int) bool {
		if g.Edges[i].From != g.Edges[j].From {
			return g.Edges[i].From < g.Edges[j].From
		}
		return g.Edges[i].To < g.Edges[j].To
	})
	return g
}

func boolKey(b bool) string {
	if b {
		return "|async"
	}
	return ""
}

// statusLabel renders a status code for a status node label. 0 means the
// request never got a response (connection error).
func statusLabel(status int) string {
	if status == 0 {
		return "ERR"
	}
	return strconv.Itoa(status)
}

// sessionize groups flows into ordered sequences.
func sessionize(flows []*PersistedFlow, meta map[uint64]reqMeta) [][]*PersistedFlow {
	// 1. Group by cookie session if present.
	byCookie := map[string][]*PersistedFlow{}
	var noCookie []*PersistedFlow
	for _, pf := range flows {
		if cs := meta[pf.id].cookieSession; cs != "" {
			byCookie[cs] = append(byCookie[cs], pf)
		} else {
			noCookie = append(noCookie, pf)
		}
	}

	var sessions [][]*PersistedFlow
	for _, seq := range byCookie {
		sessions = append(sessions, seq)
	}

	// 2. For cookie-less flows, chain by Referer where possible; otherwise put
	//    them all in one time-ordered fallback session.
	if len(noCookie) > 0 {
		// Build a lookup from response-url-template -> flow, to resolve referers.
		// Simpler robust fallback: single time-ordered session. Referer chaining
		// is approximated by keeping them in arrival order, which is what most
		// single-client browsing produces anyway.
		sessions = append(sessions, noCookie)
	}
	return sessions
}

// inferAction derives a human edge label from the request that caused the
// transition.
func inferAction(m reqMeta) string {
	tmpl := pathTemplate(m.url)
	method := m.method
	if method == "" {
		method = "GET"
	}
	if method == "POST" || method == "PUT" || method == "PATCH" {
		if fields := formFieldNames(m); len(fields) > 0 {
			return fmt.Sprintf("%s %s {%s}", method, tmpl, strings.Join(fields, ", "))
		}
	}
	return fmt.Sprintf("%s %s", method, tmpl)
}

// formFieldNames extracts submitted field names from a urlencoded/multipart body.
func formFieldNames(m reqMeta) []string {
	if len(m.body) == 0 || m.headers == nil {
		return nil
	}
	ct := strings.ToLower(m.headers.Get("Content-Type"))
	var names []string
	if strings.Contains(ct, "application/x-www-form-urlencoded") {
		vals, err := url.ParseQuery(string(m.body))
		if err == nil {
			for k := range vals {
				names = append(names, k)
			}
		}
	} else if strings.Contains(ct, "multipart/form-data") {
		// crude: pull name="..." occurrences
		for _, mt := range reMultipartName.FindAllStringSubmatch(string(m.body), -1) {
			names = append(names, mt[1])
		}
	} else if strings.Contains(ct, "application/json") {
		var obj map[string]any
		if json.Unmarshal(m.body, &obj) == nil {
			for k := range obj {
				names = append(names, k)
			}
		}
	}
	sort.Strings(names)
	if len(names) > 5 {
		names = append(names[:5], "…")
	}
	return names
}

var reMultipartName = regexp.MustCompile(`name="([^"]+)"`)

// isAsync detects XHR/fetch-style requests (async, no full navigation).
func isAsync(m reqMeta) bool {
	if m.headers == nil {
		return false
	}
	if strings.EqualFold(m.headers.Get("X-Requested-With"), "XMLHttpRequest") {
		return true
	}
	if dest := m.headers.Get("Sec-Fetch-Dest"); dest == "empty" {
		return true
	}
	accept := strings.ToLower(m.headers.Get("Accept"))
	if strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/html") {
		return true
	}
	return false
}

var reHTMLError = regexp.MustCompile(`(?i)(class="[^"]*error|<[^>]*error[^>]*>|exception|stack ?trace|traceback)`)

func htmlHasErrorMarker(pf *PersistedFlow) bool {
	return htmlHasErrorMarkerCT(responseContentType(pf), pf.responseBody)
}

// htmlHasErrorMarkerCT is htmlHasErrorMarker given an already-extracted
// content-type, so callers that already parsed the response (packFingerprint)
// don't pay for a second http.ReadResponse.
func htmlHasErrorMarkerCT(ctype string, body []byte) bool {
	if contentTypeClass(ctype) != "html" {
		return false
	}
	// Only scan a prefix to stay cheap.
	if len(body) > 8192 {
		body = body[:8192]
	}
	return reHTMLError.Match(body)
}

// dumpMap loads all flows, builds the graph, and prints a summary to stdout.
// Used by the -mapdump diagnostic flag (no ports bound).
func dumpMap(store *Store) {
	flows, err := store.LoadFlowsMeta()
	if err != nil {
		fmt.Println("LoadFlowsMeta error:", err)
		return
	}
	g := BuildGraph(flows)
	fmt.Printf("flows=%d nodes=%d edges=%d\n", len(flows), len(g.Nodes), len(g.Edges))
	fmt.Println("--- nodes ---")
	for _, n := range g.Nodes {
		fmt.Printf("  [%s] %s status=%d kind=%s count=%d err=%v (sample flow %d)\n",
			n.ID[:6], n.Label, n.Status, n.Kind, n.Count, n.Error, n.SampleFlowID)
	}
	fmt.Println("--- edges ---")
	for _, e := range g.Edges {
		fmt.Printf("  %s -> %s : %q count=%d async=%v err=%v\n",
			e.From[:6], e.To[:6], e.Label, e.Count, e.Async, e.Error)
	}
}

// mapHandler serves GET /map: it rebuilds the state graph from all persisted
// flows and returns it as JSON. Recomputed on demand (flows are already in DB).
func (s *HITM) mapHandler(w http.ResponseWriter, r *http.Request) {
	src := r.URL.Query().Get("source")

	// Cache fast path: if no new flow has been persisted since we last built the
	// graph for this source filter, serve the cached JSON. This makes repeated
	// /map calls O(1).
	if cached := s.cachedMap(src); cached != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(cached)
		return
	}

	// Snapshot the dirty generation BEFORE loading. If a flow is persisted while
	// we build, the generation advances and storeMap declines to cache the stale
	// result, so the next request rebuilds.
	gen := s.mapGen.Load()

	// Lightweight load: only the columns BuildGraph needs (no response BLOBs).
	flows, err := s.Store.LoadFlowsMeta()
	if err != nil {
		http.Error(w, "Failed to load flows: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Optional source filter, e.g. /map?source=proxy
	if src != "" {
		filtered := flows[:0]
		for _, pf := range flows {
			if pf.source == src {
				filtered = append(filtered, pf)
			}
		}
		flows = filtered
	}
	g := BuildGraph(flows)
	body, err := json.Marshal(g)
	if err != nil {
		http.Error(w, "Failed to encode map: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.storeMap(src, body, gen)
	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

// cachedMap returns the cached JSON for the given source filter if it is still
// current (built at the latest generation), else nil.
func (s *HITM) cachedMap(source string) []byte {
	s.mapCacheMu.RLock()
	defer s.mapCacheMu.RUnlock()
	if s.mapCacheGen != s.mapGen.Load() {
		return nil
	}
	return s.mapCache[source]
}

// storeMap caches the built graph JSON for a source filter, but only if no new
// flow was persisted since the build started (gen unchanged). The generation
// counter is bumped by the OutFlow drain on every persisted flow, so a flow
// that arrives mid-build invalidates this result rather than caching stale
// data.
func (s *HITM) storeMap(source string, body []byte, gen uint64) {
	if s.mapGen.Load() != gen {
		return // a flow arrived during the build; don't cache a stale graph
	}
	s.mapCacheMu.Lock()
	defer s.mapCacheMu.Unlock()
	// If the cache map is from an older generation, reset it so stale per-source
	// entries don't leak across generations.
	if s.mapCache == nil || s.mapCacheGen != gen {
		s.mapCache = map[string][]byte{}
		s.mapCacheGen = gen
	}
	s.mapCache[source] = body
}
