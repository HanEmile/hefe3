package web

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"git.emile.space/r2wars-web/internal/engine"
	"github.com/gorilla/mux"
)

// formIDsWithPrefix collects integer ids from form keys that start with prefix
// (e.g. "arch-3" -> 3). Used for the arch/bit/bot/owner checkbox fields.
func formIDsWithPrefix(form url.Values, prefix string) ([]int, error) {
	var ids []int
	for k := range form {
		if strings.HasPrefix(k, prefix) {
			id, err := strconv.Atoi(strings.TrimPrefix(k, prefix))
			if err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (s *Server) botsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data := s.baseData(r)
		data["pagelink1"] = Link{"bot", "/bot"}
		data["pagelink1options"] = []Link{
			{Name: "user", Target: "/user"},
			{Name: "battle", Target: "/battle"},
		}
		data["pagelinknext"] = []Link{
			{Name: "new", Target: "/new"},
		}

		if _, ok := s.currentUser(r); !ok {
			data["err"] = "Could not fetch the user"
		}

		bots, _ := s.Store.AllBotsWithUsers()
		data["bots"] = bots

		s.render(w, "bots", data)
	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}

func (s *Server) botSingleHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	botid, err := strconv.Atoi(vars["id"])
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - Invalid bot id"))
		return
	}

	switch r.Method {
	case http.MethodGet:
		data := s.baseData(r)
		data["pagelink1"] = Link{"bot", "/bot"}
		data["pagelink1options"] = []Link{
			{Name: "user", Target: "/user"},
			{Name: "battle", Target: "/battle"},
		}

		if queryres := r.URL.Query().Get("res"); queryres != "" {
			data["res"] = queryres
		}

		viewer, ok := s.currentUser(r)
		if !ok {
			data["err"] = "Could not get the id four your username... Please contact an admin"
		}

		bot, err := s.Store.BotByID(botid)
		if err != nil {
			data["err"] = "Could not get the bot"
		}
		data["bot"] = bot

		// assemble + disassemble the bot's source for display via r2.
		if len(bot.Archs) > 0 && len(bot.Bits) > 0 {
			r2, err := engine.OpenR2("--")
			if err != nil {
				data["err"] = "Error opening r2"
			} else {
				defer r2.Close()

				bytecode, err := engine.Assemble(r2, bot.Archs[0].Name, bot.Bits[0].Name, bot.Source)
				if err != nil {
					data["err"] = "Error assembling the bot"
					http.Redirect(w, r, fmt.Sprintf("/bot/%d", botid), http.StatusSeeOther)
					return
				}
				src := strings.ReplaceAll(bot.Source, "\r\n", "; ")
				data["bytecode_r2cmd"] = fmt.Sprintf("rasm2 -a %s -b %s \"%s\"", bot.Archs[0].Name, bot.Bits[0].Name, src)
				data["bytecode"] = bytecode

				disasm, err := engine.Disassemble(r2, bot.Archs[0].Name, bot.Bits[0].Name, bytecode)
				if err != nil {
					data["err"] = "Error disassembling the bot"
					http.Redirect(w, r, fmt.Sprintf("/bot/%d", botid), http.StatusSeeOther)
					return
				}
				data["disasm_r2cmd"] = fmt.Sprintf("rasm2 -a %s -b %s -D %s", bot.Archs[0].Name, bot.Bits[0].Name, bytecode)
				data["disasm"] = disasm
			}
		}

		// breadcrumbs
		data["pagelink2"] = Link{bot.Name, fmt.Sprintf("/%d", bot.ID)}
		allBots, _ := s.Store.AllBots()
		var opts []Link
		for _, b := range allBots {
			if b.ID != botid {
				opts = append(opts, Link{Name: b.Name, Target: fmt.Sprintf("/%d", b.ID)})
			}
		}
		data["pagelink2options"] = opts

		editable := false
		for _, u := range bot.Users {
			if u.ID == viewer.ID {
				editable = true
			}
		}
		if editable {
			data["editable"] = true
		}

		// archs + bits with the Enabled flag set for the ones linked to the bot
		archs, err := s.Store.AllArchs()
		if err != nil {
			data["err"] = "Could not fetch the archs"
		} else {
			for i, a := range archs {
				for _, b := range bot.Archs {
					if a.ID == b.ID {
						archs[i].Enabled = true
					}
				}
			}
			data["archs"] = archs
		}

		bits, err := s.Store.AllBits()
		if err != nil {
			data["err"] = "Could not fetch the bits"
		} else {
			for i, a := range bits {
				for _, b := range bot.Bits {
					if a.ID == b.ID {
						bits[i].Enabled = true
					}
				}
			}
			data["bits"] = bits
		}

		s.render(w, "botSingle", data)

	case http.MethodPost:
		viewer, ok := s.currentUser(r)
		if !ok {
			http.Redirect(w, r, fmt.Sprintf("/bot/%d", botid), http.StatusSeeOther)
			return
		}

		origBot, err := s.Store.BotByID(botid)
		if err != nil {
			log.Println("err: ", err)
			http.Redirect(w, r, fmt.Sprintf("/bot/%d", botid), http.StatusSeeOther)
			return
		}

		allowedToEdit := false
		for _, u := range origBot.Users {
			if u.ID == viewer.ID {
				allowedToEdit = true
			}
		}
		if !allowedToEdit {
			http.Redirect(w, r, fmt.Sprintf("/bot/%d", botid), http.StatusSeeOther)
			return
		}

		r.ParseForm()
		name := r.Form.Get("name")
		source := r.Form.Get("source")

		archIDs, err := formIDsWithPrefix(r.Form, "arch-")
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/bot/%d?res=%s", botid, "ERROR: Invalid arch id"), http.StatusSeeOther)
			return
		}
		bitIDs, err := formIDsWithPrefix(r.Form, "bit-")
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/bot/%d?res=%s", botid, "ERROR: Invalid bit id"), http.StatusSeeOther)
			return
		}

		if msg, ok := validateSingleArchBit(archIDs, bitIDs); !ok {
			http.Redirect(w, r, fmt.Sprintf("/bot/%d?res=%s", botid, msg), http.StatusSeeOther)
			return
		}

		if err := s.Store.LinkBotArchs(botid, archIDs); err != nil {
			log.Println("Error linking the arch ids to the bot: ", err)
			http.Redirect(w, r, fmt.Sprintf("/bot/%d?res=%s", botid, "ERROR: Could not create due to internal reasons"), http.StatusSeeOther)
			return
		}
		if err := s.Store.LinkBotBits(botid, bitIDs); err != nil {
			log.Println("Error linking the bit ids to the bot: ", err)
			http.Redirect(w, r, fmt.Sprintf("/bot/%d?res=%s", botid, "ERROR: Could not create due to internal reasons"), http.StatusSeeOther)
			return
		}

		if name != "" && source != "" {
			if err := s.Store.UpdateBot(botid, name, source); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("500 - Error inserting bot into db"))
				return
			}
		}

		http.Redirect(w, r, fmt.Sprintf("/bot/%d", botid), http.StatusSeeOther)

	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}

