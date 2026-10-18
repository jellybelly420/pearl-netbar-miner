package control

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jellybelly420/pearl-netbar-miner/internal/protocol"
)

type Server struct {
	store   *Store
	access  Access
	nonceMu sync.Mutex
	nonces  map[string]time.Time
}

type Role int

const (
	RoleViewer Role = iota + 1
	RoleOperator
	RoleAdmin
)

type Access struct {
	AdminToken    string
	OperatorToken string
	ViewerToken   string
}

func NewServer(store *Store, adminToken string) http.Handler {
	return NewServerWithAccess(store, Access{AdminToken: adminToken})
}

func NewServerWithAccess(store *Store, access Access) http.Handler {
	server := &Server{store: store, access: access, nonces: make(map[string]time.Time)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /", server.dashboard)
	mux.HandleFunc("POST /api/v1/agent/heartbeat", server.agentHeartbeat)
	mux.HandleFunc("GET /api/v1/agent/commands", server.agentCommands)
	mux.HandleFunc("POST /api/v1/agent/commands/{sequence}/ack", server.agentAck)
	mux.HandleFunc("GET /api/v1/admin/agents", server.adminAgents)
	mux.HandleFunc("GET /api/v1/admin/agents/{id}/history", server.adminHistory)
	mux.HandleFunc("GET /api/v1/admin/audit", server.adminAudit)
	mux.HandleFunc("POST /api/v1/admin/agents/{id}/enroll", server.adminEnroll)
	mux.HandleFunc("POST /api/v1/admin/agents/{id}/commands", server.adminCommand)
	mux.HandleFunc("POST /api/v1/admin/commands/batch", server.adminBatchCommand)
	return securityHeaders(mux)
}

func (s *Server) agentHeartbeat(writer http.ResponseWriter, request *http.Request) {
	id, body, ok := s.authenticateAgent(writer, request)
	if !ok {
		return
	}
	var heartbeat Heartbeat
	if err := json.Unmarshal(body, &heartbeat); err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse(err))
		return
	}
	if err := s.store.Heartbeat(id, heartbeat); err != nil {
		writeJSON(writer, http.StatusNotFound, errorResponse(err))
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) agentCommands(writer http.ResponseWriter, request *http.Request) {
	id, _, ok := s.authenticateAgent(writer, request)
	if !ok {
		return
	}
	after, err := strconv.ParseUint(request.URL.Query().Get("after"), 10, 64)
	if err != nil && request.URL.Query().Get("after") != "" {
		writeJSON(writer, http.StatusBadRequest, errorResponse(err))
		return
	}
	commands, err := s.store.Pending(id, after)
	if err != nil {
		writeJSON(writer, http.StatusNotFound, errorResponse(err))
		return
	}
	writeJSON(writer, http.StatusOK, commands)
}

func (s *Server) agentAck(writer http.ResponseWriter, request *http.Request) {
	id, _, ok := s.authenticateAgent(writer, request)
	if !ok {
		return
	}
	sequence, err := strconv.ParseUint(request.PathValue("sequence"), 10, 64)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse(err))
		return
	}
	var acknowledgement CommandAck
	if err := decodeJSON(request, &acknowledgement); err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse(err))
		return
	}
	if err := s.store.Ack(id, sequence, acknowledgement); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusNotFound
		}
		writeJSON(writer, status, errorResponse(err))
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminHistory(writer http.ResponseWriter, request *http.Request) {
	if !s.authenticateRole(writer, request, RoleViewer) {
		return
	}
	history, err := s.store.History(request.PathValue("id"))
	if err != nil {
		writeJSON(writer, http.StatusNotFound, errorResponse(err))
		return
	}
	writeJSON(writer, http.StatusOK, history)
}

func (s *Server) adminAudit(writer http.ResponseWriter, request *http.Request) {
	if !s.authenticateRole(writer, request, RoleViewer) {
		return
	}
	limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
	writeJSON(writer, http.StatusOK, s.store.Audit(limit))
}

