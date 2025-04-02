package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"os/signal"

	"github.com/mashiike/simplemqhttp"
)

func main() {
	var (
		queueName string
	)
	flag.StringVar(&queueName, "queue", "", "queue name")
	flag.Parse()

	apikey := os.Getenv("SACLOUD_API_KEY")
	if apikey == "" {
		log.Fatal("SACLOUD_API_KEY is not set")
	}
	if queueName == "" {
		log.Fatal("queue name is required")
	}
	listener := simplemqhttp.NewListener(apikey, queueName)
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			dump, err := httputil.DumpRequest(r, true)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			fmt.Println(string(dump))
			fmt.Println()
			w.WriteHeader(http.StatusOK)
		}),
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()
	log.Println("server started", "queue", queueName)
	<-ctx.Done()
	log.Println("shutting down server")
	if err := server.Shutdown(context.Background()); err != nil {
		log.Fatalf("server shutdown error: %v", err)
	}
	log.Println("server stopped")
}
