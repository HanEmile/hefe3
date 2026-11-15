package web

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"git.emile.space/r2wars-web/internal/battle"
	"git.emile.space/r2wars-web/internal/model"
)

// Defaults for /battle/quick, which deliberately has no form fields for
// these (matching GetBattleByIDDeep's own COALESCE defaults for battles
// created before these columns existed keeps "quick" and "legacy" battles
// behaving the same way).
const (
	defaultQuickBattleArenaSize = 4096
	defaultQuickBattleMaxRounds = 100
)

func (s *Server) battlesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
		return
	}

	data := s.newPageData()
	data["pagelink1"] = Link{Name: "battle", Target: "/battle"}
	data["pagelink1options"] = []Link{
		{Name: "bot", Target: "/bot"},
		{Name: "user", Target: "/user"},
	}
	data["pagelinknext"] = []Link{
		{Name: "new", Target: "/new"},
		{Name: "quick", Target: "/quick"},
	}

	user, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	data["user"] = user

	battles, err := s.Store.GetAllBattles()
	if err != nil {
		log.Println(err)
		data["err"] = "Could not fetch battles"
	}
	data["battles"] = battles

	s.render(w, "battles", data)
}

func (s *Server) battleNewHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data := s.newPageData()
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
		if msg := r.URL.Query().Get("res"); msg != "" {
			data["res"] = msg
		}

		user, ok := s.currentUser(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		data["user"] = user

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

		users, err := s.Store.GetAllUsers()
		if err != nil {
			log.Println(err)
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
		if name == "" {
			http.Redirect(w, r, "/battle/new?res=ERROR:+Please+provide+a+name", http.StatusSeeOther)
			return
		}

		arenaSize, maxRounds, public, err := parseBattleSettings(r)
		if err != nil {
			redirectWithError(w, r, err, "/battle/new?res=%s", "Invalid arena size or max rounds")
			return
		}

		archIDs, bitIDs, err := parseArchBitSelection(r.Form)
		if err != nil {
			redirectWithError(w, r, err, "/battle/new?res=%s", "Invalid arch/bit selection")
			return
		}

		battleID, err := s.Store.InsertBattle(model.Battle{
			Name:      name,
			Public:    public,
			MaxRounds: maxRounds,
			ArenaSize: arenaSize,
		}, user)
		if err != nil {
			redirectWithError(w, r, err, "/battle/new?res=%s", "Could not create the battle")
			return
		}

		if err := s.Store.LinkArchIDsToBattle(battleID, archIDs); err != nil {
			redirectWithError(w, r, err, "/battle/new?res=%s", "Could not set the battle's archs")
			return
		}
		if err := s.Store.LinkBitIDsToBattle(battleID, bitIDs); err != nil {
			redirectWithError(w, r, err, "/battle/new?res=%s", "Could not set the battle's bits")
			return
		}
		if err := s.Store.LinkOwnerIDsToBattle(battleID, []int{user.ID}); err != nil {
			redirectWithError(w, r, err, "/battle/new?res=%s", "Could not set the battle's owner")
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
		data := s.newPageData()
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
		if msg := r.URL.Query().Get("res"); msg != "" {
			data["res"] = msg
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

		s.render(w, "battleQuick", data)

	case http.MethodPost:
		user, ok := s.currentUser(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		r.ParseForm()

		botIDs, err := parseIDSelection(r.Form, "bot-")
		if err != nil {
			redirectWithError(w, r, err, "/battle/quick?res=%s", "Invalid bot selection")
			return
		}

		// "Quick" means zero setup beyond picking bots: the form (by design -
		// see battleQuick.html) has no arena-size/max-rounds/public fields at
		// all, so this must never read them from the request. It used to call
		// the same parseBattleSettings as /battle/new, which requires exactly
		// those fields - meaning every single quick-battle submission failed
		// with "Invalid arena size or max rounds", unconditionally.
		battleID, err := s.Store.InsertBattle(model.Battle{
			Name:      fmt.Sprintf("quick-%d", rand.Intn(10000)),
			Public:    false,
			MaxRounds: defaultQuickBattleMaxRounds,
			ArenaSize: defaultQuickBattleArenaSize,
		}, user)
		if err != nil {
			redirectWithError(w, r, err, "/battle/quick?res=%s", "Could not create the battle")
			return
		}

		// allow every arch/bit so quick-battles don't need manual setup
		allArchs, err := s.Store.GetAllArchs()
		if err != nil {
			log.Println(err)
		}
		archIDs := make([]int, len(allArchs))
		for i, a := range allArchs {
			archIDs[i] = a.ID
		}
		allBits, err := s.Store.GetAllBits()
		if err != nil {
			log.Println(err)
		}
		bitIDs := make([]int, len(allBits))
		for i, b := range allBits {
			bitIDs[i] = b.ID
		}

		if err := s.Store.LinkArchIDsToBattle(battleID, archIDs); err != nil {
			log.Println(err)
		}
		if err := s.Store.LinkBitIDsToBattle(battleID, bitIDs); err != nil {
			log.Println(err)
		}
		for _, botID := range botIDs {
			if err := s.Store.LinkBotBattle(botID, battleID); err != nil {
				log.Println(err)
			}
		}

		http.Redirect(w, r, fmt.Sprintf("/battle/%d", battleID), http.StatusSeeOther)

	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}

func (s *Server) battleSingleHandler(w http.ResponseWriter, r *http.Request) {
	battleID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "400 - Invalid battle id", http.StatusBadRequest)
		return
	}
	redirTarget := fmt.Sprintf("/battle/%d?res=%%s", battleID)

	switch r.Method {
	case http.MethodGet:
		data := s.newPageData()
		data["pagelink1"] = Link{Name: "battle", Target: "/battle"}
		data["pagelink1options"] = []Link{
			{Name: "user", Target: "/user"},
			{Name: "bot", Target: "/bot"},
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

		users, err := s.Store.GetAllUsers()
		if err != nil {
			redirectWithError(w, r, err, redirTarget, "Could not get the users")
			return
		}
		data["users"] = users

		battle, err := s.Store.GetBattleByIDDeep(battleID)
		if err != nil {
			redirectWithError(w, r, err, redirTarget, "Could not get the battle given the id provided")
			return
		}
		data["battle"] = battle
		data["botAmount"] = len(battle.Bots)
		data["battleCount"] = (len(battle.Bots) * len(battle.Bots)) * 2

		data["pagelink2"] = Link{Name: battle.Name, Target: fmt.Sprintf("/%d", battle.ID)}

		allBattles, err := s.Store.GetAllBattles()
		if err != nil {
			log.Println(err)
		}
		var opts []Link
		for _, b := range allBattles {
			opts = append(opts, Link{Name: b.Name, Target: fmt.Sprintf("/%d", b.ID)})
		}
		data["pagelink2options"] = opts

		myBots, err := s.Store.GetUserBotsByUsername(viewer.Name)
		if err != nil {
			redirectWithError(w, r, err, redirTarget, "Could not get your bots")
			return
		}
		data["myBots"] = myBots

		archs, err := s.Store.GetAllArchs()
		if err != nil {
			redirectWithError(w, r, err, redirTarget, "Could not get the archs")
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

		bits, err := s.Store.GetAllBits()
		if err != nil {
			redirectWithError(w, r, err, redirTarget, "Could not fetch the bits")
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

		for _, owner := range battle.Owners {
			if owner.ID == viewer.ID {
				data["editable"] = true
			}
		}

		s.render(w, "battleSingle", data)

	case http.MethodPost:
		viewer, ok := s.currentUser(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		existing, err := s.Store.GetBattleByIDDeep(battleID)
		if err != nil {
			redirectWithError(w, r, err, redirTarget, "Could not get the battle given the id provided")
			return
		}

		// Only an existing owner may change battle settings - this used to
		// check whether the submitter had included themselves in the *new*
		// owner list on the form, which let anyone grant themselves
		// ownership by just submitting their own id.
		isOwner := false
		for _, owner := range existing.Owners {
			if owner.ID == viewer.ID {
				isOwner = true
			}
		}
		if !isOwner {
			redirectWithError(w, r, nil, redirTarget+"#settings", "You aren't an owner and aren't allowed to edit the settings")
			return
		}

		r.ParseForm()
		name := r.Form.Get("name")
		arenaSize, err := strconv.Atoi(r.Form.Get("arena-size"))
		if err != nil {
			redirectWithError(w, r, err, redirTarget, "Invalid arena size")
			return
		}
		public := r.Form.Get("public") == "on"

		archIDs, bitIDs, err := parseArchBitSelection(r.Form)
		if err != nil {
			redirectWithError(w, r, err, redirTarget, "Invalid arch/bit selection")
			return
		}
		ownerIDs, err := parseIDSelection(r.Form, "owner-")
		if err != nil {
			redirectWithError(w, r, err, redirTarget, "Invalid owner selection")
			return
		}
		// the submitter must always remain able to manage the battle, even
		// if they didn't tick their own checkbox by mistake.
		ownerIDs = ensureIncluded(ownerIDs, viewer.ID)

		if err := s.Store.LinkArchIDsToBattle(battleID, archIDs); err != nil {
			redirectWithError(w, r, err, redirTarget+"#settings", "Could not link arch id to battle")
			return
		}
		if err := s.Store.LinkBitIDsToBattle(battleID, bitIDs); err != nil {
			redirectWithError(w, r, err, redirTarget+"#settings", "Could not link bit id to battle")
			return
		}
		if err := s.Store.LinkOwnerIDsToBattle(battleID, ownerIDs); err != nil {
			redirectWithError(w, r, err, redirTarget+"#settings", "Could not link owner id to battle")
			return
		}

		if err := s.Store.UpdateBattle(model.Battle{
			ID:        battleID,
			Name:      name,
			Public:    public,
			MaxRounds: existing.MaxRounds,
			ArenaSize: arenaSize,
		}); err != nil {
			redirectWithError(w, r, err, redirTarget+"#settings", "Could not update the battle")
			return
		}

		http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=Success!#settings", battleID), http.StatusSeeOther)

	default:
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
	}
}

func (s *Server) battleSubmitHandler(w http.ResponseWriter, r *http.Request) {
	battleID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "400 - Invalid battle id", http.StatusBadRequest)
		return
	}

	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
		return
	}
	r.ParseForm()

	user, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	botIDs, err := parseIDSelection(r.Form, "bot-")
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=ERROR:+Invalid+bot+supplied", battleID), http.StatusSeeOther)
		return
	}

	battle, err := s.Store.GetBattleByIDDeep(battleID)
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=ERROR:+Couldn%%27t+get+the+battle", battleID), http.StatusSeeOther)
		return
	}

	if err := s.Store.UnlinkAllBotsForUserFromBattle(user.ID, battleID); err != nil {
		log.Println(err)
	}

	for _, id := range botIDs {
		bot, err := s.Store.GetBotByID(id)
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=ERROR:+Couldn%%27t+get+bot+%d", battleID, id), http.StatusSeeOther)
			return
		}

		if !botMatchesAny(bot.Archs, battle.Archs) {
			http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=Bot+has+an+invalid+architecture!", battleID), http.StatusSeeOther)
			return
		}
		if !botMatchesAnyBits(bot.Bits, battle.Bits) {
			http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=Bot+has+an+invalid+%%27bit-ness%%27!", battleID), http.StatusSeeOther)
			return
		}

		if err := s.Store.LinkBotBattle(id, battleID); err != nil {
			log.Println(err)
			http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=ERROR:+Could+not+submit+bot+%d", battleID, id), http.StatusSeeOther)
			return
		}
	}

	http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=Success!", battleID), http.StatusSeeOther)
}

