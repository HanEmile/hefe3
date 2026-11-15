package web

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strconv"

	"git.emile.space/r2wars-web/internal/engine"
	"git.emile.space/r2wars-web/internal/model"
	"github.com/gorilla/mux"
)

func (s *Server) battlesHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data := s.baseData(r)
		data["pagelink1"] = Link{Name: "battle", Target: "/battle"}
		data["pagelink1options"] = []Link{
			{Name: "bot", Target: "/bot"},
			{Name: "user", Target: "/user"},
		}
		data["pagelinknext"] = []Link{
			{Name: "new", Target: "/new"},
			{Name: "quick", Target: "/quick"},
		}

		if _, ok := s.currentUser(r); !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		battles, _ := s.Store.AllBattles()
		data["battles"] = battles

		s.render(w, "battles", data)
	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}

func (s *Server) battleNewHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data := s.baseData(r)
		data["pagelink1"] = Link{Name: "battle", Target: "/battle"}
		data["pagelink1options"] = []Link{
			{Name: "user", Target: "/user"},
			{Name: "bot", Target: "/bot"},
		}
		data["pagelink2"] = Link{Name: "new", Target: "/new"}
		data["pagelink2options"] = []Link{
			{Name: "list", Target: ""},
			{Name: "quick", Target: "/quick"},
		}

		if queryres := r.URL.Query().Get("err"); queryres != "" {
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
		users, err := s.Store.AllUsers()
		if err != nil {
			data["err"] = "Could not fetch all users"
		} else {
			data["users"] = users
		}

		s.render(w, "battleNew", data)

	case http.MethodPost:
		user, ok := s.currentUser(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		r.ParseForm()
		name := r.Form.Get("name")
		arenasize, err := strconv.Atoi(r.Form.Get("arena-size"))
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/battle/new?res=%s", "ERROR: Invalid arena size"), http.StatusSeeOther)
			return
		}
		maxrounds, err := strconv.Atoi(r.Form.Get("max-rounds"))
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/battle/new?res=%s", "ERROR: Invalid max rounds"), http.StatusSeeOther)
			return
		}

		public := r.Form.Get("public") == "on"

		archIDs, err := formIDsWithPrefix(r.Form, "arch-")
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/battle/new?res=%s", "ERROR: Invalid arch id"), http.StatusSeeOther)
			return
		}
		bitIDs, err := formIDsWithPrefix(r.Form, "bit-")
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/battle/new?res=%s", "ERROR: Invalid bit id"), http.StatusSeeOther)
			return
		}

		if name == "" {
			http.Redirect(w, r, fmt.Sprintf("/battle/new?res=%s", "ERROR: Please provide a name"), http.StatusSeeOther)
			return
		}

		newbattle := model.Battle{
			Name:      name,
			Public:    public,
			MaxRounds: maxrounds,
			ArenaSize: arenasize,
		}
		battleid, err := s.Store.InsertBattle(newbattle, user.ID)
		if err != nil {
			log.Println(err)
			http.Redirect(w, r, fmt.Sprintf("/battle/new?res=%s", "ERROR: Could not create due to internal reasons"), http.StatusSeeOther)
			return
		}

		if err := s.Store.LinkBattleArchs(battleid, archIDs); err != nil {
			log.Println(err)
			http.Redirect(w, r, fmt.Sprintf("/battle/new?res=%s", "ERROR: Could not create due to internal reasons"), http.StatusSeeOther)
			return
		}
		if err := s.Store.LinkBattleBits(battleid, bitIDs); err != nil {
			log.Println(err)
			http.Redirect(w, r, fmt.Sprintf("/battle/new?res=%s", "ERROR: Could not create due to internal reasons"), http.StatusSeeOther)
			return
		}
		if err := s.Store.LinkBattleOwners(battleid, []int{user.ID}); err != nil {
			log.Println(err)
			http.Redirect(w, r, fmt.Sprintf("/battle/new?res=%s", "ERROR: Could not create due to internal reasons"), http.StatusSeeOther)
			return
		}

		http.Redirect(w, r, "/battle", http.StatusSeeOther)

	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}

