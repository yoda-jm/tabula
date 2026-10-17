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
		httpAddr = flag.String("http", "localhost:8033", "address to serve the GUI on (use :8033 to reach it from other devices)")
		consoleA = flag.String("console", "", "console (or tabula-emu) to connect to at start, e.g. 192.168.1.62")
		webDir   = flag.String("web-dir", "", "serve the GUI from this directory instead of the embedded build")
		sessions = flag.String("sessions", "sessions", "directory where recorded sessions are kept")
		record   = flag.Bool("record", false, "start recording as soon as the console is connected and synced (stops on exit)")
		accessF  = flag.String("access", "access.json", "file keeping the access links made on the Access page")
		guest    = flag.String("guest", "none", "role of other devices without an access link: none, viewer or engineer")
		resetPw  = flag.Bool("reset-password", false, "remove the engineer password (and its sessions) from the access file, then exit")
		showVer  = flag.Bool("version", false, "print the version and exit")
		audioF   = addAudioFlags(flag.CommandLine) // audio.go
	)
	flag.Parse()
	if *showVer {
		fmt.Println("tabula", version)
		return
	}
	if *audioF.list {
		listAudioDevices(os.Stdout)
		return
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	log.Info("tabula", "version", version)

	api := webapi.New(log)
	if *resetPw {
		if err := api.SetAccessFile(*accessF); err != nil {
			log.Error("-access", "err", err)
			os.Exit(1)
		}
		had, err := api.ResetPassword()
		if err != nil {
			log.Error("-reset-password", "err", err)
			os.Exit(1)
		}
		if had {
			fmt.Println("engineer password removed from", *accessF)
		} else {
			fmt.Println("no engineer password in", *accessF)
		}
		return
	}
	guestRole, err := webapi.ParseRole(*guest)
	if err == nil && guestRole == webapi.RoleMusician {
		err = errors.New("a musician needs an access link (their mix)")
	}
	if err != nil {
		log.Error("-guest", "err", err)
		os.Exit(2)
	}
	api.SetGuestRole(guestRole)
	if err := api.SetAccessFile(*accessF); err != nil {
		log.Error("-access", "err", err)
		os.Exit(1)
	}
	if guestRole == webapi.RoleEngineer && api.HasPassword() {
		log.Error("-guest engineer makes every device the engineer, but an engineer password is set: choose another guest role, or remove the password with -reset-password")
		os.Exit(2)
	}
	api.SetSessionsDir(*sessions)
	audioCfg, audioFile, err := audioF.load(*sessions)
	if err != nil {
		log.Error("audio", "err", err)
		os.Exit(2)
	}
	api.SetAudioConfig(audioCfg, audioFile)
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
	api.SetListenAddr(ln.Addr().String())
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
