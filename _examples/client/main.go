package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"

	"github.com/mashiike/simplemqhttp"
)

func main() {
	var (
		queueName string
		content   string
	)
	flag.StringVar(&queueName, "queue", "", "queue name")
	flag.StringVar(&content, "content", "hello world", "message content")
	flag.Parse()

	apikey := os.Getenv("SACLOUD_API_KEY")
	if apikey == "" {
		log.Fatal("SACLOUD_API_KEY is not set")
	}
	if queueName == "" {
		log.Fatal("queue name is required")
	}
	transport := simplemqhttp.NewTransport(apikey, queueName)
	client := &http.Client{
		Transport: transport,
	}
	resp, err := client.Post("/", "text/plain", strings.NewReader(content))
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	bs, err := httputil.DumpResponse(resp, true)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(bs))
}
