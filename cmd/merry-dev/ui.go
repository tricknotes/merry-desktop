//go:build linux

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"time"

	"github.com/tricknotes/merry-desktop/frontend"
	"github.com/tricknotes/merry-desktop/internal/appservice"
	"github.com/tricknotes/merry-desktop/internal/core"
	"github.com/tricknotes/merry-desktop/internal/devui"
)

// ui serves the app's settings and setup windows to a browser, backed by the
// real controller and VM. WSL forwards localhost, so a Windows browser can
// open it. The tray is not available here.
func ui(ctx context.Context, c *core.Controller, args []string) error {
	fset := flag.NewFlagSet("ui", flag.ExitOnError)
	addr := fset.String("addr", "127.0.0.1:18765", "listen address (loopback only)")
	open := fset.Bool("open", true, "open the page in the Windows browser")
	_ = fset.Parse(args)

	host, port, err := net.SplitHostPort(*addr)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return errors.New("-addr must be a loopback address")
	}

	store, err := devSettings(ctx, c)
	if err != nil {
		return err
	}
	service := appservice.New(c, store, nil)
	dist, err := fs.Sub(frontend.Dist, "dist")
	if err != nil {
		return err
	}
	srv, err := devui.New(service, dist)
	if err != nil {
		return err
	}
	service.Emit = srv.Emit

	l, err := net.Listen("tcp", net.JoinHostPort(host, port))
	if err != nil {
		return err
	}
	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = httpSrv.Serve(l) }()
	defer httpSrv.Close()

	// "/" rather than "/index.html": the file server redirects the latter to "/"
	// and the redirect would drop the token.
	url := fmt.Sprintf("http://localhost:%d/?token=%s", l.Addr().(*net.TCPAddr).Port, srv.Token)
	fmt.Println("開発用 UI:", url)
	fmt.Println("(トークン付きの URL です。ほかの人と共有しないでください。Ctrl+C で終わります)")
	if *open {
		cmd := exec.Command("cmd.exe", "/c", "start", "", url)
		cmd.Dir = "/mnt/c"
		if err := cmd.Run(); err != nil {
			fmt.Println("ブラウザを開けませんでした。上の URL を開いてください:", err)
		}
	}

	c.Refresh(ctx)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	ticks := 0
	for {
		select {
		case <-ctx.Done():
			fmt.Println("\n終了します")
			return nil
		case <-ticker.C:
			c.Refresh(context.WithoutCancel(ctx))
			if ticks++; ticks%6 == 0 {
				c.CheckMemory(context.WithoutCancel(ctx)) // every minute
				appservice.SyncNetwork(context.WithoutCancel(ctx), service, slog.Default())
				if s, err := store.Load(); err == nil && s.AutoCleanupEnabled() {
					c.MaybeAutoCleanup(context.WithoutCancel(ctx)) // at most once a day
				}
			}
		}
	}
}
