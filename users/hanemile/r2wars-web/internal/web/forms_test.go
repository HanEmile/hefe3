package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"git.emile.space/r2wars-web/internal/model"
	"git.emile.space/r2wars-web/internal/r2client"
)

// This file is the systematic answer to a real incident: /battle/quick's
// template had no arena-size/max-rounds inputs at all, but the handler
// still required them, so every single submission failed with "Invalid
// arena size or max rounds" - a bug no amount of testing the *handler* in
// isolation could catch, because the break was in the gap between the
// template and the handler, not inside either one.
//
// formFields scrapes the real <input>/<select>/<textarea> names out of a
// rendered page instead of a test hardcoding what it assumes the form
// looks like. Submitting exactly those scraped fields - not a hand-typed
// guess - means a template that drops a field the handler needs, or a
// handler that starts demanding a field the template never offers, fails a
// test immediately, regardless of which side regressed.

// A second, independent incident this file now guards against: a button
// used the HTML form="X" attribute to submit a <form id="X"> declared
// elsewhere on the page (valid HTML - form-association-by-id works
// regardless of DOM nesting) - but that form lived inside an {{ if .myBots }}
// block, so for a viewer with no bots of their own (including the battle's
// own owner), the form never rendered at all. The button was still right
// there, still clickable, and silently did nothing: no request ever left
// the browser. assertFormReferencesResolve, wired into every page fetch
// below, fails the instant any button references a form id missing from
// that same render.

var formIDRE = regexp.MustCompile(`<form\b[^>]*\bid="([^"]+)"`)
var formRefRE = regexp.MustCompile(`<(?:input|button)\b[^>]*\bform="([^"]+)"`)

func assertFormReferencesResolve(t *testing.T, path, body string) {
	t.Helper()
	ids := map[string]bool{}
	for _, m := range formIDRE.FindAllStringSubmatch(body, -1) {
		ids[m[1]] = true
	}
	for _, m := range formRefRE.FindAllStringSubmatch(body, -1) {
		if !ids[m[1]] {
			t.Errorf("%s: an element has form=%q, but no <form id=%q> exists in this render - clicking it does nothing, silently, with no request ever sent", path, m[1], m[1])
		}
	}
}

var inputNameRE = regexp.MustCompile(`<(?:input|select|textarea)[^>]*\bname="([^"]+)"`)
var inputTypeAndNameRE = regexp.MustCompile(`<input\b([^>]*)\bname="([^"]+)"`)
var typeAttrRE = regexp.MustCompile(`\btype="([^"]+)"`)
var formActionRE = regexp.MustCompile(`<form\b[^>]*\baction="([^"]+)"`)

// scrapedForm is one <input>-bearing form as it actually rendered.
type scrapedForm struct {
	action string
	fields []scrapedField
}

type scrapedField struct {
	name      string
	inputType string // "" for <select>/<textarea>, else the <input type="...">
}

// scrapeForms extracts every form's action and field names/types from
// rendered HTML. This is deliberately a simple regex scan, not a real HTML
// parser: these are templates this test suite controls, not arbitrary
// hostile markup, so "good enough to catch a missing/renamed field" is the
// right amount of rigor here.
func scrapeForms(body string) []scrapedForm {
	formStarts := formActionRE.FindAllStringSubmatchIndex(body, -1)
	if len(formStarts) == 0 {
		return nil
	}

	var forms []scrapedForm
	for i, m := range formStarts {
		action := body[m[2]:m[3]]
		start := m[1]
		end := len(body)
		if i+1 < len(formStarts) {
			end = formStarts[i+1][0]
		}
		section := body[start:end]

		var fields []scrapedField
		for _, fm := range inputTypeAndNameRE.FindAllStringSubmatch(section, -1) {
			attrs, name := fm[1], fm[2]
			inputType := "text"
			if tm := typeAttrRE.FindStringSubmatch(attrs); tm != nil {
				inputType = tm[1]
			}
			fields = append(fields, scrapedField{name: name, inputType: inputType})
		}
		// <select>/<textarea> names (no "type" attribute to extract).
		for _, fm := range inputNameRE.FindAllStringSubmatch(section, -1) {
			if strings.HasPrefix(fm[0], "<input") {
				continue // already captured above with its type
			}
			fields = append(fields, scrapedField{name: fm[1]})
		}
		forms = append(forms, scrapedForm{action: action, fields: fields})
	}
	return forms
}

// findForm returns the first scraped form whose action contains substr.
func findForm(t *testing.T, forms []scrapedForm, substr string) scrapedForm {
	t.Helper()
	for _, f := range forms {
		if strings.Contains(f.action, substr) {
			return f
		}
	}
	t.Fatalf("no form with action containing %q found; forms = %+v", substr, forms)
	return scrapedForm{}
}

