package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"
	_ "embed"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	CheckIDLength   = 32
	CheckIDPWLength = 32
	CheckIDChars    = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890"
)

var dbconn *pgxpool.Pool // Global var for DB connections

var ( // Error definitions
	ErrInvalidID    = errors.New("Invalid ID")
	ErrUnauthorized = errors.New("Unauthorized")
	ErrNonexistent  = errors.New("Nonexistent")
	ErrExists       = errors.New("Already exists")
)

//go:embed chck.js
var clientJavaScript []byte

//go:embed index.html
var index []byte

type CheckID string

func (c CheckID) Exists() bool {
	_, err := c.State()
	return err == nil
}

func (c CheckID) State() (bool, error) {
	var result bool
	row := dbconn.QueryRow(context.Background(), `SELECT state FROM switches WHERE id = $1`, c)
	if err := row.Scan(&result); err != nil {
		return false, ErrNonexistent
	}
	return result, nil
}

func (c CheckID) Valid() bool {
	if len(c) > CheckIDLength {
		return false
	}

	// TODO: Rewrite this so the lookup is quicker.
	for _, v := range c {
		flag := false
		for _, x := range CheckIDChars {
			if v == x {
				flag = true
			}
		}
		if !flag {
			return false
		}
	}

	return true
}

func (c CheckID) Create(password string) error {
	if !c.Valid() {
		return ErrInvalidID
	}

	if c.Exists() {
		return ErrExists
	}

	if _, err := dbconn.Exec(context.Background(), `INSERT INTO switches (id, password) VALUES($1, $2)`, c, password); err != nil {
		return err
	}

	return nil
}

func (c CheckID) IsAuthorized(password string) bool {
	var dbpw string
	row := dbconn.QueryRow(context.Background(), `SELECT password FROM switches WHERE id = $1`, c)
	if err := row.Scan(&dbpw); err != nil {
		return false
	}
	return password == dbpw
}

func (c CheckID) Toggle(password string) (bool, error) {
	state, err := c.State()
	if err != nil {
		return false, err
	}

	if !c.IsAuthorized(password) {
		return false, ErrUnauthorized
	}

	state = !state // Toggle to new state

	_, err = dbconn.Exec(context.Background(), `UPDATE switches SET state = NOT state WHERE id = $1 AND password = $2`, c, password)
	if err != nil {
		// Assume password is incorrect. If the CheckID didn't
		// exist, it would've been caught when call c.State()
		return false, ErrUnauthorized
	}

	return state, nil
}

func RandomCheckID() CheckID {
	result := make([]byte, CheckIDLength)

	for i := 0; i < CheckIDLength; i++ {
		n := rand.Intn(len(CheckIDChars))
		result[i] = CheckIDChars[n]
	}

	return CheckID(result)
}

func handleNewSwitches(w http.ResponseWriter, r *http.Request) {
	count := r.URL.Query().Get("count")
	password := r.URL.Query().Get("password")

	w.Header().Add("Content-Type", "text/plain")
	
	if len(count) == 0 {
		count = "1"
	}

	n, err := strconv.ParseInt(count, 10, 32)
	if err != nil {
		// TODO: Log user error.
		return
	}

	for i := int64(0); i < n; i++ {
		id := RandomCheckID()
		if err := id.Create(password); err != nil {
			log.Println(err)
			http.Error(w, "Interval Server Error", http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, "<chck value=\"%s\"></chck>\n", id)
	}
}

func handleSwitch(w http.ResponseWriter, r *http.Request) {
	id := CheckID(r.URL.Path[1:]) // Chop the leading slash

	if id == "" || !id.Valid() {
		handleStatic(w, r)
		return
	}
	
	password := r.URL.Query().Get("password")

	w.Header().Add("Content-Type", "text/plain")
	w.Header().Add("Access-Control-Allow-Origin", "*")
	w.Header().Add("Access-Control-Allow-Methods", "GET,POST,PUT")

	buf := make([]byte, CheckIDPWLength)
	n, _ := r.Body.Read(buf)
	if n > 0 {
		password = string(buf[0:n])
	}

	switch r.Method {
	case http.MethodGet:
		state, err := id.State()
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if state {
			w.Write([]byte{'1'})
		} else {
			w.Write([]byte{'0'})
		}
	case http.MethodPut:
		state, err := id.Toggle(password)
		if errors.Is(err, ErrUnauthorized) {
			log.Printf("Incorrect Password from '%s'.", r.RemoteAddr)
			w.WriteHeader(http.StatusUnauthorized)
			return
		} else if errors.Is(err, ErrNonexistent) {
			log.Printf("Attempt to toggle nonexistent ID '%s' from '%s'.", string(id), r.RemoteAddr)
			w.WriteHeader(http.StatusNotFound)
			return
		} else if err != nil {
			log.Println("Unknown error while toggling: ", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		if state {
			w.Write([]byte{'1'})
		} else {
			w.Write([]byte{'0'})
		}
	case http.MethodPost:
		err := id.Create(password)
		if errors.Is(err, ErrExists) {
			// TODO: Pick proper status
			log.Println("Already Exists: ", id)
			w.WriteHeader(http.StatusConflict)
			return
		} else if errors.Is(err, ErrInvalidID) {
			// TODO: Pick proper status
			log.Println("Unacceptable CheckID: ", id)
			w.WriteHeader(http.StatusNotAcceptable)
			return
		}
		w.Write([]byte(id))
	}
}

func handleStatic(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/js/chck.js":
		w.Header().Add("Content-Type", "application/javascript; charset=utf-8")
		w.Write(clientJavaScript)
	case "/":
		// Display index.html
		w.Header().Add("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

func serve() {
	rand.Seed(time.Now().Unix())

	var err error
	dbconn, err = pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer dbconn.Close()

	var mux http.ServeMux

	mux.HandleFunc("/", handleSwitch)
	mux.HandleFunc("/mk", handleNewSwitches)

	if err := http.ListenAndServe(":" + os.Getenv("PORT"), &mux); err != nil {
		log.Fatal(err)
	}
}

// Initialize database
func initDB() {
	ctx := context.Background()
	conn, err := pgx.Connect(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close(ctx)

	tx, err := conn.Begin(ctx)
	if err != nil {
		log.Fatal(err)
	}
	_, err = tx.Exec(ctx, `DROP TABLE IF EXISTS switches`)
	if err != nil {
		log.Fatal("Failed to drop table.")
		return
	}

	_, err = tx.Exec(ctx, fmt.Sprintf(`CREATE TABLE switches (id VARCHAR(%d) UNIQUE, password VARCHAR(%d) DEFAULT '', state BOOL DEFAULT FALSE)`, CheckIDLength, CheckIDPWLength))
	if err != nil {
		log.Fatal("Failed to create table.")
		return
	}
	tx.Commit(ctx)
}

func usage() {
	fmt.Fprintf(os.Stderr,
		`Usage: %s [command]

Commands

  init-database     Initialize/reset database
  serve             Run service
  help              Display this`, os.Args[0])
	fmt.Println()
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(-1)
	}

	switch os.Args[1] {
	case "init-database":
		initDB()
	case "serve":
		serve()
	case "help":
		usage()
	}
}