func (s *Server) battleQuickHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data := s.baseData(r)
		data["pagelink1"] = Link{Name: "battle", Target: "/battle"}
		data["pagelink1options"] = []Link{
			{Name: "user", Target: "/user"},
			{Name: "bot", Target: "/bot"},
		}
		data["pagelink2"] = Link{Name: "quick", Target: "/quick"}
		data["pagelink2options"] = []Link{
			{Name: "new", Target: "/new"},
			{Name: "list", Target: ""},
		}

		if queryres := r.URL.Query().Get("err"); queryres != "" {
			data["res"] = queryres
		}

		if _, ok := s.currentUser(r); !ok {
			data["err"] = "Could not fetch the user"
		}

		bots, _ := s.Store.AllBotsWithUsers()
		data["bots"] = bots

		s.render(w, "battleQuick", data)

	case http.MethodPost:
		user, ok := s.currentUser(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		r.ParseForm()
		public := r.Form.Get("public") == "on"

		arenasize, err := strconv.Atoi(r.Form.Get("arena-size"))
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/battle/new?res=%s", "ERROR: Invalid arena size"), http.StatusSeeOther)
			return
		}
		maxrounds, err := strconv.Atoi(r.Form.Get("max-rounds"))
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/battle/new?res=%s", "ERROR: Invalid max rounds"), http.StatusSeeOther)
			return
		}

		// bot ids are collected from the form but, as in the original, the quick
		// battle currently only creates the battle and redirects to it.
		_, _ = formIDsWithPrefix(r.Form, "bot-")

		newbattle := model.Battle{
			Name:      fmt.Sprintf("quick-%d", rand.Intn(10000)),
			Public:    public,
			MaxRounds: maxrounds,
			ArenaSize: arenasize,
		}
		battleid, err := s.Store.InsertBattle(newbattle, user.ID)
		if err != nil {
			log.Println(err)
			http.Redirect(w, r, fmt.Sprintf("/battle/quick?res=%s", "ERROR: Could not create due to internal reasons"), http.StatusSeeOther)
			return
		}

		http.Redirect(w, r, fmt.Sprintf("/battle/%d", battleid), http.StatusSeeOther)

	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}

