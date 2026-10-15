package miner

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
)

func TestFetchStatistics(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"uptime":123,"hashrate":{"total":[100.5,90.25,null],"highest":120},"results":{"shares_good":8,"shares_total":10},"connection":{"pool":"pool.example:9000"}}`)
	})}
	go server.Serve(listener)
	defer server.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	got, err := FetchStatistics(context.Background(), port)
	if err != nil {
		t.Fatal(err)
	}
	if got.Hashrate10s != 100.5 || got.Hashrate60s != 90.25 || got.Hashrate15m != 0 {
		t.Fatalf("unexpected hashrates: %+v", got)
	}
	if got.Accepted != 8 || got.Rejected != 2 || got.Pool != "pool.example:9000" {
		t.Fatalf("unexpected statistics: %+v", got)
	}
}
