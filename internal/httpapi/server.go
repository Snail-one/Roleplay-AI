package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const maxRequestBodySize = 1 << 20
const maxSessions = 1000

type ChatAgent interface {
	Chat(ctx context.Context, input string) (string, error)
}

type AgentFactory func() (ChatAgent, error)

type Options struct {
	SessionTTL     time.Duration
	AllowedOrigins []string
	Logf           func(format string, arguments ...any)
}

type Server struct {
	agentFactory AgentFactory
	sessionTTL   time.Duration
	origins      map[string]struct{}
	allowAll     bool
	logf         func(string, ...any)
	mutex        sync.Mutex
	sessions     map[string]*session
}

type session struct {
	mutex    sync.Mutex
	agent    ChatAgent
	lastUsed time.Time
	active   int
}

type chatRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

type resetRequest struct {
	SessionID string `json:"session_id"`
}

func New(agentFactory AgentFactory, options Options) (*Server, error) {
	if agentFactory == nil {
		return nil, errors.New("agent factory is required")
	}
	sessionTTL := options.SessionTTL
	if sessionTTL == 0 {
		sessionTTL = 2 * time.Hour
	}
	if sessionTTL < time.Minute {
		return nil, errors.New("session TTL must be at least one minute")
	}
	origins := make(map[string]struct{}, len(options.AllowedOrigins))
	allowAll := false
	for _, origin := range options.AllowedOrigins {
		origin = strings.TrimRight(strings.TrimSpace(origin), "/")
		if origin == "" {
			continue
		}
		if origin == "*" {
			allowAll = true
			continue
		}
		origins[origin] = struct{}{}
	}
	logf := options.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Server{
		agentFactory: agentFactory, sessionTTL: sessionTTL,
		origins: origins, allowAll: allowAll, logf: logf,
		sessions: make(map[string]*session),
	}, nil
}

func (s *Server) Handler() http.Handler {
	return s.withCORS(http.HandlerFunc(s.route))
}

func (s *Server) route(response http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case "/api/health":
		if request.Method != http.MethodGet {
			methodNotAllowed(response, http.MethodGet)
			return
		}
		writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
	case "/api/chat":
		if request.Method != http.MethodPost {
			methodNotAllowed(response, http.MethodPost)
			return
		}
		s.handleChat(response, request)
	case "/api/sessions/reset":
		if request.Method != http.MethodPost {
			methodNotAllowed(response, http.MethodPost)
			return
		}
		s.handleReset(response, request)
	default:
		writeError(response, http.StatusNotFound, "not_found", "接口不存在")
	}
}

func (s *Server) handleChat(response http.ResponseWriter, request *http.Request) {
	var input chatRequest
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	input.Message = strings.TrimSpace(input.Message)
	if input.Message == "" {
		writeError(response, http.StatusBadRequest, "empty_message", "消息不能为空")
		return
	}
	if len([]rune(input.Message)) > 32_000 {
		writeError(response, http.StatusRequestEntityTooLarge, "message_too_long", "消息不能超过 32000 个字符")
		return
	}

	sessionID, chatSession, err := s.acquireSession(strings.TrimSpace(input.SessionID))
	if err != nil {
		s.logf("创建 Web 会话失败: %v", err)
		writeError(response, http.StatusServiceUnavailable, "session_unavailable", "暂时无法创建会话")
		return
	}
	defer s.releaseSession(chatSession)

	chatSession.mutex.Lock()
	answer, err := chatSession.agent.Chat(request.Context(), input.Message)
	chatSession.mutex.Unlock()
	if err != nil {
		if request.Context().Err() == nil {
			s.logf("Web Agent 处理失败（session_id=%s）: %v", sessionID, err)
		}
		writeError(response, http.StatusBadGateway, "agent_error", "AI 暂时无法回答，请稍后重试")
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{
		"session_id": sessionID,
		"answer":     answer,
	})
}

func (s *Server) handleReset(response http.ResponseWriter, request *http.Request) {
	var input resetRequest
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	s.mutex.Lock()
	delete(s.sessions, strings.TrimSpace(input.SessionID))
	s.cleanupLocked(time.Now())
	s.mutex.Unlock()
	writeJSON(response, http.StatusOK, map[string]bool{"reset": true})
}

func (s *Server) acquireSession(requestedID string) (string, *session, error) {
	now := time.Now()
	s.mutex.Lock()
	s.cleanupLocked(now)
	if requestedID != "" {
		if existing, found := s.sessions[requestedID]; found {
			existing.active++
			existing.lastUsed = now
			s.mutex.Unlock()
			return requestedID, existing, nil
		}
	}
	s.mutex.Unlock()

	chatAgent, err := s.agentFactory()
	if err != nil {
		return "", nil, err
	}
	sessionID, err := newSessionID()
	if err != nil {
		return "", nil, err
	}
	created := &session{agent: chatAgent, lastUsed: now, active: 1}
	s.mutex.Lock()
	s.cleanupLocked(now)
	if len(s.sessions) >= maxSessions {
		var oldestID string
		var oldestTime time.Time
		for currentID, current := range s.sessions {
			if current.active != 0 {
				continue
			}
			if oldestID == "" || current.lastUsed.Before(oldestTime) {
				oldestID, oldestTime = currentID, current.lastUsed
			}
		}
		if oldestID == "" {
			s.mutex.Unlock()
			return "", nil, errors.New("maximum active sessions reached")
		}
		delete(s.sessions, oldestID)
	}
	s.sessions[sessionID] = created
	s.mutex.Unlock()
	return sessionID, created, nil
}

func (s *Server) releaseSession(chatSession *session) {
	s.mutex.Lock()
	chatSession.active--
	chatSession.lastUsed = time.Now()
	s.mutex.Unlock()
}

func (s *Server) cleanupLocked(now time.Time) {
	for sessionID, chatSession := range s.sessions {
		if chatSession.active == 0 && now.Sub(chatSession.lastUsed) > s.sessionTTL {
			delete(s.sessions, sessionID)
		}
	}
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		origin := strings.TrimRight(strings.TrimSpace(request.Header.Get("Origin")), "/")
		if origin != "" {
			_, allowed := s.origins[origin]
			if s.allowAll || allowed {
				response.Header().Set("Access-Control-Allow-Origin", origin)
				response.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				response.Header().Set("Access-Control-Allow-Headers", "Content-Type")
				response.Header().Set("Vary", "Origin")
			}
		}
		if request.Method == http.MethodOptions {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func decodeJSON(request *http.Request, target any) error {
	reader := http.MaxBytesReader(nil, request.Body, maxRequestBodySize)
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("请求 JSON 无效: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("请求只能包含一个 JSON 对象")
		}
		return fmt.Errorf("请求 JSON 无效: %w", err)
	}
	return nil
}

func newSessionID() (string, error) {
	data := make([]byte, 24)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate session ID: %w", err)
	}
	return hex.EncodeToString(data), nil
}

func methodNotAllowed(response http.ResponseWriter, allowed string) {
	response.Header().Set("Allow", allowed)
	writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "请求方法不支持")
}

func writeError(response http.ResponseWriter, status int, code, message string) {
	writeJSON(response, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func writeJSON(response http.ResponseWriter, status int, payload any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(payload)
}
