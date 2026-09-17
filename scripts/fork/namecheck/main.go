// Command namecheck is a read-only dry run of the gunmake fork's subscriber
// name migration (internal/migrations/fork_names_preview.go). Run it against a
// copy of the production database before deploying:
//
//	go run ./scripts/fork/namecheck -dsn 'postgres://user:pass@host:5432/listmonk?sslmode=disable'
//
// It reports how many names the migration would blank and split, prints random
// examples of each, and lists every template and unsent campaign that uses the
// subscriber's name, so greetings can be checked before the first send.
package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/models"
	_ "github.com/lib/pq"
)

const samples = 20

type subscriber struct {
	ID    int    `db:"id"`
	Email string `db:"email"`
	Name  string `db:"name"`
}

type change struct {
	subscriber
	first, last string
}

func main() {
	dsn := flag.String("dsn", os.Getenv("LISTMONK_DSN"), "Postgres connection URL (default $LISTMONK_DSN)")
	flag.Parse()
	if *dsn == "" {
		log.Fatal("-dsn or LISTMONK_DSN is required")
	}

	db, err := sqlx.Connect("postgres", *dsn)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer db.Close()

	tx, err := db.Beginx()
	if err != nil {
		log.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	// Guarantee the tool cannot change anything.
	if _, err := tx.Exec(`SET TRANSACTION READ ONLY`); err != nil {
		log.Fatalf("read-only transaction: %v", err)
	}

	var migrated bool
	if err := tx.Get(&migrated, `SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'subscribers' AND column_name = 'first_name')`); err != nil {
		log.Fatalf("check schema: %v", err)
	}

	if migrated {
		fmt.Println("subscribers.first_name already exists: the migration has run. Skipping the name report.")
	} else {
		reportNames(tx)
	}

	reportGreetings(tx)
}

func reportNames(tx *sqlx.Tx) {
	var subs []subscriber
	if err := tx.Select(&subs, `SELECT id, email, name FROM subscribers ORDER BY id`); err != nil {
		log.Fatalf("read subscribers: %v", err)
	}

	var (
		blank []change
		split []change
		empty int
	)
	for _, s := range subs {
		switch {
		case models.IsFallbackName(s.Name, s.Email):
			blank = append(blank, change{subscriber: s})
		case strings.TrimSpace(s.Name) == "":
			empty++
		default:
			first, last := models.SplitName(s.Name)
			split = append(split, change{subscriber: s, first: first, last: last})
		}
	}

	fmt.Printf("Subscribers: %d\n", len(subs))
	fmt.Printf("  names to blank (invented from the e-mail): %d\n", len(blank))
	fmt.Printf("  names to split into first/last: %d\n", len(split))
	fmt.Printf("  names already empty: %d\n\n", empty)

	printSample("Names that will be BLANKED", blank)
	printSample("Names that will be SPLIT", split)
}

func printSample(title string, cs []change) {
	n := min(samples, len(cs))
	fmt.Printf("%s (%d random of %d):\n", title, n, len(cs))

	rand.Shuffle(len(cs), func(i, j int) { cs[i], cs[j] = cs[j], cs[i] })
	for _, c := range cs[:n] {
		fmt.Printf("  #%-7d %-40s %-32q -> first=%q last=%q\n", c.ID, c.Email, c.Name, c.first, c.last)
	}
	fmt.Println()
}

func reportGreetings(tx *sqlx.Tx) {
	const pattern = `\.Subscriber\.(Name|FirstName|LastName)`

	var tpls []struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
		Type string `db:"type"`
	}
	if err := tx.Select(&tpls, `SELECT id, name, type FROM templates WHERE body ~ $1 ORDER BY id`, pattern); err != nil {
		log.Fatalf("read templates: %v", err)
	}

	var camps []struct {
		ID     int    `db:"id"`
		Name   string `db:"name"`
		Status string `db:"status"`
	}
	if err := tx.Select(&camps, `SELECT id, name, status FROM campaigns
		WHERE status NOT IN ('finished', 'cancelled')
		AND (body ~ $1 OR subject ~ $1 OR COALESCE(altbody, '') ~ $1)
		ORDER BY id`, pattern); err != nil {
		log.Fatalf("read campaigns: %v", err)
	}

	fmt.Printf("Templates using the subscriber's name: %d\n", len(tpls))
	for _, t := range tpls {
		fmt.Printf("  template #%d %q (%s)\n", t.ID, t.Name, t.Type)
	}
	fmt.Printf("Unsent campaigns using the subscriber's name: %d\n", len(camps))
	for _, c := range camps {
		fmt.Printf("  campaign #%d %q (%s)\n", c.ID, c.Name, c.Status)
	}
	fmt.Println(`
Check that each greeting reads well for a subscriber with no name, for example:
  Hello{{ if .Subscriber.FirstName }} {{ .Subscriber.FirstName }}{{ end }},`)
}
