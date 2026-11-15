package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.emile.space/r2wars-web/internal/r2client"
)

// fakeR2 is a scripted stand-in for a real radare2 process (see
// internal/battle's identical fake for why): it lets the whole HTTP stack,
// including assembling a bot and running a battle, be driven end to end
// without radare2 installed.
type fakeR2 struct{}

func (fakeR2) Cmd(input string) (string, error) {
	if strings.HasPrefix(input, "rasm2") && !strings.Contains(input, "-D") {
		return "90909090", nil // fake assembled bytecode
	}
	if input == "?v theend" {
		return "0x0", nil // never trips the end condition; run to max-rounds
	}
	return "ok", nil
}

func (fakeR2) Close() error { return nil }

// TestFullOfflineUserJourney drives the entire app - as an anonymous HTTP
// client, not a Go-level unit test - through DevMode (no login) and a fake
// r2 (no radare2 installed): create a bot, assemble/view it, create a
// battle, submit the bot, run the battle, and see the result. This is the
// "can we test the whole thing end to end offline" harness: nothing here
// touches a network, a real mail server, or a real radare2 binary.
func TestFullOfflineUserJourney(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.DevMode = true
	srv.OpenR2 = func(string) (r2client.Pipe, error) { return fakeR2{}, nil }
	handler := srv.Routes()

	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// Every normally-authed page is reachable with no session at all.
	for _, path := range []string{"/", "/bot", "/bot/new", "/battle", "/battle/new"} {
		if rec := get(path); rec.Code != http.StatusOK {
			t.Fatalf("GET %s in DevMode = %d, want %d, body=%s", path, rec.Code, http.StatusOK, rec.Body.String())
		}
	}

	archID, bitID := seedXArch(t, srv)

	// Create a bot.
	rec := postForm(t, handler, "/bot/new", fmt.Sprintf(
		"name=amaterasu&source=nop&arch-%d=on&bit-%d=on", archID, bitID))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/bot" {
		t.Fatalf("POST /bot/new = %d %q, want redirect to /bot", rec.Code, rec.Header().Get("Location"))
	}

	bots, err := srv.Store.GetAllBots()
	if err != nil {
		t.Fatalf("GetAllBots() error = %v", err)
	}
	if len(bots) != 1 {
		t.Fatalf("GetAllBots() = %+v, want exactly 1 bot", bots)
	}
	botID := bots[0].ID

	// The bot's own page assembles/disassembles it via the fake r2 - this is
	// the path that used to require a real radare2 binary on $PATH.
	if rec := get(fmt.Sprintf("/bot/%d", botID)); rec.Code != http.StatusOK {
		t.Fatalf("GET /bot/%d = %d, want %d, body=%s", botID, rec.Code, http.StatusOK, rec.Body.String())
	} else if !strings.Contains(rec.Body.String(), "90909090") {
		t.Fatalf("GET /bot/%d did not render the (fake) assembled bytecode", botID)
	}

	// Create a battle allowing that arch/bit.
	rec = postForm(t, handler, "/battle/new", fmt.Sprintf(
		"name=testbattle&arena-size=4096&max-rounds=4&arch-%d=on&bit-%d=on", archID, bitID))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/battle" {
		t.Fatalf("POST /battle/new = %d %q, want redirect to /battle", rec.Code, rec.Header().Get("Location"))
	}

	battles, err := srv.Store.GetAllBattles()
	if err != nil {
		t.Fatalf("GetAllBattles() error = %v", err)
	}
	if len(battles) != 1 {
		t.Fatalf("GetAllBattles() = %+v, want exactly 1 battle", battles)
	}
	battleID := battles[0].ID

	// Submit the bot to the battle.
	rec = postForm(t, handler, fmt.Sprintf("/battle/%d/submit", battleID), fmt.Sprintf("bot-%d=on", botID))
	wantLoc := fmt.Sprintf("/battle/%d?res=Success!", battleID)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != wantLoc {
		t.Fatalf("POST /battle/%d/submit = %d %q, want redirect to %q, body=%s", battleID, rec.Code, rec.Header().Get("Location"), wantLoc, rec.Body.String())
	}

	// Run it - this is the step that used to require a real r2/rasm2 binary.
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/battle/%d/run", battleID), nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	wantLoc = fmt.Sprintf("/battle/%d?res=Success!#output", battleID)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != wantLoc {
		t.Fatalf("POST /battle/%d/run = %d %q, want redirect to %q, body=%s", battleID, rec.Code, rec.Header().Get("Location"), wantLoc, rec.Body.String())
	}

	// The battle page now shows the run's output.
	rec = get(fmt.Sprintf("/battle/%d", battleID))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /battle/%d after run = %d, want %d", battleID, rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "ROUND") {
		t.Fatalf("GET /battle/%d after run did not render battle output", battleID)
	}
}
