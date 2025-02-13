package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type RepoPermissions struct {
	OrganizationName string            `json:"org_name"`
	RepositoryName   string            `json:"repo_name"`
	TeamAccess       map[string]string `json:"team_access"` // team -> access level
	UserAccess       map[string]string `json:"user_access"` // username -> access level
	LastUpdated      time.Time         `json:"last_updated"`
}

type PermissionStore struct {
	repoDir     string
	permFile    string
	permissions *RepoPermissions
	mu          sync.RWMutex
	checker     *RepoPermissionChecker
}

func NewPermissionStore(repoDir string, checker *RepoPermissionChecker) (*PermissionStore, error) {
	permFile := filepath.Join(repoDir, ".gitsync", "permissions.json")

	// Ensure .gitsync directory exists
	if err := os.MkdirAll(filepath.Dir(permFile), 0755); err != nil {
		return nil, err
	}

	store := &PermissionStore{
		repoDir:  repoDir,
		permFile: permFile,
		checker:  checker,
	}

	if err := store.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return store, nil
}

func (s *PermissionStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.permFile)
	if err != nil {
		if os.IsNotExist(err) {
			s.permissions = &RepoPermissions{
				TeamAccess:  make(map[string]string),
				UserAccess:  make(map[string]string),
				LastUpdated: time.Now(),
			}
			return nil
		}
		return err
	}

	s.permissions = &RepoPermissions{}
	return json.Unmarshal(data, s.permissions)
}

func (s *PermissionStore) save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := json.MarshalIndent(s.permissions, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.permFile, data, 0644)
}

func (s *PermissionStore) RefreshFromGitHub(user *User) error {
	if !user.IsGitHubUser() || s.permissions == nil {
		return fmt.Errorf("invalid user or permissions not initialized")
	}

	// Check repository access using GitHub API
	err := s.checker.HasRepoAccess(user, s.permissions.OrganizationName, s.permissions.RepositoryName)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Update user's direct access
	s.permissions.UserAccess[user.Git.Username] = "write"

	// Update team access if user is in any teams
	for _, team := range user.Git.Teams {
		// Only update teams in the same organization
		if user.HasGitHubOrg(s.permissions.OrganizationName) {
			s.permissions.TeamAccess[team.Slug] = "write"
		}
	}

	s.permissions.LastUpdated = time.Now()
	return s.save()
}

func (s *PermissionStore) CheckAccess(user *User) (bool, error) {
	if !user.IsGitHubUser() {
		return false, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check if permissions need refresh (older than 1 hour)
	if time.Since(s.permissions.LastUpdated) > time.Hour {
		// Don't block on refresh, do it in background
		go s.RefreshFromGitHub(user)
	}

	// Check direct user access
	if access, ok := s.permissions.UserAccess[user.Git.Username]; ok && access != "" {
		return true, nil
	}

	// Check team access
	for _, team := range user.Git.Teams {
		if access, ok := s.permissions.TeamAccess[team.Slug]; ok && access != "" {
			return true, nil
		}
	}

	return false, nil
}
