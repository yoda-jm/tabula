// Command tabula connects to an X32/M32 console (or tabula-emu) and serves a
// web GUI to browse and drive it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/yoda-jm/tabula/pkg/buildinfo"
	"github.com/yoda-jm/tabula/pkg/webapi"
	"github.com/yoda-jm/tabula/web"
)

// version is set at release build time.
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, nil)
	stop()
	os.Exit(code)
}

// run is tabula with its command line: it serves the GUI until ctx is done
// and returns the exit status (2 for a bad flag). ready, when set, is told
// the address the GUI listens on.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, ready func(net.Addr)) int {
	fs := flag.NewFlagSet("tabula", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		httpAddr = fs.String("http", "localhost:8033", "address to serve the GUI on (use :8033 to reach it from other devices)")
		consoleA = fs.String("console", "", "console (or tabula-emu) to connect to at start, e.g. 192.168.1.62")
		webDir   = fs.String("web-dir", "", "serve the GUI from this directory instead of the embedded build")
		sessions = fs.String("sessions", "sessions", "directory where recorded sessions are kept")
		scenes   = fs.String("scenes", "scenes", "directory of the scene files (.scn) to load into the console, or to save it as")
		record   = fs.Bool("record", false, "record from the start (stops on exit): the audio tracks at once when they are on, the console once connected and synced")
		accessF  = fs.String("access", "access.json", "file keeping the access links made on the Access page")
		guest    = fs.String("guest", "none", "role of other devices without an access link: none, viewer or engineer")
		resetPw  = fs.Bool("reset-password", false, "remove the engineer password (and its sessions) from the access file, then exit")
		hosts    = fs.String("allowed-hosts", "", "other host names the GUI is opened by, comma-separated (a DNS name, a reverse proxy); IP addresses, localhost and this machine's name always work")
		logF     = fs.String("log", "", `file the log is appended to, besides the terminal (default tabula.log in the -sessions directory; "none": the terminal only)`)
		showVer  = fs.Bool("version", false, "print the version and exit")
		audioF   = addAudioFlags(fs) // audio.go
	)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "tabula: unexpected argument %q (flags start with -)\n", fs.Arg(0))
		return 2
	}
	ver := buildinfo.Version(version)
	if *showVer {
		fmt.Fprintln(stdout, "tabula", ver)
		return 0
	}
	if *audioF.list {
		listAudioDevices(stdout)
		return 0
	}

	// the log goes to a file too: after a show, the terminal tabula ran in is often gone
	out := stderr
	logFile := logPath(*logF, *sessions)
	var logErr error
	if logFile != "" {
		f, err := openLog(logFile)
		if err != nil {
			logErr, logFile = err, ""
		} else {
			defer f.Close()
			out = io.MultiWriter(stderr, f)
			if abs, err := filepath.Abs(logFile); err == nil {
				logFile = abs
			}
		}
	}
	log := slog.New(slog.NewTextHandler(out, nil))
	log.Info("tabula", "version", ver, "args", strings.Join(args, " "))
	if logErr != nil {
		log.Warn("-log: the log goes to the terminal only", "err", logErr)
	}

	api := webapi.New(log)
	api.SetBuild(ver, logFile)
	if *resetPw {
		if err := api.SetAccessFile(*accessF); err != nil {
			log.Error("-access", "err", err)
			return 1
		}
		had, err := api.ResetPassword()
		if err != nil {
			log.Error("-reset-password", "err", err)
			return 1
		}
		if had {
			fmt.Fprintln(stdout, "engineer password removed from", *accessF)
		} else {
			fmt.Fprintln(stdout, "no engineer password in", *accessF)
		}
		return 0
	}
	if *hosts != "" {
		api.SetAllowedHosts(strings.Split(*hosts, ","))
	}
	guestRole, err := webapi.ParseRole(*guest)
	if err == nil && guestRole == webapi.RoleMusician {
		err = errors.New("a musician needs an access link (their mix)")
	}
	if err != nil {
		log.Error("-guest", "err", err)
		return 2
	}
	api.SetGuestRole(guestRole)
	if err := api.SetAccessFile(*accessF); err != nil {
		log.Error("-access", "err", err)
		return 1
	}
	if guestRole == webapi.RoleEngineer && api.HasPassword() {
		log.Error("-guest engineer makes every device the engineer, but an engineer password is set: choose another guest role, or remove the password with -reset-password")
		return 2
	}
	api.SetSessionsDir(*sessions)
	api.SetScenesDir(*scenes)
	audioCfg, audioFile, err := audioF.load(*sessions)
	if err != nil {
		log.Error("audio", "err", err)
		return 2
	}
	api.SetAudioConfig(audioCfg, audioFile)
	ln, err := webapi.Listen(*httpAddr)
	if err != nil {
		log.Error("listen", "err", err)
		return 1
	}
	defer api.Disconnect()
	defer api.StopRecording() // first: a recording ends cleanly, then the console is let go
	if *consoleA != "" {
		api.Connect(*consoleA)
	}
	if *record {
		api.RecordAtStart() // the audio from now when it is on, the console once synced
	}
	api.SetListenAddr(ln.Addr().String())
	srv := &http.Server{Handler: api.Handler(web.Handler(*webDir)), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(shut)
	}()
	log.Info("GUI", "url", "http://"+ln.Addr().String())
	if ready != nil {
		ready(ln.Addr())
	}
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("http", "err", err)
		return 1
	}
	log.Info("tabula stopped")
	return 0
}
