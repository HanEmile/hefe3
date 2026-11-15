package web

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"git.emile.space/r2wars-web/internal/model"
	"git.emile.space/r2wars-web/internal/r2client"
)

func (s *Server) botsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
		return
	}

	data := s.newPageData()
	data["pagelink1"] = Link{Name: "bot", Target: "/bot"}
	data["pagelink1options"] = []Link{
		{Name: "user", Target: "/user"},
		{Name: "battle", Target: "/battle"},
	}
	data["pagelinknext"] = []Link{
		{Name: "new", Target: "/new"},
	}

	user, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	data["user"] = user

	bots, err := s.Store.GetAllBotsWithUsers()
	if err != nil {
		log.Println(err)
		data["err"] = "Could not fetch bots"
	}
	data["bots"] = bots

	s.render(w, "bots", data)
}

func (s *Server) botSingleHandler(w http.ResponseWriter, r *http.Request) {
	botID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "400 - Invalid bot id", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		data := s.newPageData()
		data["pagelink1"] = Link{Name: "bot", Target: "/bot"}
		data["pagelink1options"] = []Link{
			{Name: "user", Target: "/user"},
			{Name: "battle", Target: "/battle"},
		}
		if msg := r.URL.Query().Get("res"); msg != "" {
			data["res"] = msg
		}

		viewer, ok := s.currentUser(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		data["user"] = viewer

		bot, err := s.Store.GetBotByID(botID)
		if err != nil {
			redirectWithError(w, r, err, "/bot?res=%s", "Could not find that bot")
			return
		}
		data["bot"] = bot

		if len(bot.Archs) > 0 && len(bot.Bits) > 0 {
			if bytecode, disasm, err := s.assembleAndDisassemble(bot.Source, bot.Archs[0].Name, bot.Bits[0].Name); err != nil {
				log.Println(err)
				data["err"] = "Error assembling/disassembling the bot"
			} else {
				data["bytecode"] = bytecode
				data["disasm"] = disasm
			}
		}

		data["pagelink2"] = Link{Name: bot.Name, Target: fmt.Sprintf("/%d", bot.ID)}

		allBots, err := s.Store.GetAllBots()
		if err != nil {
			log.Println(err)
		}
		var opts []Link
		for _, b := range allBots {
			if b.ID != botID {
				opts = append(opts, Link{Name: b.Name, Target: fmt.Sprintf("/%d", b.ID)})
			}
		}
		data["pagelink2options"] = opts

		for _, u := range bot.Users {
			if u.ID == viewer.ID {
				data["editable"] = true
			}
		}

		archs, err := s.Store.GetAllArchs()
		if err != nil {
			log.Println(err)
			data["err"] = "Could not fetch the archs"
		}
		for i, a := range archs {
			for _, b := range bot.Archs {
				if a.ID == b.ID {
					archs[i].Enabled = true
				}
			}
		}
		data["archs"] = archs

		bits, err := s.Store.GetAllBits()
		if err != nil {
			log.Println(err)
			data["err"] = "Could not fetch the bits"
		}
		for i, a := range bits {
			for _, b := range bot.Bits {
				if a.ID == b.ID {
					bits[i].Enabled = true
				}
			}
		}
		data["bits"] = bits

		s.render(w, "botSingle", data)

	case http.MethodPost:
		username, ok := s.sessionUsername(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		requestingUser, err := s.Store.GetUserByUsername(username)
		if err != nil {
			redirectWithError(w, r, err, fmt.Sprintf("/bot/%d?res=%%s", botID), "Could not identify you")
			return
		}

		origBot, err := s.Store.GetBotByID(botID)
		if err != nil {
			redirectWithError(w, r, err, fmt.Sprintf("/bot/%d?res=%%s", botID), "Could not find that bot")
			return
		}

		allowedToEdit := false
		for _, u := range origBot.Users {
			if u.ID == requestingUser.ID {
				allowedToEdit = true
			}
		}
		if !allowedToEdit {
			http.Error(w, "403 - You aren't allowed to edit this bot", http.StatusForbidden)
			return
		}

		r.ParseForm()
		name := r.Form.Get("name")
		source := r.Form.Get("source")

		archIDs, bitIDs, err := parseArchBitSelection(r.Form)
		if err != nil {
			redirectWithError(w, r, err, fmt.Sprintf("/bot/%d?res=%%s", botID), "Invalid arch/bit selection")
			return
		}
		if msg, ok := validateSingleArchBit(archIDs, bitIDs); !ok {
			http.Redirect(w, r, fmt.Sprintf("/bot/%d?res=%s", botID, msg), http.StatusSeeOther)
			return
		}

		if err := s.Store.LinkArchIDsToBot(botID, archIDs); err != nil {
			redirectWithError(w, r, err, fmt.Sprintf("/bot/%d?res=%%s", botID), "Could not update the bot's archs")
			return
		}
		if err := s.Store.LinkBitIDsToBot(botID, bitIDs); err != nil {
			redirectWithError(w, r, err, fmt.Sprintf("/bot/%d?res=%%s", botID), "Could not update the bot's bits")
			return
		}

		if name != "" && source != "" {
			origBot.Name = name
			origBot.Source = source
			if err := s.Store.UpdateBot(origBot); err != nil {
				redirectWithError(w, r, err, fmt.Sprintf("/bot/%d?res=%%s", botID), "Could not save the bot")
				return
			}
		}

		http.Redirect(w, r, fmt.Sprintf("/bot/%d", botID), http.StatusSeeOther)

	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}

func (s *Server) botNewHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data := s.newPageData()
		username, ok := s.sessionUsername(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		data["pagelink1"] = Link{Name: "bot", Target: "/bot"}
		data["pagelink1options"] = []Link{
			{Name: "user", Target: "/user"},
			{Name: "battle", Target: "/battle"},
		}
		data["pagelink2"] = Link{Name: "new", Target: "/new"}
		data["pagelink2options"] = []Link{
			{Name: "list", Target: ""},
		}
		if msg := r.URL.Query().Get("res"); msg != "" {
			data["res"] = msg
		}

		user, err := s.Store.GetUserByUsername(username)
		if err != nil {
			log.Println(err)
			data["err"] = "Could not fetch the user"
		} else {
			data["user"] = user
		}

		archs, err := s.Store.GetAllArchs()
		if err != nil {
			log.Println(err)
			data["err"] = "Could not fetch the archs"
		} else {
			data["archs"] = archs
		}

		bits, err := s.Store.GetAllBits()
		if err != nil {
			log.Println(err)
			data["err"] = "Could not fetch the bits"
		} else {
			data["bits"] = bits
		}

		s.render(w, "botNew", data)

	case http.MethodPost:
		username, ok := s.sessionUsername(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		r.ParseForm()
		name := r.Form.Get("name")
		source := r.Form.Get("source")

		if name == "" {
			http.Redirect(w, r, "/bot/new?res=ERROR:+Please+provide+a+name", http.StatusSeeOther)
			return
		}
		if source == "" {
			http.Redirect(w, r, "/bot/new?res=ERROR:+Please+provide+some+source", http.StatusSeeOther)
			return
		}

		archIDs, bitIDs, err := parseArchBitSelection(r.Form)
		if err != nil {
			redirectWithError(w, r, err, "/bot/new?res=%s", "Invalid arch/bit selection")
			return
		}
		if msg, ok := validateSingleArchBit(archIDs, bitIDs); !ok {
			http.Redirect(w, r, fmt.Sprintf("/bot/new?res=%s", msg), http.StatusSeeOther)
			return
		}

		botID, err := s.Store.InsertBot(model.Bot{Name: name, Source: source})
		if err != nil {
			redirectWithError(w, r, err, "/bot/new?res=%s", "Could not create bot")
			return
		}

		if err := s.Store.LinkUserBot(username, botID); err != nil {
			redirectWithError(w, r, err, "/bot/new?res=%s", "Could not link the bot to your account")
			return
		}
		if err := s.Store.LinkArchIDsToBot(botID, archIDs); err != nil {
			redirectWithError(w, r, err, "/bot/new?res=%s", "Could not set the bot's archs")
			return
		}
		if err := s.Store.LinkBitIDsToBot(botID, bitIDs); err != nil {
			redirectWithError(w, r, err, "/bot/new?res=%s", "Could not set the bot's bits")
			return
		}

		http.Redirect(w, r, "/bot", http.StatusSeeOther)

	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}

// assembleAndDisassemble runs source through `rasm2` and back, for display
// on the bot page. It starts its own short-lived, input-less r2 process and
// always closes it - a failure to start r2 now returns an error instead of
// panicking the whole server.
func (s *Server) assembleAndDisassemble(source, archName, bitsName string) (bytecode, disasm string, err error) {
	p, err := s.OpenR2("--")
	if err != nil {
		return "", "", err
	}
	defer p.Close()

	src := strings.ReplaceAll(source, "\r\n", "; ")
	asmCmd := fmt.Sprintf("rasm2 -a %s -b %s \"%s\"", archName, bitsName, src)
	bytecode, err = r2client.Cmd(p, asmCmd)
	if err != nil {
		return "", "", fmt.Errorf("assembling: %w", err)
	}

	disasmCmd := fmt.Sprintf("rasm2 -a %s -b %s -D %s", archName, bitsName, bytecode)
	disasm, err = r2client.Cmd(p, disasmCmd)
	if err != nil {
		return "", "", fmt.Errorf("disassembling: %w", err)
	}
	return bytecode, disasm, nil
}
