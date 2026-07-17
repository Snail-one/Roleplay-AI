package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"roleloom/internal/app"
	"roleloom/internal/domain"
	"roleloom/internal/security"
	"roleloom/internal/store"
)

const (
	maxRequestBodySize = 1 << 20
	cookieName         = "roleloom_session"
	sessionTTL         = 24 * time.Hour
)

type Options struct {
	Store        *store.Store
	MasterKey    []byte
	CookieSecure bool
	Logf         func(string, ...any)
	Service      *app.ConversationService
}
type Server struct {
	store        *store.Store
	masterKey    []byte
	cookieSecure bool
	logf         func(string, ...any)
	service      *app.ConversationService
	limiter      *rateLimiter
	generationMu sync.Mutex
	generations  map[string]bool
}

func New(options Options) (*Server, error) {
	if options.Store == nil {
		return nil, errors.New("store is required")
	}
	if len(options.MasterKey) != 32 {
		return nil, errors.New("master key must be 32 bytes")
	}
	logf := options.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	service := options.Service
	if service == nil {
		service = app.NewConversationService(options.Store, options.MasterKey)
	}
	return &Server{store: options.Store, masterKey: options.MasterKey, cookieSecure: options.CookieSecure, logf: logf, service: service, limiter: newRateLimiter(), generations: map[string]bool{}}, nil
}
func (s *Server) Handler() http.Handler { return s.securityMiddleware(http.HandlerFunc(s.route)) }

func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	if path == "" {
		path = "/"
	}
	if path == "/api/health" {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
		return
	}
	if path == "/api/auth/login" {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		s.login(w, r)
		return
	}
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	switch path {
	case "/api/auth/session":
		if requireMethod(w, r, http.MethodGet) {
			writeJSON(w, 200, map[string]bool{"authenticated": true})
		}
		return
	case "/api/auth/logout":
		if requireMethod(w, r, http.MethodPost) {
			s.logout(w, r)
		}
		return
	case "/api/model-profiles":
		s.modelProfiles(w, r)
		return
	case "/api/characters":
		s.characters(w, r)
		return
	case "/api/conversations":
		s.conversations(w, r)
		return
	}
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) >= 3 && segments[0] == "api" {
		switch segments[1] {
		case "model-profiles":
			s.modelProfile(w, r, segments[2:])
			return
		case "characters":
			s.character(w, r, segments[2:])
			return
		case "conversations":
			s.conversation(w, r, segments[2:])
			return
		}
	}
	writeError(w, 404, "not_found", "接口不存在")
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := remoteIP(r)
	if !s.limiter.allow("login:"+ip, 5, 5*time.Minute, false) {
		writeError(w, 429, "rate_limited", "登录尝试过多，请稍后再试")
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	ok, err := s.store.VerifyAdminPassword(r.Context(), input.Password)
	if err != nil {
		s.logf("verify password: %v", err)
		writeError(w, 500, "internal_error", "无法验证登录")
		return
	}
	if !ok {
		s.limiter.record("login:" + ip)
		writeError(w, 401, "invalid_credentials", "密码错误")
		return
	}
	s.limiter.clear("login:" + ip)
	token, hash, err := security.NewToken()
	if err != nil {
		writeError(w, 500, "internal_error", "无法创建会话")
		return
	}
	expires := time.Now().Add(sessionTTL)
	if err = s.store.CreateSession(r.Context(), hash, expires); err != nil {
		writeError(w, 500, "internal_error", "无法创建会话")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: s.cookieSecure, SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: int(sessionTTL.Seconds())})
	writeJSON(w, 200, map[string]bool{"authenticated": true})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		_ = s.store.DeleteSession(r.Context(), security.TokenHash(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, Secure: s.cookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
	writeJSON(w, 200, map[string]bool{"authenticated": false})
}
func (s *Server) authorized(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return false
	}
	ok, err := s.store.SessionValid(r.Context(), security.TokenHash(c.Value), time.Now())
	return err == nil && ok
}

