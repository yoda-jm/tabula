// Command tabula connects to an X32/M32 console (or tabula-emu) and serves a
// web GUI to browse and drive it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yoda-jm/tabula/pkg/webapi"
	"github.com/yoda-jm/tabula/web"
)

// version is set at release build time.
var version = "dev"

func main() {
	var (
		httpAddr = flag.String("http", "localhost:8080", "address to serve the GUI on (use :8080 to reach it from other devices)")
		consoleA = flag.String("console", "", "console (or tabula-emu) to connect to at start, e.g. 192.168.1.62")
		webDir   = flag.String("web-dir", "", "serve the GUI from this directory instead of the embedded build")
		sessions = flag.String("sessions", "sessions", "directory where recorded sessions are kept")
		record   = flag.Bool("record", false, "start recording as soon as the console is connected and synced (stops on exit)")
		showVer  = flag.Bool("version", false, "print the version and exit")
	)
	flag.Parse()
	if *showVer {
		fmt.Println("tabula", version)
		return
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	log.Info("tabula", "version", version)

	api := webapi.New(log)
	api.SetSessionsDir(*sessions)
	api.RecordOnSync(*record)
	defer api.StopRecording()
	if *consoleA != "" {
		api.Connect(*consoleA)
	}
	ln, err := net.Listen("tcp", *httpAddr)
	if err != nil {
		log.Error("listen", "err", err)
		os.Exit(1)
	}
	srv := &http.Server{Handler: api.Handler(web.Handler(*webDir)), ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(shut)
	}()
	log.Info("GUI", "url", "http://"+ln.Addr().String())
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("http", "err", err)
		os.Exit(1)
	}
}