func (s *Server) battleSingleHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	battleid, err := strconv.Atoi(vars["id"])
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - Invalid battle id"))
		return
	}

	redirTarget := fmt.Sprintf("/battle/%d?res=%%s", battleid)

	switch r.Method {
	case http.MethodGet:
		data := s.baseData(r)
		data["pagelink1"] = Link{"battle", "/battle"}
		data["pagelink1options"] = []Link{
			{Name: "user", Target: "/user"},
			{Name: "bot", Target: "/bot"},
		}

		if queryres := r.URL.Query().Get("res"); queryres != "" {
			data["res"] = queryres
		}

		username, ok := s.currentUsername(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		viewer, err := s.Store.UserByName(username)
		if err != nil {
			s.logAndRedir(w, r, err, redirTarget, "Could not get the id for your username")
			return
		}
		data["user"] = viewer

		users, err := s.Store.AllUsers()
		if err != nil {
			s.logAndRedir(w, r, err, redirTarget, "Could not get the users")
			return
		}
		data["users"] = users

		battle, err := s.Store.BattleByID(battleid)
		if err != nil {
			s.logAndRedir(w, r, err, redirTarget, "Could not get the battle given the id provided")
			return
		}
		data["battle"] = battle
		data["botAmount"] = len(battle.Bots)
		data["battleCount"] = (len(battle.Bots) * len(battle.Bots)) * 2

		data["pagelink2"] = Link{battle.Name, fmt.Sprintf("/%d", battle.ID)}
		allBattles, _ := s.Store.AllBattles()
		var opts []Link
		for _, b := range allBattles {
			opts = append(opts, Link{Name: b.Name, Target: fmt.Sprintf("/%d", b.ID)})
		}
		data["pagelink2options"] = opts

		myBots, err := s.Store.BotsForUsername(username)
		if err != nil {
			s.logAndRedir(w, r, err, redirTarget, "Could not get your bots")
			return
		}
		data["myBots"] = myBots

		archs, err := s.Store.AllArchs()
		if err != nil {
			s.logAndRedir(w, r, err, redirTarget, "Could not get your bots")
			return
		}
		for i, a := range archs {
			for _, b := range battle.Archs {
				if a.ID == b.ID {
					archs[i].Enabled = true
				}
			}
		}
		data["archs"] = archs

		bits, err := s.Store.AllBits()
		if err != nil {
			s.logAndRedir(w, r, err, redirTarget, "Could not fetch the bits")
			return
		}
		for i, a := range bits {
			for _, b := range battle.Bits {
				if a.ID == b.ID {
					bits[i].Enabled = true
				}
			}
		}
		data["bits"] = bits

		editable := false
		for _, owner := range battle.Owners {
			if owner.ID == viewer.ID {
				editable = true
			}
		}
		if editable {
			data["editable"] = true
		}

		s.render(w, "battleSingle", data)

	case http.MethodPost:
		user, ok := s.currentUser(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		r.ParseForm()
		formName := r.Form.Get("name")
		arenasize, err := strconv.Atoi(r.Form.Get("arena-size"))
		if err != nil {
			s.logAndRedir(w, r, err, redirTarget, "Invalid Arena size")
			return
		}
		public := r.Form.Get("public") == "on"

		archIDs, err := formIDsWithPrefix(r.Form, "arch-")
		if err != nil {
			s.logAndRedir(w, r, err, redirTarget, "Invalid Arch ID")
			return
		}
		bitIDs, err := formIDsWithPrefix(r.Form, "bit-")
		if err != nil {
			s.logAndRedir(w, r, err, redirTarget, "Invalid Bit ID")
			return
		}
		ownerIDs, err := formIDsWithPrefix(r.Form, "owner-")
		if err != nil {
			s.logAndRedir(w, r, err, redirTarget, "Invalid Owner ID")
			return
		}

		// the user requesting changes must already be among the owners
		allowedToEdit := false
		for _, ownerID := range ownerIDs {
			if user.ID == ownerID {
				allowedToEdit = true
			}
		}
		if !allowedToEdit {
			s.logAndRedir(w, r, nil, redirTarget+"#settings", "You aren't an owner and aren't allowed to edit the settings")
			return
		}

		if err := s.Store.LinkBattleArchs(battleid, archIDs); err != nil {
			s.logAndRedir(w, r, err, redirTarget+"#settings", "Could not link arch id to battle")
			return
		}
		if err := s.Store.LinkBattleBits(battleid, bitIDs); err != nil {
			s.logAndRedir(w, r, err, redirTarget+"#settings", "Could not link bit id to battle")
			return
		}
		if err := s.Store.LinkBattleOwners(battleid, ownerIDs); err != nil {
			s.logAndRedir(w, r, err, redirTarget+"#settings", "Could not link owner id to battle")
			return
		}

		newBattle := model.Battle{ID: battleid, Name: formName, Owners: []model.User{user}, Public: public, MaxRounds: 100, ArenaSize: arenasize}
		if err := s.Store.UpdateBattle(newBattle); err != nil {
			s.logAndRedir(w, r, err, redirTarget+"#settings", "Could not insert battle into db")
			return
		}

		http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=Success!#settings", battleid), http.StatusSeeOther)

	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}

func (s *Server) battleSubmitHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	battleid, err := strconv.Atoi(vars["id"])
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - Invalid battle id"))
		return
	}

	switch r.Method {
	case http.MethodPost:
		r.ParseForm()

		username, ok := s.currentUsername(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		user, err := s.Store.UserByName(username)
		if err != nil {
			s.logAndRedir(w, r, err, fmt.Sprintf("/battle/%d?res=%%s", battleid), "Could not get the id for your username")
			return
		}

		botIDs, err := formIDsWithPrefix(r.Form, "bot-")
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=%s", battleid, "ERROR: Invalid bot supplied"), http.StatusSeeOther)
			return
		}

		battle, err := s.Store.BattleByID(battleid)
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=%s", battleid, "ERROR: Couln't get the battle with the given id"), http.StatusSeeOther)
			return
		}

		// clear the user's existing bots from the battle before (re)adding
		s.Store.UnlinkUserBotsFromBattle(user.ID, battleid)

		for _, id := range botIDs {
			bot, err := s.Store.BotByID(id)
			if err != nil {
				http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=%s", battleid, fmt.Sprintf("ERROR: Couldn't get bot with id %d", id)), http.StatusSeeOther)
				return
			}

			archValid := false
			for _, battleArch := range battle.Archs {
				for _, botArch := range bot.Archs {
					if battleArch.ID == botArch.ID {
						archValid = true
					}
				}
			}
			bitValid := false
			for _, battleBit := range battle.Bits {
				for _, botBit := range bot.Bits {
					if battleBit.ID == botBit.ID {
						bitValid = true
					}
				}
			}

			if archValid && bitValid {
				log.Printf("arch and bit valid, adding bot with id %d to battle with id %d\n", id, battleid)
				s.Store.LinkBattleBot(id, battleid)
			} else {
				if !archValid {
					http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=%s", battleid, "Bot has an invalid architecture!"), http.StatusSeeOther)
					return
				}
				if !bitValid {
					http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=%s", battleid, "Bot has an invalid 'bit-ness'!"), http.StatusSeeOther)
					return
				}
			}
		}

		http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=%s", battleid, "Success!"), http.StatusSeeOther)
	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}

