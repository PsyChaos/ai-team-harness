// Command harness serves the live observation dashboard or discovers provider capabilities.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/PsyChaos/ai-team-harness/internal/dashboard"
	"github.com/PsyChaos/ai-team-harness/internal/journal"
	"github.com/PsyChaos/ai-team-harness/internal/registry"
)

const defaultListen = "127.0.0.1:8080"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "discover" {
		if err := run(os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	listen := flag.String("listen", defaultListen, "dashboard listen address")
	journalPath := flag.String("journal", "", "read-only observation journal file; never selected by HTTP clients")
	repo := flag.String("repo", "", "repository owner/name displayed as installation identity")
	project := flag.String("project", "", "project identity displayed in this installation")
	flag.Parse()
	var api http.Handler
	if *journalPath != "" {
		// Match the bridge's maximum wire event size; no input is ever written.
		if _, err := os.Stat(*journalPath); err != nil {
			log.Fatal(err)
		}
		source, err := journal.Open(*journalPath, journal.Limits{Retain: 512, Pending: 1, MaxEventBytes: 1 << 20})
		if err != nil {
			log.Fatal(err)
		}
		api, err = source.Handler(journal.HTTPOptions{MaxStreams: 32, WriteTimeout: 5 * time.Second, Heartbeat: 15 * time.Second})
		if err != nil {
			log.Fatal(err)
		}
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if err := source.Reload(); err != nil {
					log.Printf("journal refresh failed: %v", err)
				}
			}
		}()
	}
	server := &http.Server{
		Addr:              *listen,
		Handler:           dashboard.WithJournal(api, *repo, *project),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("Factory Floor: http://%s (read-only observations)", *listen)
	log.Print("Go dashboard only; use .ai-team/bin/coordinator-broker for execution")
	log.Fatal(server.ListenAndServe())
}

func run(args []string) error {
	if len(args) != 2 || args[0] != "discover" {
		return fmt.Errorf("usage: harness discover CONFIG.json")
	}
	f, err := os.Open(args[1])
	if err != nil {
		return err
	}
	defer f.Close()
	c, err := registry.Load(f)
	if err != nil {
		return err
	}
	entries, err := registry.Discover(context.Background(), c, args[1])
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(entries)
}
