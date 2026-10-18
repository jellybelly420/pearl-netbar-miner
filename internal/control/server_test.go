package control

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestCommandAckRecordsResultAndAudit(t *testing.T) {
	store, _ := OpenStore(t.TempDir() + "/state.json")
	secret, _ := store.Enroll("seat-003")
	command, err := store.Queue("seat-003", "start", nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServer(store, "12345678901234567890123456789012")
	body := bytes.NewBufferString(`{"result":"error","message":"disabled by local policy"}`)
	request, _ := http.NewRequest(http.MethodPost, "/api/v1/agent/commands/1/ack", body)
	request.Header.Set(protocol.HeaderAgent, "seat-003")
	request.Header.Set("Content-Type", "application/json")
	_ = protocol.SignRequest(request, secret, time.Now())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("ack failed: %d %s", recorder.Code, recorder.Body.String())
	}
	agents := store.Agents()
	if len(agents) != 1 || len(agents[0].Commands) != 1 || agents[0].Commands[0].Result != "error" {
		t.Fatalf("ack result not stored: %+v", agents)
	}
	if agents[0].Commands[0].Sequence != command.Sequence || len(store.Audit(10)) < 3 {
		t.Fatal("command audit trail is incomplete")
	}
}

func TestBatchCommandAndHistory(t *testing.T) {
	store, _ := OpenStore(t.TempDir() + "/state.json")
	_, _ = store.Enroll("seat-a")
	_, _ = store.Enroll("seat-b")
	commands, err := store.QueueBatch([]string{"seat-a", "seat-b"}, "set-mode", map[string]string{"mode": "smart"})
	if err != nil || len(commands) != 2 {
		t.Fatalf("batch failed: %v %+v", err, commands)
	}
	for index := 0; index < 250; index++ {
		if err := store.Heartbeat("seat-a", Heartbeat{Hashrate10s: float64(index)}); err != nil {
			t.Fatal(err)
		}
	}
	history, err := store.History("seat-a")
	if err != nil || len(history) != 240 || history[0].Hashrate10s != 10 {
		t.Fatalf("history retention failed: len=%d err=%v", len(history), err)
	}
}

func TestRoleBasedAccess(t *testing.T) {
	store, _ := OpenStore(t.TempDir() + "/state.json")
	_, _ = store.Enroll("seat-rbac")
	handler := NewServerWithAccess(store, Access{
		AdminToken:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		OperatorToken: "oooooooooooooooooooooooooooooooo",
		ViewerToken:   "vvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvv",
	})

	listRequest, _ := http.NewRequest(http.MethodGet, "/api/v1/admin/agents", nil)
	listRequest.Header.Set("Authorization", "Bearer vvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvv")
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("viewer could not list agents: %d", listRecorder.Code)
	}

	startRequest, _ := http.NewRequest(http.MethodPost, "/api/v1/admin/agents/seat-rbac/commands", bytes.NewBufferString(`{"type":"start","payload":{}}`))
	startRequest.Header.Set("Authorization", "Bearer oooooooooooooooooooooooooooooooo")
	startRequest.Header.Set("Content-Type", "application/json")
	startRecorder := httptest.NewRecorder()
	handler.ServeHTTP(startRecorder, startRequest)
	if startRecorder.Code != http.StatusCreated {
		t.Fatalf("operator could not start agent: %d %s", startRecorder.Code, startRecorder.Body.String())
	}

	miningRequest, _ := http.NewRequest(http.MethodPost, "/api/v1/admin/agents/seat-rbac/commands", bytes.NewBufferString(`{"type":"set-mining","payload":{"power_limit":"150"}}`))
	miningRequest.Header.Set("Authorization", "Bearer oooooooooooooooooooooooooooooooo")
	miningRequest.Header.Set("Content-Type", "application/json")
	miningRecorder := httptest.NewRecorder()
	handler.ServeHTTP(miningRecorder, miningRequest)
	if miningRecorder.Code != http.StatusForbidden {
		t.Fatalf("operator should not change mining configuration: %d", miningRecorder.Code)
	}
}

func TestRejectsTrailingAndOversizedAdminJSON(t *testing.T) {
	store, _ := OpenStore(t.TempDir() + "/state.json")
	_, _ = store.Enroll("seat-json")
	handler := NewServer(store, "12345678901234567890123456789012")

	for name, body := range map[string]string{
		"trailing value": `{"type":"start","payload":{}} {}`,
		"oversized":      `{"type":"start","payload":{},"padding":"` + strings.Repeat("a", 1<<20) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/agents/seat-json/commands", strings.NewReader(body))
			request.Header.Set("Authorization", "Bearer 12345678901234567890123456789012")
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected malformed JSON rejection, got %d", recorder.Code)
			}
		})
	}
}

func TestPersistenceFailureRollsBackMemory(t *testing.T) {
	root := t.TempDir()
	store, _ := OpenStore(filepath.Join(root, "state.json"))
	_, _ = store.Enroll("seat-disk")
	badParent := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(badParent, []byte("block writes"), 0o600); err != nil {
		t.Fatal(err)
	}
	store.path = filepath.Join(badParent, "state.json")

	if _, err := store.Queue("seat-disk", "start", nil); err == nil {
		t.Fatal("expected queue persistence failure")
	}
	agents := store.Agents()
	if len(agents) != 1 || agents[0].NextSequence != 1 || len(agents[0].Commands) != 0 || len(store.Audit(10)) != 1 {
		t.Fatalf("failed queue leaked into memory: %+v audit=%+v", agents, store.Audit(10))
	}
	if err := store.Heartbeat("seat-disk", Heartbeat{Hashrate10s: 1}); err == nil {
		t.Fatal("expected heartbeat persistence failure")
	}
	if store.Agents()[0].LastHeartbeat != nil {
		t.Fatal("failed heartbeat leaked into memory")
	}
}
