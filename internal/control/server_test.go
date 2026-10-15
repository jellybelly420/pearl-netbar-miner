package control

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jellybelly420/pearl-netbar-miner/internal/protocol"
)

func TestFleetCommandRoundTrip(t *testing.T) {
	store, err := OpenStore(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	secret, err := store.Enroll("seat-001")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(store, "12345678901234567890123456789012"))
	defer server.Close()

	adminBody := bytes.NewBufferString(`{"type":"set-mode","payload":{"mode":"smart"}}`)
	adminRequest, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/admin/agents/seat-001/commands", adminBody)
	adminRequest.Header.Set("Authorization", "Bearer 12345678901234567890123456789012")
	adminRequest.Header.Set("Content-Type", "application/json")
	adminResponse, err := http.DefaultClient.Do(adminRequest)
	if err != nil || adminResponse.StatusCode != http.StatusCreated {
		t.Fatalf("queue command failed: %v status=%v", err, adminResponse.StatusCode)
	}
	adminResponse.Body.Close()

	agentRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/agent/commands?after=0", nil)
	agentRequest.Header.Set(protocol.HeaderAgent, "seat-001")
	if err := protocol.SignRequest(agentRequest, secret, time.Now()); err != nil {
		t.Fatal(err)
	}
	agentResponse, err := http.DefaultClient.Do(agentRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer agentResponse.Body.Close()
	var commands []Command
	if err := json.NewDecoder(agentResponse.Body).Decode(&commands); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0].Type != "set-mode" {
		t.Fatalf("unexpected commands: %+v", commands)
	}
}

func TestRejectsReplay(t *testing.T) {
	store, _ := OpenStore(t.TempDir() + "/state.json")
	secret, _ := store.Enroll("seat-001")
	handler := NewServer(store, "12345678901234567890123456789012")
	request, _ := http.NewRequest(http.MethodGet, "/api/v1/agent/commands?after=0", nil)
	request.Header.Set(protocol.HeaderAgent, "seat-001")
	_ = protocol.SignRequest(request, secret, time.Now())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("first request failed: %d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected replay rejection, got %d", recorder.Code)
	}
}

func TestAdminEnrollsRunningServer(t *testing.T) {
	store, _ := OpenStore(t.TempDir() + "/state.json")
	handler := NewServer(store, "12345678901234567890123456789012")
	request, _ := http.NewRequest(http.MethodPost, "/api/v1/admin/agents/seat-002/enroll", nil)
	request.Header.Set("Authorization", "Bearer 12345678901234567890123456789012")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected enrollment, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if secret, ok := store.Secret("seat-002"); !ok || len(secret) < 32 {
		t.Fatal("running server did not retain enrolled agent")
	}
}
