package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"builder/cli/builder/internal/serverbridge"
)

type serveCommandServer = serverbridge.ServeServer

var startServeServer = func(ctx context.Context, req serverbridge.StartupRequest, authHandler serverbridge.StartupAuthHandler, onboardingHandler serverbridge.StartupOnboardingHandler) (serveCommandServer, error) {
	return serverbridge.StartServe(ctx, req, authHandler, onboardingHandler)
}
var newServeStartupHandlers = func() (serverbridge.StartupAuthHandler, serverbridge.StartupOnboardingHandler) {
	return serverbridge.NewHeadlessHandlers(nil)
}

func serveSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	serveFS := flag.NewFlagSet("builder serve", flag.ContinueOnError)
	serveFS.SetOutput(stderr)
	serveFS.Usage = func() { writeServeUsage(serveFS) }
	if err := serveFS.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if remaining := serveFS.Args(); len(remaining) > 0 {
		fmt.Fprintf(stderr, "unexpected arguments: %s\n", strings.Join(remaining, " "))
		serveFS.Usage()
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	authHandler, onboardingHandler := newServeStartupHandlers()
	server, err := startServeServer(ctx, serverbridge.StartupRequest{
		AllowUnauthenticated: true,
	}, authHandler, onboardingHandler)
	if err != nil {
		fmt.Fprintln(stderr, err)
		if errors.Is(err, context.Canceled) {
			return 130
		}
		return 1
	}
	defer func() { _ = server.Close() }()
	_, _ = fmt.Fprintln(stderr, "Server started, Ctrl+C to stop")
	if err := server.Serve(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return 130
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
