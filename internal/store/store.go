package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"roleloom/internal/domain"
	"roleloom/internal/security"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("database path is required")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	dsn := "file:" + url.PathEscape(path) + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	if path == ":memory:" {
		id, _ := security.NewID()
		dsn = "file:roleloom-memory-" + id + "?mode=memory&cache=shared&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) DB() *sql.DB  { return s.db }

func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value BLOB NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS model_profiles (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, provider TEXT NOT NULL, api_url TEXT NOT NULL,
			api_key_encrypted BLOB, model TEXT NOT NULL, timeout_seconds INTEGER NOT NULL DEFAULT 60,
			max_output_tokens INTEGER NOT NULL DEFAULT 4096, context_window INTEGER NOT NULL DEFAULT 32768,
			is_default INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS one_default_model ON model_profiles(is_default) WHERE is_default=1`,
		`CREATE TABLE IF NOT EXISTS characters (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, avatar BLOB, avatar_mime TEXT NOT NULL DEFAULT '', bio TEXT NOT NULL DEFAULT '',
			personality TEXT NOT NULL DEFAULT '', scenario TEXT NOT NULL DEFAULT '', greeting TEXT NOT NULL DEFAULT '',
			system_rules TEXT NOT NULL DEFAULT '', example_dialogue TEXT NOT NULL DEFAULT '', enable_time INTEGER NOT NULL DEFAULT 0,
			enable_calculator INTEGER NOT NULL DEFAULT 0, default_model_profile_id TEXT REFERENCES model_profiles(id) ON DELETE SET NULL,
			created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS conversations (
			id TEXT PRIMARY KEY, title TEXT NOT NULL, character_id TEXT NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
			character_snapshot TEXT NOT NULL, model_profile_id TEXT NOT NULL REFERENCES model_profiles(id) ON DELETE RESTRICT,
			memory_summary TEXT NOT NULL DEFAULT '', summary_through_seq INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS messages (
			id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
			seq INTEGER NOT NULL, role TEXT NOT NULL CHECK(role IN ('user','assistant','tool')),
			content TEXT NOT NULL DEFAULT '', status TEXT NOT NULL CHECK(status IN ('generating','completed','failed','cancelled')),
			hidden_tool_info TEXT NOT NULL DEFAULT '', client_message_id TEXT, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
			UNIQUE(conversation_id,seq), UNIQUE(conversation_id,client_message_id))`,
		`CREATE INDEX IF NOT EXISTS messages_conversation_seq ON messages(conversation_id,seq)`,
		`CREATE TABLE IF NOT EXISTS admin_sessions (
			token_hash BLOB PRIMARY KEY, expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL)`,
		`INSERT INTO settings(key,value) VALUES('database_version','1') ON CONFLICT(key) DO UPDATE SET value='1'`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("database migration: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) SyncAdminPassword(ctx context.Context, password string) (bool, error) {
	var current string
	err := s.db.QueryRowContext(ctx, `SELECT CAST(value AS TEXT) FROM settings WHERE key='admin_password_hash'`).Scan(&current)
	if err == nil && security.VerifyPassword(current, password) {
		return false, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	hash, err := security.HashPassword(password)
	if err != nil {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('admin_password_hash',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, hash); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM admin_sessions`); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s *Store) AdminPasswordInitialized(ctx context.Context) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM settings WHERE key='admin_password_hash'`).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) VerifyAdminPassword(ctx context.Context, password string) (bool, error) {
	var encoded string
	err := s.db.QueryRowContext(ctx, `SELECT CAST(value AS TEXT) FROM settings WHERE key='admin_password_hash'`).Scan(&encoded)
	if err != nil {
		return false, err
	}
	return security.VerifyPassword(encoded, password), nil
}

func (s *Store) CreateSession(ctx context.Context, hash []byte, expires time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO admin_sessions(token_hash,expires_at,created_at) VALUES(?,?,?)`, hash, millis(expires), millis(time.Now()))
	return err
}
func (s *Store) SessionValid(ctx context.Context, hash []byte, now time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE expires_at<=?`, millis(now))
	_ = result
	if err != nil {
		return false, err
	}
	var one int
	err = s.db.QueryRowContext(ctx, `SELECT 1 FROM admin_sessions WHERE token_hash=? AND expires_at>?`, hash, millis(now)).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
func (s *Store) DeleteSession(ctx context.Context, hash []byte) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE token_hash=?`, hash)
	return err
}

