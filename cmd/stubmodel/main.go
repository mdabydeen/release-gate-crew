// Command stubmodel serves the scripted OpenAI-compatible stub used by the
// crew's "offline" flavor.
//
//	go run ./cmd/stubmodel                 # listens on 127.0.0.1:8089
//	go run ./cmd/stubmodel -addr :9000 -log requests.jsonl
package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mdabydeen/release-gate-crew/internal/stub"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8089", "listen address")
	logPath := flag.String("log", "", "append one JSON line per request to this file")
	flag.Parse()

	var reqLog io.Writer
	if *logPath != "" {
		f, err := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			log.Fatalf("open log: %v", err)
		}
		defer f.Close()
		reqLog = f
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           &stub.Server{Log: reqLog},
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	log.Printf("stub model on http://%s/v1", *addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
