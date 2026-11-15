package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
)

const sessionFile = "sessions.json"

var admins = make(map[string]bool)

var sessions = make(map[string]string)
var challenges = make(map[string]SSHChallenge)

var challengeMu sync.Mutex

type SSHChallenge struct {
	Token     string
	Code      string
	Username  string
	CreatedAt int64
}

func StartChallenge(username string) (string, string) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		log.Printf("Error: %v", err)
	}
	token := base64.URLEncoding.EncodeToString(b)

	b2 := make([]byte, 6)
	_, err = rand.Read(b2)
	if err != nil {
		log.Printf("Error: %v", err)
	}
	var code string
	for _, b := range b2 {
		code += string('0' + (b % 10))
	}

	challengeMu.Lock()
	challenges[token] = SSHChallenge{
		Token:    token,
		Code:     code,
		Username: username,
	}
	challengeMu.Unlock()

	return token, code
}

func VerifyChallenge(token, code string) (string, bool) {
	challengeMu.Lock()
	defer challengeMu.Unlock()

	challenge, ok := challenges[token]
	if !ok || challenge.Code != code {
		return "", false
	}

	delete(challenges, token)
	return challenge.Username, true
}

func GetChallengeCode(token string) (string, bool) {
	challengeMu.Lock()
	defer challengeMu.Unlock()
	c, ok := challenges[token]
	return c.Code, ok
}

func Init(adminList []string) {
	for _, admin := range adminList {
		admins[admin] = true
	}
	loadSessions()
}

func CreateSession(username string) string {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		log.Printf("Error: %v", err)
	}
	token := base64.URLEncoding.EncodeToString(b)

	challengeMu.Lock()
	sessions[token] = username
	challengeMu.Unlock()
	saveSessions()
	return token
}

func Logout(token string) {
	challengeMu.Lock()
	delete(sessions, token)
	challengeMu.Unlock()
	saveSessions()
}

func GetUser(r *http.Request) (string, bool) {
	cookie, err := r.Cookie("session_token")
	if err != nil {
		return "", false
	}
	challengeMu.Lock()
	username, ok := sessions[cookie.Value]
	challengeMu.Unlock()

	return username, ok
}

func IsAdmin(username string) bool {
	return admins[username]
}

func Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := GetUser(r); !ok {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next(w, r)
	}
}

func saveSessions() {
	challengeMu.Lock()
	defer challengeMu.Unlock()

	data, err := json.Marshal(sessions)
	if err != nil {
		fmt.Printf("[Error] Failed to marshal sessions: %v\n", err)
		return
	}

	if err := os.WriteFile(sessionFile, data, 0644); err != nil {
		fmt.Printf("[Error] Failed to save sessions: %v\n", err)
	}
}

func loadSessions() {
	challengeMu.Lock()
	defer challengeMu.Unlock()

	data, err := os.ReadFile(sessionFile)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Printf("[Error] Failed to read sessions file: %v\n", err)
		}
		return
	}

	if err := json.Unmarshal(data, &sessions); err != nil {
		fmt.Printf("[Error] Failed to unmarshal sessions: %v\n", err)
	} else {
		fmt.Printf("[Log] Loaded %d sessions from disk\n", len(sessions))
	}
}