func (s *Store) ListModelProfiles(ctx context.Context) ([]domain.ModelProfile, error) {
	rows, err := s.db.QueryContext(ctx, modelSelect+` ORDER BY is_default DESC, created_at ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.ModelProfile, 0)
	for rows.Next() {
		p, err := scanModel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) GetModelProfile(ctx context.Context, id string) (domain.ModelProfile, error) {
	p, err := scanModel(s.db.QueryRowContext(ctx, modelSelect+` WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return p, err
}

const modelSelect = `SELECT id,name,provider,api_url,api_key_encrypted,model,timeout_seconds,max_output_tokens,context_window,is_default,created_at,updated_at FROM model_profiles`

type scanner interface{ Scan(...any) error }

func scanModel(row scanner) (domain.ModelProfile, error) {
	var p domain.ModelProfile
	var key []byte
	var def int
	var created, updated int64
	err := row.Scan(&p.ID, &p.Name, &p.Provider, &p.APIURL, &key, &p.Model, &p.TimeoutSeconds, &p.MaxOutputTokens, &p.ContextWindow, &def, &created, &updated)
	p.APIKeyEncrypted = key
	p.HasAPIKey = len(key) > 0
	p.IsDefault = def != 0
	p.CreatedAt = fromMillis(created)
	p.UpdatedAt = fromMillis(updated)
	return p, err
}

func (s *Store) SaveModelProfile(ctx context.Context, p domain.ModelProfile) (domain.ModelProfile, error) {
	now := time.Now()
	if p.ID == "" {
		var err error
		p.ID, err = security.NewID()
		if err != nil {
			return p, err
		}
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	if p.TimeoutSeconds == 0 {
		p.TimeoutSeconds = 60
	}
	if p.MaxOutputTokens == 0 {
		p.MaxOutputTokens = 4096
	}
	if p.ContextWindow == 0 {
		p.ContextWindow = 32768
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	if p.IsDefault {
		if _, err = tx.ExecContext(ctx, `UPDATE model_profiles SET is_default=0`); err != nil {
			return p, err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO model_profiles(id,name,provider,api_url,api_key_encrypted,model,timeout_seconds,max_output_tokens,context_window,is_default,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,provider=excluded.provider,api_url=excluded.api_url,api_key_encrypted=excluded.api_key_encrypted,model=excluded.model,timeout_seconds=excluded.timeout_seconds,max_output_tokens=excluded.max_output_tokens,context_window=excluded.context_window,is_default=excluded.is_default,updated_at=excluded.updated_at`, p.ID, p.Name, p.Provider, p.APIURL, nullBytes(p.APIKeyEncrypted), p.Model, p.TimeoutSeconds, p.MaxOutputTokens, p.ContextWindow, p.IsDefault, millis(p.CreatedAt), millis(p.UpdatedAt))
	if err != nil {
		return p, err
	}
	if err = tx.Commit(); err != nil {
		return p, err
	}
	return s.GetModelProfile(ctx, p.ID)
}
func (s *Store) DeleteModelProfile(ctx context.Context, id string) error {
	r, err := s.db.ExecContext(ctx, `DELETE FROM model_profiles WHERE id=?`, id)
	if err != nil {
		if strings.Contains(err.Error(), "FOREIGN KEY") {
			return ErrConflict
		}
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) DefaultModelProfile(ctx context.Context) (domain.ModelProfile, error) {
	p, err := scanModel(s.db.QueryRowContext(ctx, modelSelect+` WHERE is_default=1 ORDER BY created_at LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return p, err
}

const characterSelect = `SELECT id,name,avatar_mime,COALESCE(length(avatar),0),bio,personality,scenario,greeting,system_rules,example_dialogue,enable_time,enable_calculator,default_model_profile_id,created_at,updated_at FROM characters`

func scanCharacter(row scanner) (domain.Character, error) {
	var c domain.Character
	var avatarSize int
	var t, calc int
	var model sql.NullString
	var created, updated int64
	err := row.Scan(&c.ID, &c.Name, &c.AvatarMIME, &avatarSize, &c.Bio, &c.Personality, &c.Scenario, &c.Greeting, &c.SystemRules, &c.ExampleDialogue, &t, &calc, &model, &created, &updated)
	c.HasAvatar = avatarSize > 0
	c.EnableTime = t != 0
	c.EnableCalculator = calc != 0
	if model.Valid {
		c.DefaultModelProfileID = &model.String
	}
	c.CreatedAt = fromMillis(created)
	c.UpdatedAt = fromMillis(updated)
	return c, err
}
func (s *Store) ListCharacters(ctx context.Context) ([]domain.Character, error) {
	rows, err := s.db.QueryContext(ctx, characterSelect+` ORDER BY created_at ASC,id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Character, 0)
	for rows.Next() {
		c, e := scanCharacter(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) GetCharacter(ctx context.Context, id string) (domain.Character, error) {
	c, err := scanCharacter(s.db.QueryRowContext(ctx, characterSelect+` WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return c, err
}
func (s *Store) SaveCharacter(ctx context.Context, c domain.Character) (domain.Character, error) {
	now := time.Now()
	if c.ID == "" {
		var err error
		c.ID, err = security.NewID()
		if err != nil {
			return c, err
		}
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO characters(id,name,bio,personality,scenario,greeting,system_rules,example_dialogue,enable_time,enable_calculator,default_model_profile_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,bio=excluded.bio,personality=excluded.personality,scenario=excluded.scenario,greeting=excluded.greeting,system_rules=excluded.system_rules,example_dialogue=excluded.example_dialogue,enable_time=excluded.enable_time,enable_calculator=excluded.enable_calculator,default_model_profile_id=excluded.default_model_profile_id,updated_at=excluded.updated_at`, c.ID, c.Name, c.Bio, c.Personality, c.Scenario, c.Greeting, c.SystemRules, c.ExampleDialogue, c.EnableTime, c.EnableCalculator, c.DefaultModelProfileID, millis(c.CreatedAt), millis(c.UpdatedAt))
	if err != nil {
		return c, err
	}
	return s.GetCharacter(ctx, c.ID)
}
func (s *Store) DeleteCharacter(ctx context.Context, id string) error {
	r, e := s.db.ExecContext(ctx, `DELETE FROM characters WHERE id=?`, id)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) SetAvatar(ctx context.Context, id, mime string, data []byte) error {
	r, e := s.db.ExecContext(ctx, `UPDATE characters SET avatar=?,avatar_mime=?,updated_at=? WHERE id=?`, data, mime, millis(time.Now()), id)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) GetAvatar(ctx context.Context, id string) (string, []byte, error) {
	var mime string
	var data []byte
	e := s.db.QueryRowContext(ctx, `SELECT avatar_mime,avatar FROM characters WHERE id=?`, id).Scan(&mime, &data)
	if errors.Is(e, sql.ErrNoRows) || e == nil && len(data) == 0 {
		return "", nil, ErrNotFound
	}
	return mime, data, e
}

const conversationSelect = `SELECT id,title,character_id,character_snapshot,model_profile_id,memory_summary,summary_through_seq,created_at,updated_at FROM conversations`

func scanConversation(row scanner) (domain.Conversation, error) {
	var c domain.Conversation
	var snapshot string
	var created, updated int64
	err := row.Scan(&c.ID, &c.Title, &c.CharacterID, &snapshot, &c.ModelProfileID, &c.MemorySummary, &c.SummaryThroughSeq, &created, &updated)
	if err == nil {
		err = json.Unmarshal([]byte(snapshot), &c.CharacterSnapshot)
	}
	c.CreatedAt = fromMillis(created)
	c.UpdatedAt = fromMillis(updated)
	return c, err
}
func (s *Store) ListConversations(ctx context.Context) ([]domain.Conversation, error) {
	rows, e := s.db.QueryContext(ctx, conversationSelect+` ORDER BY updated_at DESC,id DESC`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := make([]domain.Conversation, 0)
	for rows.Next() {
		c, e := scanConversation(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) GetConversation(ctx context.Context, id string) (domain.Conversation, error) {
	c, e := scanConversation(s.db.QueryRowContext(ctx, conversationSelect+` WHERE id=?`, id))
	if errors.Is(e, sql.ErrNoRows) {
		e = ErrNotFound
	}
	return c, e
}
func (s *Store) CreateConversation(ctx context.Context, c domain.Conversation, greeting string) (domain.Conversation, error) {
	id, e := security.NewID()
	if e != nil {
		return c, e
	}
	c.ID = id
	now := time.Now()
	c.CreatedAt = now
	c.UpdatedAt = now
	snapshot, e := json.Marshal(c.CharacterSnapshot)
	if e != nil {
		return c, e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return c, e
	}
	defer tx.Rollback()
	_, e = tx.ExecContext(ctx, `INSERT INTO conversations(id,title,character_id,character_snapshot,model_profile_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, c.ID, c.Title, c.CharacterID, string(snapshot), c.ModelProfileID, millis(now), millis(now))
	if e != nil {
		return c, e
	}
	if strings.TrimSpace(greeting) != "" {
		mid, _ := security.NewID()
		_, e = tx.ExecContext(ctx, `INSERT INTO messages(id,conversation_id,seq,role,content,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, mid, c.ID, 1, "assistant", greeting, domain.MessageCompleted, millis(now), millis(now))
		if e != nil {
			return c, e
		}
	}
	if e = tx.Commit(); e != nil {
		return c, e
	}
	return s.GetConversation(ctx, c.ID)
}
func (s *Store) DeleteConversation(ctx context.Context, id string) error {
	r, e := s.db.ExecContext(ctx, `DELETE FROM conversations WHERE id=?`, id)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) UpdateSummary(ctx context.Context, id, summary string, through int64) error {
	_, e := s.db.ExecContext(ctx, `UPDATE conversations SET memory_summary=?,summary_through_seq=?,updated_at=? WHERE id=?`, summary, through, millis(time.Now()), id)
	return e
}

const messageSelect = `SELECT id,conversation_id,seq,role,content,status,hidden_tool_info,COALESCE(client_message_id,''),created_at,updated_at FROM messages`

func scanMessage(row scanner) (domain.Message, error) {
	var m domain.Message
	var created, updated int64
	e := row.Scan(&m.ID, &m.ConversationID, &m.Seq, &m.Role, &m.Content, &m.Status, &m.HiddenToolInfo, &m.ClientMessageID, &created, &updated)
	m.CreatedAt = fromMillis(created)
	m.UpdatedAt = fromMillis(updated)
	return m, e
}
func (s *Store) ListMessages(ctx context.Context, conversationID string, before int64, limit int) ([]domain.Message, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	if before <= 0 {
		before = 1 << 62
	}
	rows, e := s.db.QueryContext(ctx, messageSelect+` WHERE conversation_id=? AND seq<? ORDER BY seq DESC LIMIT ?`, conversationID, before, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var reversed []domain.Message
	for rows.Next() {
		m, e := scanMessage(rows)
		if e != nil {
			return nil, e
		}
		reversed = append(reversed, m)
	}
	out := make([]domain.Message, len(reversed))
	for i := range reversed {
		out[len(reversed)-1-i] = reversed[i]
	}
	return out, rows.Err()
}
func (s *Store) GetMessage(ctx context.Context, id string) (domain.Message, error) {
	m, e := scanMessage(s.db.QueryRowContext(ctx, messageSelect+` WHERE id=?`, id))
	if errors.Is(e, sql.ErrNoRows) {
		e = ErrNotFound
	}
	return m, e
}
func (s *Store) AppendUserAndDraft(ctx context.Context, conversationID, clientID, content string) (domain.Message, domain.Message, bool, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return domain.Message{}, domain.Message{}, false, e
	}
	defer tx.Rollback()
	if clientID != "" {
		existing, e := scanMessage(tx.QueryRowContext(ctx, messageSelect+` WHERE conversation_id=? AND client_message_id=?`, conversationID, clientID))
		if e == nil {
			var draft domain.Message
			draft, _ = scanMessage(tx.QueryRowContext(ctx, messageSelect+` WHERE conversation_id=? AND seq=?`, conversationID, existing.Seq+1))
			return existing, draft, true, nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return domain.Message{}, domain.Message{}, false, e
		}
	}
	var seq int64
	if e = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0)+1 FROM messages WHERE conversation_id=?`, conversationID).Scan(&seq); e != nil {
		return domain.Message{}, domain.Message{}, false, e
	}
	now := time.Now()
	uid, _ := security.NewID()
	aid, _ := security.NewID()
	_, e = tx.ExecContext(ctx, `INSERT INTO messages(id,conversation_id,seq,role,content,status,client_message_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, uid, conversationID, seq, "user", content, domain.MessageCompleted, nullString(clientID), millis(now), millis(now))
	if e != nil {
		return domain.Message{}, domain.Message{}, false, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO messages(id,conversation_id,seq,role,content,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, aid, conversationID, seq+1, "assistant", "", domain.MessageGenerating, millis(now), millis(now))
	if e != nil {
		return domain.Message{}, domain.Message{}, false, e
	}
	_, e = tx.ExecContext(ctx, `UPDATE conversations SET updated_at=? WHERE id=?`, millis(now), conversationID)
	if e != nil {
		return domain.Message{}, domain.Message{}, false, e
	}
	if e = tx.Commit(); e != nil {
		return domain.Message{}, domain.Message{}, false, e
	}
	u, _ := s.GetMessage(ctx, uid)
	a, _ := s.GetMessage(ctx, aid)
	return u, a, false, nil
}
func (s *Store) UpdateMessage(ctx context.Context, id, content, status, hidden string) error {
	r, e := s.db.ExecContext(ctx, `UPDATE messages SET content=?,status=?,hidden_tool_info=?,updated_at=? WHERE id=?`, content, status, hidden, millis(time.Now()), id)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) EditLatestUser(ctx context.Context, conversationID, messageID, content string) error {
	return s.truncateFrom(ctx, conversationID, messageID, content, false)
}
func (s *Store) DeleteFromMessage(ctx context.Context, conversationID, messageID string) error {
	return s.truncateFrom(ctx, conversationID, messageID, "", true)
}
func (s *Store) truncateFrom(ctx context.Context, conversationID, messageID, content string, remove bool) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var seq int64
	var role string
	e = tx.QueryRowContext(ctx, `SELECT seq,role FROM messages WHERE id=? AND conversation_id=?`, messageID, conversationID).Scan(&seq, &role)
	if errors.Is(e, sql.ErrNoRows) {
		return ErrNotFound
	}
	if e != nil {
		return e
	}
	if !remove {
		if role != "user" {
			return ErrConflict
		}
		var latest int64
		e = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM messages WHERE conversation_id=? AND role='user'`, conversationID).Scan(&latest)
		if e != nil {
			return e
		}
		if latest != seq {
			return ErrConflict
		}
		_, e = tx.ExecContext(ctx, `UPDATE messages SET content=?,updated_at=? WHERE id=?`, content, millis(time.Now()), messageID)
		if e != nil {
			return e
		}
		seq++
	}
	_, e = tx.ExecContext(ctx, `DELETE FROM messages WHERE conversation_id=? AND seq>=?`, conversationID, seq)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE conversations SET memory_summary='',summary_through_seq=0,updated_at=? WHERE id=?`, millis(time.Now()), conversationID)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) DeleteLastAssistant(ctx context.Context, conversationID string) (domain.Message, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return domain.Message{}, e
	}
	defer tx.Rollback()
	m, e := scanMessage(tx.QueryRowContext(ctx, messageSelect+` WHERE conversation_id=? AND role='assistant' ORDER BY seq DESC LIMIT 1`, conversationID))
	if errors.Is(e, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	if e != nil {
		return m, e
	}
	_, e = tx.ExecContext(ctx, `DELETE FROM messages WHERE id=?`, m.ID)
	if e != nil {
		return m, e
	}
	if e = tx.Commit(); e != nil {
		return m, e
	}
	return m, nil
}
func (s *Store) AppendDraft(ctx context.Context, conversationID string) (domain.Message, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return domain.Message{}, e
	}
	defer tx.Rollback()
	var seq int64
	e = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0)+1 FROM messages WHERE conversation_id=?`, conversationID).Scan(&seq)
	if e != nil {
		return domain.Message{}, e
	}
	id, _ := security.NewID()
	now := time.Now()
	_, e = tx.ExecContext(ctx, `INSERT INTO messages(id,conversation_id,seq,role,content,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, id, conversationID, seq, "assistant", "", domain.MessageGenerating, millis(now), millis(now))
	if e != nil {
		return domain.Message{}, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE conversations SET updated_at=? WHERE id=?`, millis(now), conversationID); e != nil {
		return domain.Message{}, e
	}
	if e = tx.Commit(); e != nil {
		return domain.Message{}, e
	}
	return s.GetMessage(ctx, id)
}

func millis(t time.Time) int64     { return t.UnixMilli() }
func fromMillis(v int64) time.Time { return time.UnixMilli(v).UTC() }
func nullBytes(v []byte) any {
	if len(v) == 0 {
		return nil
	}
	return v
}
func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
