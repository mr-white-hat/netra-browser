package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/pavankumar2138/netra-browser/internal/cdp"
	"github.com/pavankumar2138/netra-browser/internal/mcp"
	"github.com/pavankumar2138/netra-browser/internal/mcp/tools"
	"github.com/pavankumar2138/netra-browser/internal/profile"
)

const Version = "0.0.1-dev"

func main() {
	var (
		showVersion = flag.Bool("version", false, "print version and exit")
		debugURL    = flag.String("debug-url", "http://127.0.0.1:9222", "Chrome remote debugging URL")
		autoAttach  = flag.Bool("auto-attach", false, "attach to Chrome at startup")
		lockPath    = flag.String("lock", "", "lock file path (default: ~/.config/netra-browser/active.lock)")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println(Version)
		return
	}

	if *lockPath == "" {
		home, _ := os.UserHomeDir()
		*lockPath = filepath.Join(home, ".config", "netra-browser", "active.lock")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	lock, err := profile.Acquire(*lockPath, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lock: %v (use --force-reattach in a future version)\n", err)
		os.Exit(2)
	}
	defer lock.Release()

	sess := mcp.NewSession()
	reg := mcp.NewRegistry()

	tools.RegisterMeta(reg, sess, tools.MetaDeps{
		StartedAt: time.Now(),
		AttachFunc: func(ctx context.Context, url string) (mcp.CDPSender, string, int, error) {
			return cdp.Attach(ctx, url)
		},
	})
	tools.RegisterBrowserTargets(reg, sess)
	tools.RegisterBrowserNav(reg, sess)
	tools.RegisterBrowserInspect(reg, sess)
	tools.RegisterBrowserInteract(reg, sess)
	tools.RegisterBrowserEvents(reg, sess)

	if *autoAttach {
		client, version, count, err := cdp.Attach(ctx, *debugURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "auto-attach: %v\n", err)
		} else {
			sess.SetClient(client)
			fmt.Fprintf(os.Stderr, "attached to %s (%d targets)\n", version, count)
		}
	}

	if err := mcp.Serve(ctx, os.Stdin, os.Stdout, reg); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		os.Exit(1)
	}
}
