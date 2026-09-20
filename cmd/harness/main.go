// Command harness serves the demo dashboard or discovers provider capabilities.
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
	flag.Parse()
	server := &http.Server{
		Addr:              *listen,
		Handler:           dashboard.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("Factory Floor Demo: http://%s (offline fixtures only)", *listen)
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