// battleRunHandler assembles every submitted bot and runs the simulation.
func (s *Server) battleRunHandler(w http.ResponseWriter, r *http.Request) {
	battleID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "400 - Invalid battle id", http.StatusBadRequest)
		return
	}

	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
		return
	}

	if _, ok := s.currentUser(r); !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	deepBattle, err := s.Store.GetBattleByIDDeep(battleID)
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=ERROR:+Could+not+load+the+battle", battleID), http.StatusSeeOther)
		return
	}
	if len(deepBattle.Bots) == 0 {
		http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=ERROR:+No+bots+submitted", battleID), http.StatusSeeOther)
		return
	}

	specs := make([]battle.Spec, len(deepBattle.Bots))
	for i, b := range deepBattle.Bots {
		full, err := s.Store.GetBotByID(b.ID)
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=ERROR:+Could+not+load+bot+%d", battleID, b.ID), http.StatusSeeOther)
			return
		}
		if len(full.Archs) == 0 || len(full.Bits) == 0 {
			http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=ERROR:+Bot+%s+has+no+arch/bits+set", battleID, full.Name), http.StatusSeeOther)
			return
		}
		specs[i] = battle.Spec{
			Name:     full.Name,
			Source:   full.Source,
			ArchName: full.Archs[0].Name,
			BitsName: full.Bits[0].Name,
		}
	}

	p, err := s.OpenR2(fmt.Sprintf("malloc://%d", deepBattle.ArenaSize))
	if err != nil {
		log.Println(err)
		http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=ERROR:+Could+not+start+radare2", battleID), http.StatusSeeOther)
		return
	}
	defer p.Close()

	outcome, err := battle.Run(p, deepBattle.ArenaSize, deepBattle.MaxRounds, specs)
	if err != nil {
		log.Println(err)
		http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=ERROR:+Battle+failed+to+run", battleID), http.StatusSeeOther)
		return
	}

	if err := s.Store.UpdateBattleRawOutput(battleID, outcome.RawOutput); err != nil {
		log.Println(err)
	}

	http.Redirect(w, r, fmt.Sprintf("/battle/%d?res=Success!#output", battleID), http.StatusSeeOther)
}

