// Command harness is the Go migration entrypoint. The Python broker remains active.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/PsyChaos/ai-team-harness/internal/registry"
)

func main() {
	if len(os.Args) == 1 {
		fmt.Println("harness: Go foundation only; use .ai-team/bin/coordinator-broker for execution")
		return
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
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
