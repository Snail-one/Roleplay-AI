package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"roleloom/internal/agent"
	"roleloom/internal/ai"
	"roleloom/internal/ai/provider"
	"roleloom/internal/domain"
	"roleloom/internal/security"
	"roleloom/internal/store"
)

const BaseRule = "你正在进行沉浸式文字角色扮演。始终以角色身份回应；不要声称看到了未提供的现实信息；尊重用户边界；用自然、连贯的文本作答。"

var ErrGenerating = errors.New("conversation is already generating")

type StreamEvent struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}
type EventSink func(StreamEvent) error
type ModelFactory func(domain.ModelProfile, string) (*ai.Client, error)

type ConversationService struct {
	store        *store.Store
	masterKey    []byte
	modelFactory ModelFactory
	mu           sync.Mutex
	active       map[string]context.CancelFunc
}

func NewConversationService(st *store.Store, masterKey []byte) *ConversationService {
	return &ConversationService{store: st, masterKey: masterKey, active: map[string]context.CancelFunc{}, modelFactory: defaultModelFactory}
}
func (s *ConversationService) SetModelFactory(factory ModelFactory) {
	if factory != nil {
		s.modelFactory = factory
	}
}
func defaultModelFactory(p domain.ModelProfile, key string) (*ai.Client, error) {
	return provider.New(provider.Config{Provider: p.Provider, APIURL: p.APIURL, APIKey: key, Model: p.Model, MaxTokens: p.MaxOutputTokens, Timeout: time.Duration(p.TimeoutSeconds) * time.Second})
}

func (s *ConversationService) CreateConversation(ctx context.Context, title, characterID string, modelID *string) (domain.Conversation, error) {
	c, err := s.store.GetCharacter(ctx, characterID)
	if err != nil {
		return domain.Conversation{}, err
	}
	selected := ""
	if modelID != nil {
		selected = strings.TrimSpace(*modelID)
	} else if c.DefaultModelProfileID != nil {
		selected = *c.DefaultModelProfileID
	} else {
		p, e := s.store.DefaultModelProfile(ctx)
		if e != nil {
			return domain.Conversation{}, fmt.Errorf("default model profile: %w", e)
		}
		selected = p.ID
	}
	if _, err = s.store.GetModelProfile(ctx, selected); err != nil {
		return domain.Conversation{}, err
	}
	if strings.TrimSpace(title) == "" {
		title = c.Name
	}
	return s.store.CreateConversation(ctx, domain.Conversation{Title: strings.TrimSpace(title), CharacterID: c.ID, CharacterSnapshot: c.Snapshot(), ModelProfileID: selected}, c.Greeting)
}

func (s *ConversationService) TestModel(ctx context.Context, p domain.ModelProfile) (string, error) {
	key, err := s.decryptKey(p)
	if err != nil {
		return "", err
	}
	client, err := s.modelFactory(p, key)
	if err != nil {
		return "", err
	}
	prompt := "只回复 OK"
	reply, err := client.Complete(ctx, []ai.Message{{Role: ai.RoleUser, Content: &prompt}}, nil)
	if err != nil {
		return "", err
	}
	if reply.Content == nil {
		return "连接成功（模型未返回文本）", nil
	}
	return "连接成功", nil
}

func (s *ConversationService) Send(ctx context.Context, conversationID, clientID, content string, sink EventSink) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return errors.New("message is empty")
	}
	cancel, err := s.begin(conversationID, ctx)
	if err != nil {
		return err
	}
	defer s.end(conversationID)
	ctx = cancel.ctx
	user, draft, duplicate, err := s.store.AppendUserAndDraft(ctx, conversationID, clientID, content)
	if err != nil {
		return err
	}
	if err = emit(sink, "user_message", user); err != nil {
		return err
	}
	if duplicate {
		if draft.ID != "" && draft.Status == domain.MessageCompleted {
			return emit(sink, "assistant_done", draft)
		}
		return ErrGenerating
	}
	if err = emit(sink, "assistant_start", draft); err != nil {
		return err
	}
	return s.generate(ctx, conversationID, draft, sink)
}

func (s *ConversationService) Regenerate(ctx context.Context, conversationID string, sink EventSink) error {
	state, err := s.begin(conversationID, ctx)
	if err != nil {
		return err
	}
	defer s.end(conversationID)
	ctx = state.ctx
	messages, err := s.store.ListMessages(ctx, conversationID, 0, 200)
	if err != nil {
		return err
	}
	if len(messages) == 0 {
		return errors.New("conversation has no messages")
	}
	if messages[len(messages)-1].Role == "assistant" {
		if _, err = s.store.DeleteLastAssistant(ctx, conversationID); err != nil {
			return err
		}
	}
	messages, err = s.store.ListMessages(ctx, conversationID, 0, 200)
	if err != nil || len(messages) == 0 {
		return errors.New("conversation has no user message")
	}
	if messages[len(messages)-1].Role != "user" {
		return errors.New("last message is not a user message")
	}
	draft, err := s.store.AppendDraft(ctx, conversationID)
	if err != nil {
		return err
	}
	if err = emit(sink, "assistant_start", draft); err != nil {
		return err
	}
	return s.generate(ctx, conversationID, draft, sink)
}

