package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/x/term"
	"github.com/magarcia/cuentica-cli/internal/cli"
)

// Set these with -ldflags for release builds.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM, syscall.SIGPIPE)
	defer signal.Stop(interrupts)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var terminalState *term.State
	if term.IsTerminal(os.Stdin.Fd()) {
		terminalState, _ = term.GetState(os.Stdin.Fd())
	}

	complete := make(chan int, 1)
	go func() {
		version := fmt.Sprintf("cuentica %s (commit %s, built %s)", Version, Commit, BuildDate)
		complete <- cli.New(version).Run(ctx, args)
	}()
	select {
	case code := <-complete:
		return code
	case interrupted := <-interrupts:
		cancel()
		// Restore echo even if password input stays blocked after cancellation.
		if terminalState != nil {
			_ = term.Restore(os.Stdin.Fd(), terminalState)
		}
		switch interrupted {
		case os.Interrupt:
			return 130
		case syscall.SIGTERM:
			return 143
		default:
			return 0
		}
	}
}