// fill builds a urlencoded body for a scraped form: checkboxes/radios are
// only included (as "on") if explicitly requested by name, everything else
// gets a value from overrides if present, else a type-appropriate default.
// This mirrors how a browser actually submits a form - unchecked boxes
// aren't sent at all - which matters because that's exactly the "field
// present in the DOM but legitimately absent from the POST" case the
// previous hand-written tests didn't model.
func fill(form scrapedForm, check map[string]bool, overrides map[string]string) string {
	vals := url.Values{}
	for _, f := range form.fields {
		switch f.inputType {
		case "checkbox", "radio":
			if check[f.name] {
				vals.Set(f.name, "on")
			}
		case "submit", "button":
			// not a data field
		default:
			if v, ok := overrides[f.name]; ok {
				vals.Set(f.name, v)
			} else {
				vals.Set(f.name, "1")
			}
		}
	}
	return vals.Encode()
}

func getBody(t *testing.T, handler http.Handler, path string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want %d, body=%s", path, rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	assertFormReferencesResolve(t, path, body)
	return body
}

// TestQuickBattleFormMatchesHandler is a direct regression test for the
// reported incident: scrape /battle/quick's real form and submit exactly
// those fields (plus checking the bot boxes) - no arena-size/max-rounds
// hand-typed in, because the template genuinely doesn't have them. This
// fails immediately if the handler ever again demands a field the form
// doesn't offer.
func TestQuickBattleFormMatchesHandler(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.DevMode = true
	handler := srv.Routes()

	archID, bitID := seedXArch(t, srv)
	for _, name := range []string{"bot-a", "bot-b"} {
		rec := postForm(t, handler, "/bot/new", fmt.Sprintf("name=%s&source=nop&arch-%d=on&bit-%d=on", name, archID, bitID))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("POST /bot/new(%s) = %d, want %d", name, rec.Code, http.StatusSeeOther)
		}
	}
	bots, err := srv.Store.GetAllBots()
	if err != nil || len(bots) != 2 {
		t.Fatalf("GetAllBots() = %+v, %v, want 2 bots", bots, err)
	}

	body := getBody(t, handler, "/battle/quick")
	form := findForm(t, scrapeForms(body), "/battle/quick")

	check := map[string]bool{}
	for _, b := range bots {
		check[fmt.Sprintf("bot-%d", b.ID)] = true
	}

	rec := postForm(t, handler, form.action, fill(form, check, nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s = %d, want %d, body=%s", form.action, rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if strings.Contains(loc, "res=") || strings.Contains(loc, "/battle/quick") {
		t.Fatalf("POST %s redirected to %q, want a new battle, not an error back to the form", form.action, loc)
	}
}

// seedXArch returns the seeded x86/32 arch+bit ids, used across tests that
// need a bot buildable against something real.
func seedXArch(t *testing.T, srv *Server) (archID, bitID int) {
	t.Helper()
	archs, err := srv.Store.GetAllArchs()
	if err != nil {
		t.Fatalf("GetAllArchs() error = %v", err)
	}
	bits, err := srv.Store.GetAllBits()
	if err != nil {
		t.Fatalf("GetAllBits() error = %v", err)
	}
	for _, a := range archs {
		if a.Name == "x86" {
			archID = a.ID
		}
	}
	for _, b := range bits {
		if b.Name == "32" {
			bitID = b.ID
		}
	}
	if archID == 0 || bitID == 0 {
		t.Fatal("seeded x86/32 arch+bit not found")
	}
	return archID, bitID
}

