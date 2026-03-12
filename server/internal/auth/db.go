package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

var (
	ErrUserNotFound    = errors.New("user not found")
	ErrInvalidPassword = errors.New("invalid password")
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionExpired  = errors.New("session expired")
)

type User struct {
	ID        int64
	Username  string
	Password  string
	Role      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Session struct {
	ID        string
	UserID    int64
	Token     string
	ExpiresAt time.Time
	CreatedAt time.Time
}

type DB struct {
	db     *sql.DB
	logger *slog.Logger
}

func New(dbPath string, logger *slog.Logger) (*DB, error) {
	if logger == nil {
		logger = slog.Default()
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := initDB(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("init database: %w", err)
	}

	return &DB{db: db, logger: logger}, nil
}

func initDB(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'user',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		token TEXT UNIQUE NOT NULL,
		expires_at TEXT NOT NULL,
		created_at TEXT NOT NULL,
		FOREIGN KEY(user_id) REFERENCES users(id)
	);
	CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(token);
	CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
	`
	_, err := db.Exec(schema)
	return err
}

func (d *DB) CreateUser(username, password, role string) (*User, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	now := time.Now()
	result, err := d.db.Exec(
		"INSERT INTO users (username, password, role, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		username, string(hashedPassword), role, now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}

	id, _ := result.LastInsertId()
	return &User{
		ID:        id,
		Username:  username,
		Password:  string(hashedPassword),
		Role:      role,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (d *DB) GetUserByUsername(username string) (*User, error) {
	var user User
	var createdAt, updatedAt string

	err := d.db.QueryRow(
		"SELECT id, username, password, role, created_at, updated_at FROM users WHERE username = ?",
		username,
	).Scan(&user.ID, &user.Username, &user.Password, &user.Role, &createdAt, &updatedAt)

	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query user: %w", err)
	}

	user.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	user.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &user, nil
}

func (d *DB) GetUserByID(id int64) (*User, error) {
	var user User
	var createdAt, updatedAt string

	err := d.db.QueryRow(
		"SELECT id, username, password, role, created_at, updated_at FROM users WHERE id = ?",
		id,
	).Scan(&user.ID, &user.Username, &user.Password, &user.Role, &createdAt, &updatedAt)

	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query user: %w", err)
	}

	user.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	user.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &user, nil
}

func (d *DB) ValidatePassword(user *User, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password))
	return err == nil
}

func (d *DB) CreateSession(userID int64, ttl time.Duration) (*Session, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, fmt.Errorf("generate id: %w", err)
	}
	id := hex.EncodeToString(idBytes)

	now := time.Now()
	expiresAt := now.Add(ttl)

	_, err := d.db.Exec(
		"INSERT INTO sessions (id, user_id, token, expires_at, created_at) VALUES (?, ?, ?, ?, ?)",
		id, userID, token, expiresAt.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("insert session: %w", err)
	}

	return &Session{
		ID:        id,
		UserID:    userID,
		Token:     token,
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}, nil
}

func (d *DB) GetSessionByToken(token string) (*Session, error) {
	var session Session
	var expiresAt, createdAt string

	err := d.db.QueryRow(
		"SELECT id, user_id, token, expires_at, created_at FROM sessions WHERE token = ?",
		token,
	).Scan(&session.ID, &session.UserID, &session.Token, &expiresAt, &createdAt)

	if err == sql.ErrNoRows {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query session: %w", err)
	}

	session.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
	session.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

	if session.ExpiresAt.Before(time.Now()) {
		d.DeleteSession(token)
		return nil, ErrSessionExpired
	}

	return &session, nil
}

func (d *DB) DeleteSession(token string) error {
	_, err := d.db.Exec("DELETE FROM sessions WHERE token = ?", token)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (d *DB) DeleteUserSessions(userID int64) error {
	_, err := d.db.Exec("DELETE FROM sessions WHERE user_id = ?", userID)
	if err != nil {
		return fmt.Errorf("delete user sessions: %w", err)
	}
	return nil
}

func (d *DB) CleanExpiredSessions() error {
	_, err := d.db.Exec("DELETE FROM sessions WHERE expires_at < ?", time.Now().Format(time.RFC3339))
	return err
}

func (d *DB) Close() error {
	return d.db.Close()
}

type AuthService struct {
	db         *DB
	sessionTTL time.Duration
	logger     *slog.Logger
}

func NewService(dbPath string, sessionTTL time.Duration, logger *slog.Logger) (*AuthService, error) {
	db, err := New(dbPath, logger)
	if err != nil {
		return nil, err
	}

	return &AuthService{
		db:         db,
		sessionTTL: sessionTTL,
		logger:     logger,
	}, nil
}

func (s *AuthService) Bootstrap(username, password string) error {
	_, err := s.db.GetUserByUsername(username)
	if err == nil {
		return nil
	}
	if err != ErrUserNotFound {
		return err
	}

	user, err := s.db.CreateUser(username, password, "admin")
	if err != nil {
		return err
	}

	s.logger.Info("bootstrap: admin user created", "username", user.Username, "id", user.ID)
	return nil
}

func (s *AuthService) Login(ctx context.Context, username, password string) (*Session, error) {
	user, err := s.db.GetUserByUsername(username)
	if err != nil {
		return nil, err
	}

	if !s.db.ValidatePassword(user, password) {
		return nil, ErrInvalidPassword
	}

	session, err := s.db.CreateSession(user.ID, s.sessionTTL)
	if err != nil {
		return nil, err
	}

	s.logger.Info("auth: user logged in", "username", username, "session", session.ID)
	return session, nil
}

func (s *AuthService) Logout(ctx context.Context, token string) error {
	if err := s.db.DeleteSession(token); err != nil {
		return err
	}
	s.logger.Info("auth: session logged out", "token", token[:8]+"...")
	return nil
}

func (s *AuthService) ValidateSession(ctx context.Context, token string) (*User, error) {
	session, err := s.db.GetSessionByToken(token)
	if err != nil {
		return nil, err
	}

	user, err := s.db.GetUserByID(session.UserID)
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (s *AuthService) GetDB() *DB {
	return s.db
}

func (s *AuthService) Close() error {
	return s.db.Close()
}

func GetDefaultAuthDBPath(configDir string) string {
	return filepath.Join(configDir, "auth.db")
}

func EnsureAuthDB(configDir string, logger *slog.Logger) (string, error) {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", err
	}
	return GetDefaultAuthDBPath(configDir), nil
}
