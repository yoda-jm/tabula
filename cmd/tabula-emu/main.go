// Command tabula-emu emulates an X32/M32 console on the network, so that
// remote apps (X32-Edit, M32-Edit, the tabula client…) can be used and
// tested without a console.
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
	"strings"
	"syscall"
	"time"

	"github.com/yoda-jm/tabula/pkg/emu"
	"github.com/yoda-jm/tabula/pkg/scene"
	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/state"
)

// version is set at release build time.
var version = "dev"

var models = []string{"X32", "X32RACK", "X32C", "X32P", "X32CORE", "M32", "M32R", "M32C"}

func main() {
	var (
		listen    = flag.String("listen", ":10023", "UDP address to serve the console protocol on")
		model     = flag.String("model", "M32C", "console model to report: "+strings.Join(models, ", "))
		name      = flag.String("name", "tabula-emu", "console network name (reported by /xinfo)")
		firmware  = flag.String("firmware", "4.06", "console firmware version to report")
		sceneFile = flag.String("scene", "", "scene file (.scn) to load at start")
		save      = flag.String("save", "", "scene file to write the state to on exit")
		httpAddr  = flag.String("http", "", "address of the read-only debug page, e.g. :8032 (disabled if empty)")
		unhandled = flag.String("unhandled-log", "", "append every unhandled request to this JSON lines file")
		verbose   = flag.Bool("v", false, "log every unhandled request, not only the first of each kind")
		showVer   = flag.Bool("version", false, "print the version and exit")
	)
	flag.Parse()
	if *showVer {
		fmt.Println("tabula-emu", version)
		return
	}

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	log.Info("tabula-emu", "version", version)
	if err := run(log, *listen, *model, *name, *firmware, *sceneFile, *save, *httpAddr, *unhandled); err != nil {
		log.Error("tabula-emu", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, listen, model, name, firmware, sceneFile, save, httpAddr, unhandled string) error {
	model = strings.ToUpper(model)
	if !contains(models, model) {
		return fmt.Errorf("unknown model %q (known: %s)", model, strings.Join(models, ", "))
	}
	st := state.New(schema.Build(schema.Model(model)))
	header := scene.Header("tabula-emu", "")
	if sceneFile != "" {
		h, err := loadScene(st, sceneFile, log)
		if err != nil {
			return err
		}
		if h != "" {
			header = h
		}
	}

	var jsonl io.Writer
	if unhandled != "" {
		f, err := os.OpenFile(unhandled, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		jsonl = f
	}

	srv := emu.New(emu.Config{Model: schema.Model(model), Name: name, Firmware: firmware, UnhandledLog: jsonl, Logger: log}, st)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if httpAddr != "" {
		ln, err := net.Listen("tcp", httpAddr)
		if err != nil {
			return err
		}
		hs := &http.Server{Handler: srv.DebugHandler(), ReadHeaderTimeout: 5 * time.Second}
		go func() {
			if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("debug page", "err", err)
			}
		}()
		defer hs.Close()
		log.Info("debug page", "url", "http://"+displayAddr(ln.Addr()))
	}

	err := srv.ListenAndServe(ctx, listen)
	if save != "" {
		if serr := saveScene(st, save, header); serr != nil {
			log.Error("save", "err", serr)
		} else {
			log.Info("state saved", "file", save)
		}
	}
	return err
}

func loadScene(st *state.Store, file string, log *slog.Logger) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	header, errs := scene.Load(f, st, "scene "+file)
	for _, e := range errs {
		log.Warn("scene line not applied", "file", file, "err", e)
	}
	log.Info("scene loaded", "file", file, "problems", len(errs))
	return header, nil
}

func saveScene(st *state.Store, file, header string) error {
	tmp := file + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := scene.Write(f, st, header); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

func displayAddr(a net.Addr) string {
	s := a.String()
	if strings.HasPrefix(s, "[::]:") {
		return "localhost" + strings.TrimPrefix(s, "[::]")
	}
	return s
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
