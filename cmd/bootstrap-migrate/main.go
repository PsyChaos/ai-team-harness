// Command bootstrap-migrate verifies and preserves a Python tracking issue body.
// It reads stdin and emits no stdout until verification succeeds.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/PsyChaos/ai-team-harness/internal/bootstrap/migration"
	"github.com/PsyChaos/ai-team-harness/internal/snapshot"
)

func run() error {
	repo := flag.String("repo", "", "expected owner/repository")
	flag.Parse()
	if flag.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	signer, err := snapshot.NewSigner(os.Getenv("HARNESS_BOOTSTRAP_HMAC_KEY"))
	if err != nil {
		return err
	}
	body, err := io.ReadAll(io.LimitReader(os.Stdin, 4*1024*1024+1))
	if err != nil {
		return err
	}
	result, err := migration.Preserve(body, *repo, signer)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(result)
	return err
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap-migrate:", err)
		os.Exit(1)
	}
}