type generationContext struct{ ctx context.Context }

func (s *ConversationService) begin(id string, parent context.Context) (generationContext, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.active[id]; ok {
		return generationContext{}, ErrGenerating
	}
	ctx, cancel := context.WithCancel(parent)
	s.active[id] = cancel
	return generationContext{ctx}, nil
}
func (s *ConversationService) end(id string) { s.mu.Lock(); delete(s.active, id); s.mu.Unlock() }
func (s *ConversationService) Stop(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cancel, ok := s.active[id]
	if ok {
		cancel()
	}
	return ok
}
func (s *ConversationService) IsGenerating(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.active[id]
	return ok
}

func (s *ConversationService) generate(ctx context.Context, conversationID string, draft domain.Message, sink EventSink) (err error) {
	status := domain.MessageFailed
	content := ""
	hidden := ""
	var output strings.Builder
	defer func() {
		if ctx.Err() != nil {
			status = domain.MessageCancelled
		}
		if content == "" && output.Len() > 0 {
			content = output.String()
		}
		_ = s.store.UpdateMessage(context.Background(), draft.ID, content, status, hidden)
	}()
	conv, err := s.store.GetConversation(ctx, conversationID)
	if err != nil {
		return err
	}
	profile, err := s.store.GetModelProfile(ctx, conv.ModelProfileID)
	if err != nil {
		return err
	}
	key, err := s.decryptKey(profile)
	if err != nil {
		return err
	}
	client, err := s.modelFactory(profile, key)
	if err != nil {
		return err
	}
	messages, err := s.store.ListMessages(ctx, conversationID, 0, 200)
	if err != nil {
		return err
	}
	messages = withoutDraft(messages, draft.ID)
	if warning := s.maybeSummarize(ctx, &conv, profile, client, messages); warning != "" {
		_ = emit(sink, "warning", map[string]string{"message": warning})
	}
	history := buildHistory(conv, messages, profile.ContextWindow)
	tools, definitions := enabledTools(conv.CharacterSnapshot)
	var traces []any
	for iteration := 0; iteration < 8; iteration++ {
		response, callErr := client.Stream(ctx, history, definitions, func(event ai.StreamEvent) error {
			if event.Delta != "" {
				output.WriteString(event.Delta)
				return emit(sink, "assistant_delta", map[string]string{"delta": event.Delta})
			}
			return nil
		})
		if callErr != nil {
			return callErr
		}
		if response.Role == "" {
			response.Role = ai.RoleAssistant
		}
		if len(response.ToolCalls) == 0 {
			if response.Content == nil || strings.TrimSpace(*response.Content) == "" {
				return errors.New("model returned an empty response")
			}
			content = output.String()
			if content == "" {
				content = *response.Content
			}
			status = domain.MessageCompleted
			if len(traces) > 0 {
				data, _ := json.Marshal(traces)
				hidden = string(data)
			}
			done := draft
			done.Content = content
			done.Status = status
			if err := emit(sink, "assistant_done", done); err != nil {
				return err
			}
			return nil
		}
		history = append(history, response)
		for _, call := range response.ToolCalls {
			_ = emit(sink, "tool_status", map[string]string{"name": call.Function.Name, "status": "running"})
			result := executeTool(ctx, tools, call)
			traces = append(traces, map[string]any{"call": call, "result": result})
			history = append(history, ai.Message{Role: ai.RoleTool, Content: &result, ToolCallID: call.ID, Name: call.Function.Name})
			_ = emit(sink, "tool_status", map[string]string{"name": call.Function.Name, "status": "completed"})
		}
	}
	b, _ := json.Marshal(traces)
	hidden = string(b)
	return errors.New("model exceeded the maximum tool iterations")
}

func (s *ConversationService) decryptKey(p domain.ModelProfile) (string, error) {
	if len(p.APIKeyEncrypted) == 0 {
		return "", nil
	}
	plain, err := security.Decrypt(s.masterKey, p.APIKeyEncrypted)
	if err != nil {
		return "", fmt.Errorf("model profile API key is unavailable: %w", err)
	}
	return string(plain), nil
}

