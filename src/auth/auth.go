package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const CookieName = "metaatoms_session"

type User struct {
	UserID       string    `json:"user_id"`
	Nickname     string    `json:"nickname"`
	Email        string    `json:"email"`
	PasswordSalt string    `json:"password_salt"`
	PasswordHash string    `json:"password_hash"`
	CreatedAt    time.Time `json:"created_at"`
}

type publicUser struct {
	UserID   string `json:"user_id"`
	Nickname string `json:"nickname"`
	Email    string `json:"email"`
}

type Store struct {
	path     string
	userRoot string
	mu       sync.Mutex
	users    map[string]User
	sessions map[string]string
}

func NewStore(baseDir string) (*Store, error) {
	if strings.TrimSpace(baseDir) == "" {
		return nil, errors.New("auth base dir is empty")
	}
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}
	s := &Store{
		path:     filepath.Join(baseDir, "user_data.dat"),
		userRoot: baseDir,
		users:    make(map[string]User),
		sessions: make(map[string]string),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func BaseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".metaatoms"), nil
}

func UserDir(baseDir, userID string) string {
	return filepath.Join(baseDir, userID)
}

func (s *Store) Register(nickname, email, password string) (User, error) {
	nickname = strings.TrimSpace(nickname)
	email = strings.ToLower(strings.TrimSpace(email))
	if nickname == "" || email == "" || password == "" {
		return User{}, errors.New("nickname, email and password are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[email]; ok {
		return User{}, errors.New("email already registered")
	}
	salt := randomID()
	u := User{
		UserID:       randomUUID(),
		Nickname:     nickname,
		Email:        email,
		PasswordSalt: salt,
		PasswordHash: hashPassword(salt, password),
		CreatedAt:    time.Now(),
	}
	s.users[email] = u
	if err := s.saveLocked(); err != nil {
		return User{}, err
	}
	_ = os.MkdirAll(UserDir(s.userRoot, u.UserID), 0755)
	return u, nil
}

func (s *Store) Login(email, password string) (token string, user User, err error) {
	email = strings.ToLower(strings.TrimSpace(email))
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[email]
	if !ok || u.PasswordHash != hashPassword(u.PasswordSalt, password) {
		return "", User{}, errors.New("invalid email or password")
	}
	token = randomID()
	s.sessions[token] = u.UserID
	return token, u, nil
}

func (s *Store) Logout(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

func (s *Store) UserIDFromRequest(r *http.Request) (string, bool) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	uid, ok := s.sessions[c.Value]
	return uid, ok
}

func (s *Store) PublicUser(userID string) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.UserID == userID {
			return publicUser{UserID: u.UserID, Nickname: u.Nickname, Email: u.Email}, true
		}
	}
	return nil, false
}

func SetSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var users []User
	if err := json.Unmarshal(data, &users); err != nil {
		return fmt.Errorf("parse user_data.dat: %w", err)
	}
	for _, u := range users {
		s.users[strings.ToLower(u.Email)] = u
	}
	return nil
}

func (s *Store) saveLocked() error {
	users := make([]User, 0, len(s.users))
	for _, u := range s.users {
		users = append(users, u)
	}
	data, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

func hashPassword(salt, password string) string {
	sum := sha256.Sum256([]byte(salt + ":" + password))
	return hex.EncodeToString(sum[:])
}

func randomID() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func randomUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