func (s *Server) battleDeleteHandler(w http.ResponseWriter, r *http.Request) {
	battleID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "400 - Invalid battle id", http.StatusBadRequest)
		return
	}
	redirTarget := fmt.Sprintf("/battle/%d?res=%%s", battleID)

	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusMethodNotAllowed)
		return
	}

	viewer, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	battle, err := s.Store.GetBattleByIDDeep(battleID)
	if err != nil {
		redirectWithError(w, r, err, redirTarget, "Could not get the battle given the id provided")
		return
	}

	owned := false
	for _, owner := range battle.Owners {
		if owner.ID == viewer.ID {
			owned = true
			break
		}
	}
	if !owned {
		http.Error(w, "403 - You aren't an owner of this battle", http.StatusForbidden)
		return
	}

	if err := s.Store.DeleteBattleByID(battleID); err != nil {
		redirectWithError(w, r, err, redirTarget, "Could not delete the battle")
		return
	}

	http.Redirect(w, r, "/battle?res=Successfully+deleted+the+battle", http.StatusSeeOther)
}

func parseBattleSettings(r *http.Request) (arenaSize, maxRounds int, public bool, err error) {
	arenaSize, err = strconv.Atoi(r.Form.Get("arena-size"))
	if err != nil {
		return 0, 0, false, fmt.Errorf("invalid arena-size: %w", err)
	}
	maxRounds, err = strconv.Atoi(r.Form.Get("max-rounds"))
	if err != nil {
		return 0, 0, false, fmt.Errorf("invalid max-rounds: %w", err)
	}
	public = r.Form.Get("public") == "on"
	return arenaSize, maxRounds, public, nil
}

func ensureIncluded(ids []int, id int) []int {
	for _, existing := range ids {
		if existing == id {
			return ids
		}
	}
	return append(ids, id)
}

func botMatchesAny(botArchs, battleArchs []model.Arch) bool {
	for _, ba := range battleArchs {
		for _, bo := range botArchs {
			if ba.ID == bo.ID {
				return true
			}
		}
	}
	return false
}

func botMatchesAnyBits(botBits, battleBits []model.Bit) bool {
	for _, bb := range battleBits {
		for _, bo := range botBits {
			if bb.ID == bo.ID {
				return true
			}
		}
	}
	return false
}