// validateSingleArchBit enforces exactly one arch and exactly one bit, matching
// the original bot create/edit checks. Returns an error message and false if
// invalid.
func validateSingleArchBit(archIDs, bitIDs []int) (string, bool) {
	switch {
	case len(archIDs) == 0:
		return "ERROR: Please select an architecture", false
	case len(archIDs) >= 2:
		return "ERROR: Please select ONE architecture", false
	case len(bitIDs) == 0:
		return "ERROR: Please select one of the bits", false
	case len(bitIDs) >= 2:
		return "ERROR: Please select ONE of the bits", false
	}
	return "", true
}

func (s *Server) botNewHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data := s.baseData(r)
		data["pagelink1"] = Link{Name: "bot", Target: "/bot"}
		data["pagelink1options"] = []Link{
			{Name: "user", Target: "/user"},
			{Name: "battle", Target: "/battle"},
		}
		data["pagelink2"] = Link{Name: "new", Target: "/new"}
		data["pagelink2options"] = []Link{
			{Name: "list", Target: ""},
		}

		if queryres := r.URL.Query().Get("res"); queryres != "" {
			data["res"] = queryres
		}

		if _, ok := s.currentUser(r); !ok {
			data["err"] = "Could not fetch the user"
		}

		archs, err := s.Store.AllArchs()
		if err != nil {
			data["err"] = "Could not fetch the archs"
		} else {
			data["archs"] = archs
		}
		bits, err := s.Store.AllBits()
		if err != nil {
			data["err"] = "Could not fetch the bits"
		} else {
			data["bits"] = bits
		}

		s.render(w, "botNew", data)

	case http.MethodPost:
		username, _ := s.currentUsername(r)

		r.ParseForm()
		name := r.Form.Get("name")
		source := r.Form.Get("source")

		if name == "" {
			http.Redirect(w, r, fmt.Sprintf("/bot/new?res=%s", "ERROR: Please provide a name"), http.StatusSeeOther)
			return
		}
		if source == "" {
			http.Redirect(w, r, fmt.Sprintf("/bot/new?res=%s", "ERROR: Please provide some source"), http.StatusSeeOther)
			return
		}

		archIDs, err := formIDsWithPrefix(r.Form, "arch-")
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/bot/new?res=%s", "ERROR: Invalid arch id"), http.StatusSeeOther)
			return
		}
		bitIDs, err := formIDsWithPrefix(r.Form, "bit-")
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/bot/new?res=%s", "ERROR: Invalid bit id"), http.StatusSeeOther)
			return
		}

		if msg, ok := validateSingleArchBit(archIDs, bitIDs); !ok {
			http.Redirect(w, r, fmt.Sprintf("/bot/new?res=%s", msg), http.StatusSeeOther)
			return
		}

		botid, err := s.Store.InsertBot(name, source)
		if err != nil {
			log.Println("Error creating the bot: ", err)
			http.Redirect(w, r, fmt.Sprintf("/bot/new?res=%s", "ERROR: Could not create bot"), http.StatusSeeOther)
			return
		}

		if err := s.Store.LinkUserBot(username, botid); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("500 - Error adding the bot to the user"))
			return
		}

		if err := s.Store.LinkBotArchs(botid, archIDs); err != nil {
			log.Println("Error linking the arch ids to the bot: ", err)
			http.Redirect(w, r, fmt.Sprintf("/bot/new?res=%s", "ERROR: Could not create due to internal reasons"), http.StatusSeeOther)
			return
		}
		if err := s.Store.LinkBotBits(botid, bitIDs); err != nil {
			log.Println("Error linking the bit ids to the bot: ", err)
			http.Redirect(w, r, fmt.Sprintf("/bot/new?res=%s", "ERROR: Could not create due to internal reasons"), http.StatusSeeOther)
			return
		}

		http.Redirect(w, r, "/bot", http.StatusSeeOther)

	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}