type modelInput struct {
	Name            *string `json:"name"`
	Provider        *string `json:"provider"`
	APIURL          *string `json:"api_url"`
	APIKey          *string `json:"api_key"`
	ClearAPIKey     bool    `json:"clear_api_key"`
	Model           *string `json:"model"`
	TimeoutSeconds  *int    `json:"timeout_seconds"`
	MaxOutputTokens *int    `json:"max_output_tokens"`
	ContextWindow   *int    `json:"context_window"`
	IsDefault       *bool   `json:"is_default"`
}

func (s *Server) modelProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.ListModelProfiles(r.Context())
		respond(w, items, err)
	case http.MethodPost:
		var in modelInput
		if err := decodeJSON(w, r, &in); err != nil {
			writeError(w, 400, "invalid_request", err.Error())
			return
		}
		p := domain.ModelProfile{}
		if err := s.applyModelInput(&p, in, true); err != nil {
			writeError(w, 400, "invalid_request", err.Error())
			return
		}
		saved, err := s.store.SaveModelProfile(r.Context(), p)
		if err != nil {
			respond(w, nil, err)
			return
		}
		writeJSON(w, 201, saved)
	default:
		methodNotAllowed(w, "GET, POST")
	}
}
func (s *Server) modelProfile(w http.ResponseWriter, r *http.Request, parts []string) {
	id := parts[0]
	if len(parts) == 2 && parts[1] == "test" {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		p, err := s.store.GetModelProfile(r.Context(), id)
		if err != nil {
			respond(w, nil, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Duration(p.TimeoutSeconds)*time.Second)
		defer cancel()
		detail, err := s.service.TestModel(ctx, p)
		if err != nil {
			category := classifyModelError(err)
			writeJSON(w, 200, map[string]any{"success": false, "category": category, "message": "连接测试失败（" + category + "），请检查地址、模型和密钥"})
			return
		}
		writeJSON(w, 200, map[string]any{"success": true, "message": detail})
		return
	}
	if len(parts) != 1 {
		writeError(w, 404, "not_found", "接口不存在")
		return
	}
	switch r.Method {
	case http.MethodGet:
		p, e := s.store.GetModelProfile(r.Context(), id)
		respond(w, p, e)
	case http.MethodPatch:
		p, e := s.store.GetModelProfile(r.Context(), id)
		if e != nil {
			respond(w, nil, e)
			return
		}
		var in modelInput
		if e = decodeJSON(w, r, &in); e != nil {
			writeError(w, 400, "invalid_request", e.Error())
			return
		}
		if e = s.applyModelInput(&p, in, false); e != nil {
			writeError(w, 400, "invalid_request", e.Error())
			return
		}
		p, e = s.store.SaveModelProfile(r.Context(), p)
		respond(w, p, e)
	case http.MethodDelete:
		respondNoContent(w, s.store.DeleteModelProfile(r.Context(), id))
	default:
		methodNotAllowed(w, "GET, PATCH, DELETE")
	}
}
func (s *Server) applyModelInput(p *domain.ModelProfile, in modelInput, creating bool) error {
	if in.Name != nil {
		p.Name = strings.TrimSpace(*in.Name)
	}
	if in.Provider != nil {
		p.Provider = strings.ToLower(strings.TrimSpace(*in.Provider))
	}
	if p.Provider == "anthropic" {
		p.Provider = "claude"
	}
	if in.APIURL != nil {
		p.APIURL = strings.TrimSpace(*in.APIURL)
	}
	if in.Model != nil {
		p.Model = strings.TrimSpace(*in.Model)
	}
	if in.TimeoutSeconds != nil {
		p.TimeoutSeconds = *in.TimeoutSeconds
	}
	if in.MaxOutputTokens != nil {
		p.MaxOutputTokens = *in.MaxOutputTokens
	}
	if in.ContextWindow != nil {
		p.ContextWindow = *in.ContextWindow
	}
	if in.IsDefault != nil {
		p.IsDefault = *in.IsDefault
	}
	if in.ClearAPIKey {
		p.APIKeyEncrypted = nil
	} else if in.APIKey != nil && *in.APIKey != "" {
		encrypted, err := security.Encrypt(s.masterKey, []byte(*in.APIKey))
		if err != nil {
			return err
		}
		p.APIKeyEncrypted = encrypted
	}
	if p.Name == "" || p.APIURL == "" || p.Model == "" {
		return errors.New("name, api_url and model are required")
	}
	if len([]rune(p.Name)) > 120 || len([]rune(p.Model)) > 300 {
		return errors.New("name or model is too long")
	}
	parsed, err := url.Parse(p.APIURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("api_url must be an absolute HTTP(S) URL without query or fragment")
	}
	switch p.Provider {
	case "openai", "openai_compatible", "deepseek", "claude", "mimo":
	default:
		return errors.New("unsupported provider")
	}
	endpoint := strings.TrimRight(parsed.Path, "/")
	chat, responses, messages := strings.HasSuffix(endpoint, "/chat/completions"), strings.HasSuffix(endpoint, "/responses"), strings.HasSuffix(endpoint, "/messages")
	validEndpoint := false
	switch p.Provider {
	case "openai", "openai_compatible":
		validEndpoint = chat || responses
	case "deepseek":
		validEndpoint = chat || messages
	case "claude":
		validEndpoint = messages
	case "mimo":
		validEndpoint = chat || responses || messages
	}
	if !validEndpoint {
		return errors.New("api_url must contain a complete provider endpoint")
	}
	if p.TimeoutSeconds == 0 {
		p.TimeoutSeconds = 60
	}
	if p.MaxOutputTokens == 0 {
		p.MaxOutputTokens = 4096
	}
	if p.ContextWindow == 0 {
		p.ContextWindow = 32768
	}
	if p.TimeoutSeconds < 1 || p.TimeoutSeconds > 600 || p.MaxOutputTokens < 1 || p.ContextWindow < 1024 {
		return errors.New("invalid timeout or token limits")
	}
	_ = creating
	return nil
}

type characterInput struct {
	Name                  *string `json:"name"`
	Bio                   *string `json:"bio"`
	Personality           *string `json:"personality"`
	Scenario              *string `json:"scenario"`
	Greeting              *string `json:"greeting"`
	SystemRules           *string `json:"system_rules"`
	ExampleDialogue       *string `json:"example_dialogue"`
	EnableTime            *bool   `json:"enable_time"`
	EnableCalculator      *bool   `json:"enable_calculator"`
	DefaultModelProfileID *string `json:"default_model_profile_id"`
}

func (s *Server) characters(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, e := s.store.ListCharacters(r.Context())
		respond(w, items, e)
	case http.MethodPost:
		var in characterInput
		if e := decodeJSON(w, r, &in); e != nil {
			writeError(w, 400, "invalid_request", e.Error())
			return
		}
		c := domain.Character{}
		if e := applyCharacter(&c, in); e != nil {
			writeError(w, 400, "invalid_request", e.Error())
			return
		}
		saved, e := s.store.SaveCharacter(r.Context(), c)
		if e != nil {
			respond(w, nil, e)
			return
		}
		writeJSON(w, 201, saved)
	default:
		methodNotAllowed(w, "GET, POST")
	}
}
func (s *Server) character(w http.ResponseWriter, r *http.Request, parts []string) {
	id := parts[0]
	if len(parts) == 2 && parts[1] == "avatar" {
		s.avatar(w, r, id)
		return
	}
	if len(parts) != 1 {
		writeError(w, 404, "not_found", "接口不存在")
		return
	}
	switch r.Method {
	case http.MethodGet:
		c, e := s.store.GetCharacter(r.Context(), id)
		respond(w, c, e)
	case http.MethodPatch:
		c, e := s.store.GetCharacter(r.Context(), id)
		if e != nil {
			respond(w, nil, e)
			return
		}
		var in characterInput
		if e = decodeJSON(w, r, &in); e != nil {
			writeError(w, 400, "invalid_request", e.Error())
			return
		}
		if e = applyCharacter(&c, in); e != nil {
			writeError(w, 400, "invalid_request", e.Error())
			return
		}
		c, e = s.store.SaveCharacter(r.Context(), c)
		respond(w, c, e)
	case http.MethodDelete:
		respondNoContent(w, s.store.DeleteCharacter(r.Context(), id))
	default:
		methodNotAllowed(w, "GET, PATCH, DELETE")
	}
}
func applyCharacter(c *domain.Character, in characterInput) error {
	if in.Name != nil {
		c.Name = strings.TrimSpace(*in.Name)
	}
	if in.Bio != nil {
		c.Bio = strings.TrimSpace(*in.Bio)
	}
	if in.Personality != nil {
		c.Personality = strings.TrimSpace(*in.Personality)
	}
	if in.Scenario != nil {
		c.Scenario = strings.TrimSpace(*in.Scenario)
	}
	if in.Greeting != nil {
		c.Greeting = strings.TrimSpace(*in.Greeting)
	}
	if in.SystemRules != nil {
		c.SystemRules = strings.TrimSpace(*in.SystemRules)
	}
	if in.ExampleDialogue != nil {
		c.ExampleDialogue = strings.TrimSpace(*in.ExampleDialogue)
	}
	if in.EnableTime != nil {
		c.EnableTime = *in.EnableTime
	}
	if in.EnableCalculator != nil {
		c.EnableCalculator = *in.EnableCalculator
	}
	if in.DefaultModelProfileID != nil {
		v := strings.TrimSpace(*in.DefaultModelProfileID)
		if v == "" {
			c.DefaultModelProfileID = nil
		} else {
			c.DefaultModelProfileID = &v
		}
	}
	if c.Name == "" {
		return errors.New("name is required")
	}
	if len([]rune(c.Name)) > 120 || len([]rune(c.Bio))+len([]rune(c.Personality))+len([]rune(c.Scenario))+len([]rune(c.Greeting))+len([]rune(c.SystemRules))+len([]rune(c.ExampleDialogue)) > 200000 {
		return errors.New("character fields are too long")
	}
	return nil
}
func (s *Server) avatar(w http.ResponseWriter, r *http.Request, id string) {
	switch r.Method {
	case http.MethodGet:
		mime, data, e := s.store.GetAvatar(r.Context(), id)
		if e != nil {
			respond(w, nil, e)
			return
		}
		w.Header().Set("Content-Type", mime)
		w.Header().Set("Cache-Control", "private, max-age=3600")
		_, _ = w.Write(data)
	case http.MethodPut:
		mime := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
		if mime != "image/png" && mime != "image/jpeg" && mime != "image/webp" {
			writeError(w, 415, "unsupported_media_type", "仅支持 PNG、JPEG 和 WebP")
			return
		}
		data, e := io.ReadAll(io.LimitReader(r.Body, (2<<20)+1))
		if e != nil || len(data) > 2<<20 {
			writeError(w, 413, "avatar_too_large", "头像不能超过 2 MiB")
			return
		}
		detected := strings.ToLower(strings.Split(http.DetectContentType(data), ";")[0])
		if detected != mime {
			writeError(w, http.StatusUnsupportedMediaType, "invalid_avatar", "头像内容与声明的图片格式不一致")
			return
		}
		respondNoContent(w, s.store.SetAvatar(r.Context(), id, mime, data))
	case http.MethodDelete:
		respondNoContent(w, s.store.SetAvatar(r.Context(), id, "", nil))
	default:
		methodNotAllowed(w, "GET, PUT, DELETE")
	}
}

