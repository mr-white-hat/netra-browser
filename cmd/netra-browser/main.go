package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/pavankumar2138/netra-browser/internal/cdp"
	"github.com/pavankumar2138/netra-browser/internal/mcp"
	"github.com/pavankumar2138/netra-browser/internal/mcp/tools"
	"github.com/pavankumar2138/netra-browser/internal/profile"
)

const Version = "0.0.1-dev"

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	var (
		showVersion     = flag.Bool("version", false, "print version and exit")
		debugURL        = flag.String("debug-url", "http://127.0.0.1:9222", "Chrome remote debugging URL")
		autoAttach      = flag.Bool("auto-attach", false, "attach to Chrome at startup")
		lockPath        = flag.String("lock", "", "lock file path (default: ~/.config/netra-browser/active.lock)")
		listen          = flag.String("listen", "", "HTTP-SSE listen address (e.g. 127.0.0.1:7878). Empty = stdio mode.")
		token           = flag.String("token", "", "Bearer token for HTTP-SSE auth")
		launch          = flag.Bool("launch", false, "launch Chrome instead of attaching to a running one")
		profileDir      = flag.String("profile-dir", "", "user-data-dir for launched Chrome (default: platform default)")
		profileSnapshot = flag.Bool("profile-snapshot", false, "copy profile to a temp dir before launching")
		launchHeadless  = flag.Bool("launch-headless", false, "pass --headless=new when launching")
	)
	var allowOrigins multiFlag
	flag.Var(&allowOrigins, "allow-origin", "CORS allowed origin (repeatable)")
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
	tools.RegisterSessionTasks(reg, sess)
	tools.RegisterHighLevelTasks(reg, sess)

	var launchHandle *profile.LaunchHandle
	if *launch {
		dir := *profileDir
		if dir == "" {
			var err error
			dir, err = profile.DefaultUserDataDir()
			if err != nil {
				fmt.Fprintf(os.Stderr, "profile: %v\n", err)
				os.Exit(2)
			}
		}
		if *profileSnapshot {
			td, err := os.MkdirTemp("", "netra-profile-")
			if err != nil {
				fmt.Fprintf(os.Stderr, "snapshot: %v\n", err)
				os.Exit(2)
			}
			if err := copyTree(dir, td); err != nil {
				fmt.Fprintf(os.Stderr, "copy profile: %v\n", err)
				os.Exit(2)
			}
			dir = td
		}
		h, err := profile.Launch(profile.LaunchOpts{UserDataDir: dir, Headless: *launchHeadless})
		if err != nil {
			fmt.Fprintf(os.Stderr, "launch: %v\n", err)
			os.Exit(1)
		}
		launchHandle = h
		*debugURL = h.DebugURL()
		*autoAttach = true
		fmt.Fprintf(os.Stderr, "launched chrome at %s (user-data-dir=%s)\n", h.DebugURL(), h.UserDataDir())
	}
	defer func() {
		if launchHandle != nil {
			_ = launchHandle.Stop()
		}
	}()

	if *autoAttach {
		client, version, count, err := cdp.Attach(ctx, *debugURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "auto-attach: %v\n", err)
		} else {
			sess.SetClient(client)
			fmt.Fprintf(os.Stderr, "attached to %s (%d targets)\n", version, count)
		}
	}

	if *listen != "" {
		if !isLocalListen(*listen) && *token == "" {
			fmt.Fprintln(os.Stderr, "non-localhost listen requires --token")
			os.Exit(2)
		}
		handler := mcp.NewHTTPHandler(reg, mcp.HTTPOpts{Token: *token, AllowedOrigins: allowOrigins})
		srv := &http.Server{Addr: *listen, Handler: handler}
		fmt.Fprintf(os.Stderr, "netra-browser listening on %s\n", *listen)
		go func() {
			<-ctx.Done()
			shutCtx, cancelShut := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancelShut()
			_ = srv.Shutdown(shutCtx)
		}()
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "http: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := mcp.Serve(ctx, os.Stdin, os.Stdout, reg); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		os.Exit(1)
	}
}

func isLocalListen(addr string) bool {
	return strings.HasPrefix(addr, "127.0.0.1:") ||
		strings.HasPrefix(addr, "localhost:") ||
		strings.HasPrefix(addr, "[::1]:")
}

// copyTree copies srcDir into dstDir recursively (regular files only — symlinks and special files skipped).
// Skips obvious lock-like files to avoid corrupting the source profile.
func copyTree(srcDir, dstDir string) error {
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Skip permission errors instead of failing the whole copy.
			if os.IsPermission(err) {
				return nil
			}
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(dstDir, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		// Skip non-regular (symlinks, sockets, etc).
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		// Skip Chrome's lock files.
		base := filepath.Base(path)
		if base == "SingletonLock" || base == "SingletonCookie" || base == "SingletonSocket" {
			return nil
		}
		return copyFile(path, dst, info.Mode().Perm())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