// TestEveryFormSubmitsSuccessfully walks every page in the app that renders
// a <form>, submits each one with its own scraped fields, and asserts the
// result isn't an error bounced back to the same page. This is the general
// case of TestQuickBattleFormMatchesHandler: one incident showed the gap
// between "the handler compiles and has a unit test" and "the page you'd
// actually land on works" is exactly where real bugs hide, so this walks
// the real rendered forms instead of asserting against hand-typed
// assumptions about what each form contains.
func TestEveryFormSubmitsSuccessfully(t *testing.T) {
	srv, mail := newTestServer(t)
	// Real login is exercised here (no DevMode), but bot assembly shouldn't
	// depend on radare2 being installed on whatever machine runs this test.
	srv.OpenR2 = func(string) (r2client.Pipe, error) { return fakeR2{}, nil }
	handler := srv.Routes()

	// --- register (no DevMode yet: this is the one flow that must work
	// for an anonymous visitor with no bypass) ---
	body := getBody(t, handler, "/register")
	form := findForm(t, scrapeForms(body), "/register")
	rec := postForm(t, handler, form.action, fill(form, nil, map[string]string{
		"username": "formtester",
		"email":    "formtester@example.com",
	}))
	if rec.Code != http.StatusSeeOther || strings.Contains(rec.Header().Get("Location"), "ERROR") {
		t.Fatalf("POST %s (register) = %d %q, body=%s", form.action, rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}

	// --- login ---
	body = getBody(t, handler, "/login")
	form = findForm(t, scrapeForms(body), "/login")
	rec = postForm(t, handler, form.action, fill(form, nil, map[string]string{
		"email": "formtester@example.com",
	}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s (login) = %d, body=%s", form.action, rec.Code, rec.Body.String())
	}
	sentMail := mail.last(t)
	rawToken := tokenFromMail(t, sentMail.Body)

	verifyReq := httptest.NewRequest(http.MethodGet, "/login/verify?token="+rawToken, nil)
	verifyRec := httptest.NewRecorder()
	handler.ServeHTTP(verifyRec, verifyReq)
	if verifyRec.Code != http.StatusSeeOther {
		t.Fatalf("GET /login/verify = %d, want %d", verifyRec.Code, http.StatusSeeOther)
	}
	cookies := verifyRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login/verify did not set a session cookie")
	}

	authedGet := func(path string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want %d, body=%s", path, rec.Code, http.StatusOK, rec.Body.String())
		}
		body := rec.Body.String()
		assertFormReferencesResolve(t, path, body)
		return body
	}
	authedPost := func(path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	archID, bitID := seedXArch(t, srv)

	// --- bot/new ---
	body = authedGet("/bot/new")
	form = findForm(t, scrapeForms(body), "/bot/new")
	rec = authedPost(form.action, fill(form,
		map[string]bool{fmt.Sprintf("arch-%d", archID): true, fmt.Sprintf("bit-%d", bitID): true},
		map[string]string{"name": "formbot", "source": "nop"},
	))
	if rec.Code != http.StatusSeeOther || strings.Contains(rec.Header().Get("Location"), "res=ERROR") {
		t.Fatalf("POST %s (bot/new) = %d %q, body=%s", form.action, rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}

	bots, err := srv.Store.GetAllBots()
	if err != nil || len(bots) == 0 {
		t.Fatalf("GetAllBots() = %+v, %v", bots, err)
	}
	botID := bots[len(bots)-1].ID

	// --- battle/new ---
	body = authedGet("/battle/new")
	form = findForm(t, scrapeForms(body), "/battle/new")
	rec = authedPost(form.action, fill(form,
		map[string]bool{fmt.Sprintf("arch-%d", archID): true, fmt.Sprintf("bit-%d", bitID): true},
		map[string]string{"name": "formbattle", "arena-size": "4096", "max-rounds": "10"},
	))
	if rec.Code != http.StatusSeeOther || strings.Contains(rec.Header().Get("Location"), "res=ERROR") {
		t.Fatalf("POST %s (battle/new) = %d %q, body=%s", form.action, rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}

	battles, err := srv.Store.GetAllBattles()
	if err != nil || len(battles) == 0 {
		t.Fatalf("GetAllBattles() = %+v, %v", battles, err)
	}
	battleID := battles[len(battles)-1].ID

	// --- battle/{id} submit-bot form ---
	body = authedGet(fmt.Sprintf("/battle/%d", battleID))
	forms := scrapeForms(body)
	form = findForm(t, forms, fmt.Sprintf("/battle/%d/submit", battleID))
	rec = authedPost(form.action, fill(form, map[string]bool{fmt.Sprintf("bot-%d", botID): true}, nil))
	if rec.Code != http.StatusSeeOther || strings.Contains(rec.Header().Get("Location"), "ERROR") {
		t.Fatalf("POST %s (battle submit) = %d %q, body=%s", form.action, rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}

	// --- battle/{id} settings form ---
	form = findForm(t, forms, fmt.Sprintf("/battle/%d", battleID))
	rec = authedPost(form.action, fill(form,
		map[string]bool{fmt.Sprintf("arch-%d", archID): true, fmt.Sprintf("bit-%d", bitID): true},
		map[string]string{"name": "formbattle-renamed", "arena-size": "4096"},
	))
	if rec.Code != http.StatusSeeOther || strings.Contains(rec.Header().Get("Location"), "ERROR") {
		t.Fatalf("POST %s (battle settings) = %d %q, body=%s", form.action, rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}

	// --- bot/{id} edit form ---
	body = authedGet(fmt.Sprintf("/bot/%d", botID))
	form = findForm(t, scrapeForms(body), fmt.Sprintf("/bot/%d", botID))
	rec = authedPost(form.action, fill(form,
		map[string]bool{fmt.Sprintf("arch-%d", archID): true, fmt.Sprintf("bit-%d", bitID): true},
		map[string]string{"name": "formbot-renamed", "source": "nop"},
	))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s (bot edit) = %d, body=%s", form.action, rec.Code, rec.Body.String())
	}

	// --- profile form ---
	users, err := srv.Store.GetAllUsers()
	if err != nil {
		t.Fatalf("GetAllUsers() error = %v", err)
	}
	var formtesterID int
	for _, u := range users {
		if u.Name == "formtester" {
			formtesterID = u.ID
		}
	}
	if formtesterID == 0 {
		t.Fatal("formtester user not found")
	}
	body = authedGet(fmt.Sprintf("/user/%d/profile", formtesterID))
	form = findForm(t, scrapeForms(body), fmt.Sprintf("/user/%d/profile", formtesterID))
	rec = authedPost(form.action, fill(form, nil, map[string]string{
		"username": "formtester",
		"email":    "formtester-updated@example.com",
	}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s (profile) = %d, body=%s", form.action, rec.Code, rec.Body.String())
	}
}

// TestBattleOwnerWithNoBotsCanRunAndDelete is a direct regression test for
// a real incident: the battle page's "Run Battle"/"Delete" buttons target
// <form id="run">/<form id="delete"> via the HTML form="..." attribute, but
// those forms used to be declared inside {{ if .myBots }}. An owner who
// owns the battle but not any bots - a perfectly normal case, e.g. running
// a battle between two other people's bots - got a page where those buttons
// were visibly present and clickable but silently did nothing: the target
// form didn't exist, so no request was ever sent.
//
// This only reproduces when the viewer has zero bots of their own, which is
// why TestEveryFormSubmitsSuccessfully (which creates a bot before ever
// looking at a battle page) couldn't have caught it - the gap in coverage
// was the scenario itself, not a missing assertion.
func TestBattleOwnerWithNoBotsCanRunAndDelete(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.DevMode = true
	srv.OpenR2 = func(string) (r2client.Pipe, error) { return fakeR2{}, nil }
	handler := srv.Routes()

	// DevMode's fixed dev user owns the battle below and, deliberately,
	// never owns a bot anywhere in this test.
	rec := postForm(t, handler, "/battle/new", "name=ownerbattle&arena-size=4096&max-rounds=4")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /battle/new = %d, body=%s", rec.Code, rec.Body.String())
	}
	battles, err := srv.Store.GetAllBattles()
	if err != nil || len(battles) != 1 {
		t.Fatalf("GetAllBattles() = %+v, %v, want exactly 1", battles, err)
	}
	battleID := battles[0].ID

	user, ok := srv.devUser()
	if !ok {
		t.Fatal("devUser() failed")
	}
	if bots, err := srv.Store.GetUserBotsByID(user.ID); err != nil || len(bots) != 0 {
		t.Fatalf("test setup invariant broken: dev user has bots = %+v, %v, want none", bots, err)
	}

	// Someone else's bot, submitted to the battle the dev user owns - a
	// perfectly normal "I'm running a battle between other people's bots"
	// setup, and the exact scenario that was broken.
	archID, bitID := seedXArch(t, srv)
	otherBotID, err := srv.Store.InsertBot(model.Bot{Name: "someone-elses-bot", Source: "nop"})
	if err != nil {
		t.Fatalf("InsertBot() error = %v", err)
	}
	if err := srv.Store.LinkArchIDsToBot(otherBotID, []int{archID}); err != nil {
		t.Fatalf("LinkArchIDsToBot() error = %v", err)
	}
	if err := srv.Store.LinkBitIDsToBot(otherBotID, []int{bitID}); err != nil {
		t.Fatalf("LinkBitIDsToBot() error = %v", err)
	}
	if err := srv.Store.LinkBotBattle(otherBotID, battleID); err != nil {
		t.Fatalf("LinkBotBattle() error = %v", err)
	}

	body := getBody(t, handler, fmt.Sprintf("/battle/%d", battleID))
	// assertFormReferencesResolve (via getBody) already fails the test if
	// "Run Battle"/"Delete" reference a form id missing from this render;
	// additionally drive an actual POST through the scraped form to prove
	// the request really reaches the handler end to end.
	form := findForm(t, scrapeForms(body), fmt.Sprintf("/battle/%d/run", battleID))
	rec = postForm(t, handler, form.action, fill(form, nil, nil))
	// "No bots submitted" is also a 303, so the status code alone can't
	// distinguish that from success - check the redirect target too.
	if rec.Code != http.StatusSeeOther || strings.Contains(rec.Header().Get("Location"), "ERROR") {
		t.Fatalf("POST %s (run, owner with no bots) = %d %q, body=%s", form.action, rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
}
