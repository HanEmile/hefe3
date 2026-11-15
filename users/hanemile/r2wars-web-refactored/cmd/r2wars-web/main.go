// Command r2wars-web is the HTTP entrypoint for the refactored r2wars-web. It
// wires the store, session store and template-backed web.Server together and
// serves the same route table as the original flat package-main version.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"git.emile.space/r2wars-web/internal/store"
	"git.emile.space/r2wars-web/internal/web"
)

func main() {
	var (
		host          string
		port          int
		logFilePath   string
		databasePath  string
		sessiondbPath string
		templatesPath string
	)

	flag.StringVar(&host, "host", "127.0.0.1", "The host to listen on")
	flag.StringVar(&host, "h", "127.0.0.1", "The host to listen on (shorthand)")
	flag.IntVar(&port, "port", 8080, "The port to listen on")
	flag.IntVar(&port, "p", 8080, "The port to listen on (shorthand)")
	flag.StringVar(&logFilePath, "logfilepath", "./server.log", "The path to the log file")
	flag.StringVar(&databasePath, "databasepath", "./main.db", "The path to the main database")
	flag.StringVar(&sessiondbPath, "sessiondbpath", "./sessions.db", "The path to the session database")
	flag.StringVar(&templatesPath, "templates", "./templates", "The path to the templates used")
	flag.Parse()

	// log init
	log.Println("[i] Setting up logging...")
	logFile, err := os.OpenFile(logFilePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0664)
	if err != nil {
		log.Fatal("Error opening the server.log file: ", err)
	}
	defer logFile.Close()

	// db init
	log.Println("[i] Setting up the store...")
	st, err := store.New(databasePath)
	if err != nil {
		log.Fatal("Error creating the store: ", err)
	}
	defer st.Close()

	// session init
	log.Println("[i] Setting up Session Storage...")
	sess, err := store.NewSessionStore(sessiondbPath, "sessions", "/", 3600, []byte(os.Getenv("SESSION_KEY")))
	if err != nil {
		log.Fatal("Error creating the session store: ", err)
	}

	// HTTP init
	log.Println("[i] Setting up HTTP Routes...")
	server, err := web.NewServer(
		st, sess,
		fmt.Sprintf("%s/*.html", templatesPath),
		os.Getenv("VERSION"),
		os.Getenv("SALT"),
	)
	if err != nil {
		log.Fatal("Error creating the web server: ", err)
	}
	server.SetLogOutput(logFile)

	log.Printf("[i] HTTP Server running on %s:%d\n", host, port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf("%s:%d", host, port), server.Router()))
}
