package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"roleloom/internal/httpapi"
	"roleloom/internal/store"
)

func testServer(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err = st.SyncAdminPassword(context.Background(), "a secure admin password"); err != nil {
		t.Fatal(err)
	}
	server, err := httpapi.New(httpapi.Options{Store: st, MasterKey: bytes.Repeat([]byte{3}, 32), CookieSecure: true})
	if err != nil {
		t.Fatal(err)
	}
	return server.Handler(), st
}
func request(t *testing.T, h http.Handler, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1234"
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func login(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	w := request(t, h, "POST", "http://example.test/api/auth/login", `{"password":"a secure admin password"}`, nil)
	if w.Code != 200 {
		t.Fatalf("login status=%d body=%s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie=%#v", cookies)
	}
	return cookies[0]
}
func TestAuthenticationOriginAndSecretRedaction(t *testing.T) {
	h, _ := testServer(t)
	if w := request(t, h, "GET", "http://example.test/api/model-profiles", "", nil); w.Code != 401 {
		t.Fatalf("unauthorized=%d", w.Code)
	}
	cookie := login(t, h)
	for _, path := range []string{"/api/model-profiles", "/api/characters", "/api/conversations"} {
		empty := request(t, h, "GET", "http://example.test"+path, "", cookie)
		if empty.Code != 200 || strings.TrimSpace(empty.Body.String()) != "[]" {
			t.Fatalf("empty list %s = %d %q", path, empty.Code, empty.Body.String())
		}
	}
	body := `{"name":"main","provider":"openai","api_url":"https://example.test/v1/chat/completions","api_key":"top-secret","model":"gpt-test","is_default":true}`
	w := request(t, h, "POST", "http://example.test/api/model-profiles", body, cookie)
	if w.Code != 201 {
		t.Fatalf("create=%d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "top-secret") || strings.Contains(w.Body.String(), "api_key_encrypted") {
		t.Fatal("API key leaked")
	}
	var created struct {
		ID     string `json:"id"`
		HasKey bool   `json:"has_api_key"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || !created.HasKey {
		t.Fatalf("created=%#v err=%v", created, err)
	}
	w = request(t, h, "PATCH", "http://example.test/api/model-profiles/"+created.ID, `{"api_key":""}`, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"has_api_key":true`) {
		t.Fatalf("empty key did not preserve secret: %d %s", w.Code, w.Body.String())
	}
	w = request(t, h, "PATCH", "http://example.test/api/model-profiles/"+created.ID, `{"clear_api_key":true}`, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"has_api_key":false`) || strings.Contains(w.Body.String(), "top-secret") {
		t.Fatalf("clear key failed: %d %s", w.Code, w.Body.String())
	}
	r := httptest.NewRequest("POST", "http://example.test/api/model-profiles", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://evil.test")
	r.AddCookie(cookie)
	blocked := httptest.NewRecorder()
	h.ServeHTTP(blocked, r)
	if blocked.Code != 403 {
		t.Fatalf("cross-origin status=%d", blocked.Code)
	}
}
