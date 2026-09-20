// Command harness serves the embedded Factory Floor demo dashboard.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/PsyChaos/ai-team-harness/internal/dashboard"
)

const defaultListen = "127.0.0.1:8080"

func main() {
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