func (s *Server) adminAgents(writer http.ResponseWriter, request *http.Request) {
	if !s.authenticateRole(writer, request, RoleViewer) {
		return
	}
	writeJSON(writer, http.StatusOK, s.store.Agents())
}

func (s *Server) adminEnroll(writer http.ResponseWriter, request *http.Request) {
	if !s.authenticateAdmin(writer, request) {
		return
	}
	id := request.PathValue("id")
	secret, err := s.store.Enroll(id)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse(err))
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]string{"agent_id": id, "agent_secret": secret})
}

func (s *Server) adminCommand(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Type    string            `json:"type"`
		Payload map[string]string `json:"payload"`
	}
	if err := decodeJSON(request, &body); err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse(err))
		return
	}
	if !s.authenticateRole(writer, request, requiredRole(body.Type)) {
		return
	}
	command, err := s.store.Queue(request.PathValue("id"), body.Type, body.Payload)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusNotFound
		}
		writeJSON(writer, status, errorResponse(err))
		return
	}
	writeJSON(writer, http.StatusCreated, command)
}

func (s *Server) adminBatchCommand(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		AgentIDs []string          `json:"agent_ids"`
		Type     string            `json:"type"`
		Payload  map[string]string `json:"payload"`
	}
	if err := decodeJSON(request, &body); err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse(err))
		return
	}
	if !s.authenticateRole(writer, request, requiredRole(body.Type)) {
		return
	}
	commands, err := s.store.QueueBatch(body.AgentIDs, body.Type, body.Payload)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse(err))
		return
	}
	writeJSON(writer, http.StatusCreated, commands)
}

func (s *Server) authenticateAgent(writer http.ResponseWriter, request *http.Request) (string, []byte, bool) {
	id := request.Header.Get(protocol.HeaderAgent)
	secret, exists := s.store.Secret(id)
	if !exists {
		writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "unknown agent"})
		return "", nil, false
	}
	body, err := protocol.VerifyRequest(request, secret, time.Now(), 5*time.Minute)
	if err != nil {
		writeJSON(writer, http.StatusUnauthorized, errorResponse(err))
		return "", nil, false
	}
	nonce := request.Header.Get(protocol.HeaderNonce)
	if !s.acceptNonce(id+":"+nonce, time.Now()) {
		writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "replayed nonce"})
		return "", nil, false
	}
	return id, body, true
}

func (s *Server) acceptNonce(key string, now time.Time) bool {
	s.nonceMu.Lock()
	defer s.nonceMu.Unlock()
	for nonce, expiry := range s.nonces {
		if now.After(expiry) {
			delete(s.nonces, nonce)
		}
	}
	if _, exists := s.nonces[key]; exists {
		return false
	}
	s.nonces[key] = now.Add(10 * time.Minute)
	return true
}

func (s *Server) authenticateAdmin(writer http.ResponseWriter, request *http.Request) bool {
	return s.authenticateRole(writer, request, RoleAdmin)
}

func (s *Server) authenticateRole(writer http.ResponseWriter, request *http.Request, required Role) bool {
	provided := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
	role := Role(0)
	for _, candidate := range []struct {
		token string
		role  Role
	}{{s.access.AdminToken, RoleAdmin}, {s.access.OperatorToken, RoleOperator}, {s.access.ViewerToken, RoleViewer}} {
		if candidate.token == "" || len(provided) != len(candidate.token) {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(provided), []byte(candidate.token)) == 1 {
			role = candidate.role
			break
		}
	}
	if role < required {
		status := http.StatusForbidden
		if role == 0 {
			status = http.StatusUnauthorized
		}
		writeJSON(writer, status, map[string]string{"error": "insufficient role"})
		return false
	}
	return true
}

func requiredRole(commandType string) Role {
	switch commandType {
	case "start", "stop", "resume", "set-mode", "set-policy":
		return RoleOperator
	default:
		return RoleAdmin
	}
}

func decodeJSON(request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(nil, request.Body, 1<<20)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.New("request body must contain one JSON value")
		}
		return err
	}
	return nil
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func errorResponse(err error) map[string]string { return map[string]string{"error": err.Error()} }

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'")
		next.ServeHTTP(writer, request)
	})
}
