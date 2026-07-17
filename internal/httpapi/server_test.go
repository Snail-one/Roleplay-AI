package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeAgent struct {
	inputs []string
}

func (f *fakeAgent) Chat(_ context.Context, input string) (string, error) {
	f.inputs = append(f.inputs, input)
	return "answer " + input, nil
}

func TestServerChatSessionsAndReset(t *testing.T) {
	var mutex sync.Mutex
	var agents []*fakeAgent
	server, err := New(func() (ChatAgent, error) {
		mutex.Lock()
		defer mutex.Unlock()
		agent := &fakeAgent{}
		agents = append(agents, agent)
		return agent, nil
	}, Options{SessionTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()

	first := sendChat(t, handler, "", "hello")
	if len(first.SessionID) != 48 || first.Answer != "answer hello" {
		t.Fatalf("first response = %#v", first)
	}
	second := sendChat(t, handler, first.SessionID, "again")
	if second.SessionID != first.SessionID || second.Answer != "answer again" {
		t.Fatalf("second response = %#v", second)
	}
	other := sendChat(t, handler, "", "other")
	if other.SessionID == first.SessionID {
		t.Fatal("different browser session reused the same session ID")
	}
	if len(agents) != 2 || len(agents[0].inputs) != 2 || len(agents[1].inputs) != 1 {
		t.Fatalf("agents = %#v", agents)
	}

	reset := httptest.NewRequest(http.MethodPost, "/api/sessions/reset", strings.NewReader(`{"session_id":"`+first.SessionID+`"}`))
	reset.Header.Set("Content-Type", "application/json")
	resetResponse := httptest.NewRecorder()
	handler.ServeHTTP(resetResponse, reset)
	if resetResponse.Code != http.StatusOK {
		t.Fatalf("reset status = %d, body = %s", resetResponse.Code, resetResponse.Body.String())
	}
	afterReset := sendChat(t, handler, first.SessionID, "fresh")
	if afterReset.SessionID == first.SessionID || len(agents) != 3 {
		t.Fatalf("after reset = %#v, agent count = %d", afterReset, len(agents))
	}
}

func TestServerValidationHealthAndCORS(t *testing.T) {
	server, err := New(func() (ChatAgent, error) { return &fakeAgent{}, nil }, Options{
		SessionTTL: time.Hour, AllowedOrigins: []string{"http://localhost:5173"},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if health.Code != http.StatusOK || !strings.Contains(health.Body.String(), `"status":"ok"`) {
		t.Fatalf("health = %d, %s", health.Code, health.Body.String())
	}

	invalid := httptest.NewRecorder()
	handler.ServeHTTP(invalid, httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"message":""}`)))
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "empty_message") {
		t.Fatalf("invalid request = %d, %s", invalid.Code, invalid.Body.String())
	}

	preflightRequest := httptest.NewRequest(http.MethodOptions, "/api/chat", nil)
	preflightRequest.Header.Set("Origin", "http://localhost:5173")
	preflight := httptest.NewRecorder()
	handler.ServeHTTP(preflight, preflightRequest)
	if preflight.Code != http.StatusNoContent || preflight.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("preflight = %d, headers = %#v", preflight.Code, preflight.Header())
	}

	wrongMethod := httptest.NewRecorder()
	handler.ServeHTTP(wrongMethod, httptest.NewRequest(http.MethodGet, "/api/chat", nil))
	if wrongMethod.Code != http.StatusMethodNotAllowed || wrongMethod.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("wrong method = %d, headers = %#v", wrongMethod.Code, wrongMethod.Header())
	}
}

func TestSPAHandler(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "index.html"), []byte("<main>app</main>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "assets", "app.js"), []byte("app()"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := NewSPAHandler(directory)
	if err != nil {
		t.Fatal(err)
	}

	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/conversation/123", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "<main>app</main>") {
		t.Fatalf("SPA page = %d, %s", page.Code, page.Body.String())
	}
	asset := httptest.NewRecorder()
	handler.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if asset.Code != http.StatusOK || asset.Body.String() != "app()" {
		t.Fatalf("asset = %d, %s", asset.Code, asset.Body.String())
	}
	missingAsset := httptest.NewRecorder()
	handler.ServeHTTP(missingAsset, httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil))
	if missingAsset.Code != http.StatusNotFound {
		t.Fatalf("missing asset status = %d", missingAsset.Code)
	}
}

type chatResult struct {
	SessionID string `json:"session_id"`
	Answer    string `json:"answer"`
}

func sendChat(t *testing.T, handler http.Handler, sessionID, message string) chatResult {
	t.Helper()
	body, err := json.Marshal(map[string]string{"session_id": sessionID, "message": message})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("chat status = %d, body = %s", response.Code, response.Body.String())
	}
	var result chatResult
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}
