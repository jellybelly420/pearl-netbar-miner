package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jellybelly420/pearl-netbar-miner/internal/control"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: pearl-control <serve|enroll>")
	}
	switch os.Args[1] {
	case "serve":
		serve(os.Args[2:])
	case "enroll":
		enroll(os.Args[2:])
	default:
		log.Fatalf("unknown command %q", os.Args[1])
	}
}

func serve(arguments []string) {
	flags := flag.NewFlagSet("serve", flag.ExitOnError)
	listen := flags.String("listen", "127.0.0.1:8080", "listen address")
	data := flags.String("data", "data/control-state.json", "state file")
	tlsCert := flags.String("tls-cert", "", "TLS certificate file")
	tlsKey := flags.String("tls-key", "", "TLS private key file")
	_ = flags.Parse(arguments)
	adminToken := os.Getenv("PEARL_ADMIN_TOKEN")
	if len(adminToken) < 32 {
		log.Fatal("PEARL_ADMIN_TOKEN must contain at least 32 characters")
	}
	store, err := control.OpenStore(*data)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		Addr: *listen, Handler: control.NewServer(store, adminToken),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
	}
	log.Printf("control service listening on %s", *listen)
	if (*tlsCert == "") != (*tlsKey == "") {
		log.Fatal("tls-cert and tls-key must be supplied together")
	}
	if *tlsCert != "" {
		log.Fatal(server.ListenAndServeTLS(*tlsCert, *tlsKey))
	}
	log.Fatal(server.ListenAndServe())
}

func enroll(arguments []string) {
	flags := flag.NewFlagSet("enroll", flag.ExitOnError)
	data := flags.String("data", "data/control-state.json", "state file")
	id := flags.String("id", "", "agent ID")
	serviceURL := flags.String("url", "", "running control service URL; omit for offline enrollment")
	_ = flags.Parse(arguments)
	if !idPattern.MatchString(*id) {
		log.Fatal("id must contain 1-64 letters, digits, or hyphens")
	}
	if *serviceURL != "" {
		enrollOnline(*serviceURL, *id)
		return
	}
	store, err := control.OpenStore(*data)
	if err != nil {
		log.Fatal(err)
	}
	secret, err := store.Enroll(*id)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("agent_id=%s\nagent_secret=%s\n", *id, secret)
}

func enrollOnline(serviceURL, id string) {
	adminToken := os.Getenv("PEARL_ADMIN_TOKEN")
	if len(adminToken) < 32 {
		log.Fatal("PEARL_ADMIN_TOKEN must contain at least 32 characters")
	}
	parsed, err := url.Parse(serviceURL)
	loopback := parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost" || parsed.Hostname() == "::1"
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopback)) {
		log.Fatal("url must use HTTPS except for loopback development")
	}
	endpoint := strings.TrimRight(parsed.String(), "/") + "/api/v1/admin/agents/" + url.PathEscape(id) + "/enroll"
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(nil))
	if err != nil {
		log.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+adminToken)
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		log.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		log.Fatalf("enrollment failed: %s: %s", response.Status, body)
	}
	var result struct {
		AgentID     string `json:"agent_id"`
		AgentSecret string `json:"agent_secret"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("agent_id=%s\nagent_secret=%s\n", result.AgentID, result.AgentSecret)
}