// battleRunHandler runs the battle using the engine package. It deep-fetches the
// battle and each linked bot (so Source/Archs/Bits are populated), builds a
// model.Battle with fully-populated bots, derives an engine.Spec and runs it in
// a fresh malloc:// arena. This now supports N bots, unlike the original's
// hardcoded-two-bot inline engine.
func (s *Server) battleRunHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	battleid, err := strconv.Atoi(vars["id"])
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - Invalid battle id"))
		return
	}

	redirTarget := fmt.Sprintf("/battle/%d?res=%%s", battleid)

	switch r.Method {
	case http.MethodPost:
		r.ParseForm()
		log.Printf("running the battle with the id %d", battleid)

		username, ok := s.currentUsername(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if _, err := s.Store.UserByName(username); err != nil {
			s.logAndRedir(w, r, err, redirTarget, "Could not get the id for your username")
			return
		}

		battle, err := s.Store.BattleByID(battleid)
		if err != nil {
			s.logAndRedir(w, r, err, redirTarget, "Could not get the battle given the id provided")
			return
		}

		// deep-fetch every linked bot so its Source/Archs/Bits are populated
		// (BattleByID only fills bot id+name).
		deepBots := make([]model.Bot, 0, len(battle.Bots))
		for _, b := range battle.Bots {
			bot, err := s.Store.BotByID(b.ID)
			if err != nil {
				s.logAndRedir(w, r, err, redirTarget, "Could not get a bot in the battle")
				return
			}
			deepBots = append(deepBots, bot)
		}
		battle.Bots = deepBots

		spec, err := engine.SpecFromModel(battle)
		if err != nil {
			s.logAndRedir(w, r, err, redirTarget, "Could not build the battle spec")
			return
		}

		r2, err := engine.OpenR2(fmt.Sprintf("malloc://%d", battle.ArenaSize))
		if err != nil {
			s.logAndRedir(w, r, err, redirTarget, "Could not open the arena")
			return
		}
		defer r2.Close()

		res, err := engine.Run(r2, spec)
		if err != nil {
			s.logAndRedir(w, r, err, redirTarget, "Error running the battle")
			return
		}

		if err := s.Store.SaveBattleRawOutput(battleid, res.Transcript); err != nil {
			s.logAndRedir(w, r, err, redirTarget, "Could not save the battle output")
			return
		}

		http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=%s#output", battleid, "Success!"), http.StatusSeeOther)
	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}

func (s *Server) battleDeleteHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	battleid, err := strconv.Atoi(vars["id"])
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - Invalid battle id"))
		return
	}

	redirTarget := fmt.Sprintf("/battle/%d?res=%%s", battleid)

	switch r.Method {
	case http.MethodPost: // can't send a DELETE with pure HTML...
		viewer, ok := s.currentUser(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		battle, err := s.Store.BattleByID(battleid)
		if err != nil {
			s.logAndRedir(w, r, err, redirTarget, "Could not get the battle given the id provided")
			return
		}

		ownedByRequester := false
		for _, owner := range battle.Owners {
			if viewer.ID == owner.ID {
				ownedByRequester = true
				break
			}
		}
		if !ownedByRequester {
			s.logAndRedir(w, r, nil, redirTarget, "You aren't in the owners list of the battle, so you can't delete this battle")
			return
		}

		if err := s.Store.DeleteBattle(battleid); err != nil {
			s.logAndRedir(w, r, err, redirTarget, "Could not delete the battle")
			return
		}

		http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=%s", battleid, "Successfully deleted the battle"), http.StatusSeeOther)
	default:
		log.Println("expected POST, got ", r.Method)
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}