func (s *ConversationService) maybeSummarize(ctx context.Context, conv *domain.Conversation, p domain.ModelProfile, client *ai.Client, messages []domain.Message) string {
	budget := p.ContextWindow
	if budget <= 0 {
		budget = 32768
	}
	estimated := 0
	for _, m := range messages {
		estimated += (len([]rune(m.Content))+3)/4 + 8
	}
	if estimated < budget*70/100 || len(messages) <= 16 {
		return ""
	}
	cut := len(messages) - 16
	for cut > 0 && messages[cut-1].Role == "user" {
		cut--
	}
	if cut <= 0 {
		return ""
	}
	var source strings.Builder
	if conv.MemorySummary != "" {
		source.WriteString("已有摘要：\n" + conv.MemorySummary + "\n\n")
	}
	for _, m := range messages[:cut] {
		if m.Status == domain.MessageCompleted {
			fmt.Fprintf(&source, "%s: %s\n", m.Role, m.Content)
		}
	}
	prompt := "请把以下角色扮演对话压缩为准确的滚动记忆，保留人物关系、承诺、事实、情绪变化和未完成事项，不要续写对话：\n" + source.String()
	system := "你是对话记忆整理器。只输出摘要。"
	result, err := client.Complete(ctx, []ai.Message{{Role: ai.RoleSystem, Content: &system}, {Role: ai.RoleUser, Content: &prompt}}, nil)
	if err != nil || result.Content == nil || strings.TrimSpace(*result.Content) == "" {
		return "较早对话摘要失败，已仅使用预算内的最近消息"
	}
	through := messages[cut-1].Seq
	if err = s.store.UpdateSummary(ctx, conv.ID, *result.Content, through); err != nil {
		return "摘要已生成但保存失败，已仅使用最近消息"
	}
	conv.MemorySummary = *result.Content
	conv.SummaryThroughSeq = through
	return ""
}

func buildHistory(conv domain.Conversation, messages []domain.Message, contextWindow int) []ai.Message {
	parts := []string{BaseRule}
	snap := conv.CharacterSnapshot
	if snap.SystemRules != "" {
		parts = append(parts, snap.SystemRules)
	}
	character := []string{}
	if snap.Name != "" {
		character = append(character, "角色名："+snap.Name)
	}
	if snap.Bio != "" {
		character = append(character, "简介："+snap.Bio)
	}
	if snap.Personality != "" {
		character = append(character, "性格："+snap.Personality)
	}
	if snap.Scenario != "" {
		character = append(character, "场景："+snap.Scenario)
	}
	if snap.ExampleDialogue != "" {
		character = append(character, "示例对话：\n"+snap.ExampleDialogue)
	}
	if len(character) > 0 {
		parts = append(parts, strings.Join(character, "\n"))
	}
	if conv.MemorySummary != "" {
		parts = append(parts, "已保存的对话记忆：\n"+conv.MemorySummary)
	}
	system := strings.Join(parts, "\n\n")
	history := []ai.Message{{Role: ai.RoleSystem, Content: &system}}
	visible := make([]domain.Message, 0, len(messages))
	for _, m := range messages {
		if m.Seq <= conv.SummaryThroughSeq || m.Status != domain.MessageCompleted || m.Content == "" || (m.Role != "user" && m.Role != "assistant") {
			continue
		}
		visible = append(visible, m)
	}
	if contextWindow <= 0 {
		contextWindow = 32768
	}
	budget, used, start := contextWindow*70/100, (len([]rune(system))+3)/4, len(visible)
	for start > 0 {
		cost := (len([]rune(visible[start-1].Content))+3)/4 + 8
		if len(visible)-start >= 16 && used+cost > budget {
			break
		}
		used += cost
		start--
	}
	for _, m := range visible[start:] {
		if m.Status != domain.MessageCompleted || m.Content == "" {
			continue
		}
		role := m.Role
		if role != "user" && role != "assistant" {
			continue
		}
		content := m.Content
		history = append(history, ai.Message{Role: role, Content: &content})
	}
	return history
}
func enabledTools(snapshot domain.CharacterSnapshot) (map[string]agent.Tool, []ai.ToolDefinition) {
	tools := map[string]agent.Tool{}
	if snapshot.EnableTime {
		tools["get_current_time"] = agent.TimeTool{}
	}
	if snapshot.EnableCalculator {
		tools["calculate"] = agent.CalculatorTool{}
	}
	defs := make([]ai.ToolDefinition, 0, len(tools))
	for _, name := range []string{"get_current_time", "calculate"} {
		if t := tools[name]; t != nil {
			defs = append(defs, t.Definition())
		}
	}
	return tools, defs
}
func executeTool(ctx context.Context, tools map[string]agent.Tool, call ai.ToolCall) string {
	tool := tools[call.Function.Name]
	if tool == nil {
		return `{"error":"tool is not enabled"}`
	}
	result, err := tool.Execute(ctx, json.RawMessage(call.Function.Arguments))
	if err != nil {
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		return string(b)
	}
	return result
}
func withoutDraft(messages []domain.Message, id string) []domain.Message {
	out := messages[:0]
	for _, m := range messages {
		if m.ID != id {
			out = append(out, m)
		}
	}
	return out
}
func emit(sink EventSink, eventType string, data any) error {
	if sink == nil {
		return nil
	}
	return sink(StreamEvent{Type: eventType, Data: data})
}