func (s *Server) conversations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, e := s.store.ListConversations(r.Context())
		respond(w, items, e)
	case http.MethodPost:
		var in struct {
			Title          string  `json:"title"`
			CharacterID    string  `json:"character_id"`
			ModelProfileID *string `json:"model_profile_id"`
		}
		if e := decodeJSON(w, r, &in); e != nil {
			writeError(w, 400, "invalid_request", e.Error())
			return
		}
		c, e := s.service.CreateConversation(r.Context(), in.Title, in.CharacterID, in.ModelProfileID)
		if e != nil {
			respond(w, nil, e)
			return
		}
		writeJSON(w, 201, c)
	default:
		methodNotAllowed(w, "GET, POST")
	}
}
func (s *Server) conversation(w http.ResponseWriter, r *http.Request, parts []string) {
	id := parts[0]
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			c, e := s.store.GetConversation(r.Context(), id)
			respond(w, c, e)
		case http.MethodDelete:
			if s.service.IsGenerating(id) {
				writeError(w, http.StatusConflict, "generating", "该会话正在生成回复")
				return
			}
			respondNoContent(w, s.store.DeleteConversation(r.Context(), id))
		default:
			methodNotAllowed(w, "GET, DELETE")
		}
		return
	}
	if len(parts) == 2 && parts[1] == "messages" {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		items, e := s.store.ListMessages(r.Context(), id, before, limit)
		respond(w, items, e)
		return
	}
	if len(parts) == 3 && parts[1] == "messages" && parts[2] == "stream" {
		s.streamMessage(w, r, id)
		return
	}
	if len(parts) == 3 && parts[1] == "messages" {
		s.mutateMessage(w, r, id, parts[2])
		return
	}
	if len(parts) == 2 && parts[1] == "regenerate" {
		s.regenerate(w, r, id)
		return
	}
	if len(parts) == 2 && parts[1] == "stop" {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		writeJSON(w, 200, map[string]bool{"stopped": s.service.Stop(id)})
		return
	}
	writeError(w, 404, "not_found", "接口不存在")
}
func (s *Server) mutateMessage(w http.ResponseWriter, r *http.Request, conversationID, messageID string) {
	if s.service.IsGenerating(conversationID) {
		writeError(w, http.StatusConflict, "generating", "该会话正在生成回复")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var in struct {
			Content string `json:"content"`
		}
		if e := decodeJSON(w, r, &in); e != nil {
			writeError(w, 400, "invalid_request", e.Error())
			return
		}
		in.Content = strings.TrimSpace(in.Content)
		if in.Content == "" {
			writeError(w, 400, "invalid_request", "消息不能为空")
			return
		}
		e := s.store.EditLatestUser(r.Context(), conversationID, messageID, in.Content)
		if e != nil {
			respond(w, nil, e)
			return
		}
		m, e := s.store.GetMessage(r.Context(), messageID)
		respond(w, m, e)
	case http.MethodDelete:
		respondNoContent(w, s.store.DeleteFromMessage(r.Context(), conversationID, messageID))
	default:
		methodNotAllowed(w, "PATCH, DELETE")
	}
}
func (s *Server) streamMessage(w http.ResponseWriter, r *http.Request, id string) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !s.reserveGeneration(id) {
		writeError(w, 409, "generating", "该会话正在生成回复")
		return
	}
	defer s.releaseGeneration(id)
	if !s.limiter.allow("chat:"+remoteIP(r), 30, time.Minute, true) {
		writeError(w, 429, "rate_limited", "请求过于频繁")
		return
	}
	var in struct {
		ClientMessageID string `json:"client_message_id"`
		Content         string `json:"content"`
	}
	if e := decodeJSON(w, r, &in); e != nil {
		writeError(w, 400, "invalid_request", e.Error())
		return
	}
	in.Content = strings.TrimSpace(in.Content)
	in.ClientMessageID = strings.TrimSpace(in.ClientMessageID)
	if in.ClientMessageID == "" || len(in.ClientMessageID) > 200 {
		writeError(w, http.StatusBadRequest, "invalid_request", "client_message_id 不能为空")
		return
	}
	if in.Content == "" {
		writeError(w, http.StatusBadRequest, "empty_message", "消息不能为空")
		return
	}
	if len([]rune(in.Content)) > 32000 {
		writeError(w, http.StatusRequestEntityTooLarge, "message_too_long", "消息不能超过 32000 个字符")
		return
	}
	s.serveSSE(w, func(sink app.EventSink) error {
		return s.service.Send(r.Context(), id, in.ClientMessageID, in.Content, sink)
	})
}
func (s *Server) regenerate(w http.ResponseWriter, r *http.Request, id string) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !s.reserveGeneration(id) {
		writeError(w, 409, "generating", "该会话正在生成回复")
		return
	}
	defer s.releaseGeneration(id)
	if !s.limiter.allow("chat:"+remoteIP(r), 30, time.Minute, true) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "请求过于频繁")
		return
	}
	s.serveSSE(w, func(sink app.EventSink) error { return s.service.Regenerate(r.Context(), id, sink) })
}

