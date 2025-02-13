package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type FileUserStore struct {
	path  string
	mu    sync.RWMutex
	users map[string]*User
}

func NewFileUserStore(path string) (*FileUserStore, error) {
	store := &FileUserStore{
		path:  path,
		users: make(map[string]*User),
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	if err := store.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return store, nil
}

func (s *FileUserStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &s.users)
}

func (s *FileUserStore) save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := json.MarshalIndent(s.users, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, data, 0644)
}

func (s *FileUserStore) CreateUser(username, password string, role Role) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.users[username]; exists {
		return ErrUserExists
	}

	user, err := NewUser(username, password, role)
	if err != nil {
		return err
	}

	s.users[username] = user
	return s.save()
}

func (s *FileUserStore) GetUser(username string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, exists := s.users[username]
	if !exists {
		return nil, ErrUserNotFound
	}
	return user, nil
}

func (s *FileUserStore) GetUserByEmail(email string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, user := range s.users {
		if user.Email == email {
			return user, nil
		}
	}
	return nil, ErrUserNotFound
}

func (s *FileUserStore) UpdateUser(user *User) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.users[user.Username]; !exists {
		return ErrUserNotFound
	}

	s.users[user.Username] = user
	return s.save()
}

func (s *FileUserStore) DeleteUser(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.users[username]; !exists {
		return ErrUserNotFound
	}

	delete(s.users, username)
	return s.save()
}

func (s *FileUserStore) ListUsers() ([]*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	users := make([]*User, 0, len(s.users))
	for _, user := range s.users {
		users = append(users, user)
	}
	return users, nil
}

func (s *FileUserStore) Authenticate(username, password string) (*User, error) {
	user, err := s.GetUser(username)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	if !user.ValidatePassword(password) {
		return nil, ErrInvalidCredentials
	}

	user.LastLoginAt = time.Now()
	if err := s.UpdateUser(user); err != nil {
		return nil, err
	}

	return user, nil
}