func (s *Server) reserveGeneration(id string) bool {
	s.generationMu.Lock()
	defer s.generationMu.Unlock()
	if s.generations[id] {
		return false
	}
	s.generations[id] = true
	return true
}
func (s *Server) releaseGeneration(id string) {
	s.generationMu.Lock()
	delete(s.generations, id)
	s.generationMu.Unlock()
}
func (s *Server) serveSSE(w http.ResponseWriter, run func(app.EventSink) error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "streaming_unavailable", "服务器不支持流式响应")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	flusher.Flush()
	sink := func(event app.StreamEvent) error {
		payload, e := json.Marshal(event.Data)
		if e != nil {
			return e
		}
		if _, e = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, payload); e != nil {
			return e
		}
		flusher.Flush()
		return nil
	}
	if e := run(sink); e != nil && !errors.Is(e, context.Canceled) {
		s.logf("conversation generation failed: %v", e)
		_ = sink(app.StreamEvent{Type: "error", Data: map[string]string{"message": "生成失败（" + classifyModelError(e) + "），请检查模型档案后重试"}})
	}
}

func (s *Server) securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			origin := strings.TrimRight(r.Header.Get("Origin"), "/")
			if origin != "" && origin != requestOrigin(r) {
				writeError(w, 403, "origin_rejected", "请求来源不受信任")
				return
			}
			if !(strings.Contains(r.URL.Path, "/avatar") && r.Method == http.MethodPut) && !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
				writeError(w, 415, "json_required", "请求必须使用 application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	}
	return scheme + "://" + r.Host
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if e := dec.Decode(target); e != nil {
		return fmt.Errorf("请求 JSON 无效: %w", e)
	}
	if e := dec.Decode(&struct{}{}); !errors.Is(e, io.EOF) {
		return errors.New("请求只能包含一个 JSON 对象")
	}
	return nil
}
func respond(w http.ResponseWriter, value any, err error) {
	if err == nil {
		writeJSON(w, 200, value)
		return
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, 404, "not_found", "资源不存在")
	case errors.Is(err, store.ErrConflict), errors.Is(err, app.ErrGenerating):
		writeError(w, 409, "conflict", "资源当前无法执行该操作")
	default:
		writeError(w, 500, "internal_error", "服务器内部错误")
	}
}
func respondNoContent(w http.ResponseWriter, err error) {
	if err != nil {
		respond(w, nil, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	methodNotAllowed(w, method)
	return false
}
func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeError(w, 405, "method_not_allowed", "请求方法不支持")
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
func remoteIP(r *http.Request) string {
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e == nil {
		return host
	}
	return r.RemoteAddr
}
func classifyModelError(err error) string {
	v := strings.ToLower(err.Error())
	switch {
	case strings.Contains(v, "401") || strings.Contains(v, "403"):
		return "authentication"
	case strings.Contains(v, "timeout") || errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case strings.Contains(v, "decode") || strings.Contains(v, "protocol"):
		return "protocol"
	default:
		return "connection"
	}
}

type rateEntry struct{ times []time.Time }
type rateLimiter struct {
	mu      sync.Mutex
	entries map[string]*rateEntry
}

func newRateLimiter() *rateLimiter { return &rateLimiter{entries: map[string]*rateEntry{}} }
func (l *rateLimiter) allow(key string, limit int, window time.Duration, record bool) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	e := l.entries[key]
	if e == nil {
		e = &rateEntry{}
		l.entries[key] = e
	}
	cut := 0
	for cut < len(e.times) && now.Sub(e.times[cut]) >= window {
		cut++
	}
	e.times = append([]time.Time(nil), e.times[cut:]...)
	if len(e.times) >= limit {
		return false
	}
	if record {
		e.times = append(e.times, now)
	}
	return true
}
func (l *rateLimiter) record(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[key]
	if e == nil {
		e = &rateEntry{}
		l.entries[key] = e
	}
	e.times = append(e.times, time.Now())
}
func (l *rateLimiter) clear(key string) { l.mu.Lock(); delete(l.entries, key); l.mu.Unlock() }
