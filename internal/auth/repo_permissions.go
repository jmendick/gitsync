package auth

import (
	"fmt"
	"net/http"
	"strings"
)

// RepoPermissionChecker validates repository access permissions
type RepoPermissionChecker struct {
	store UserStore
}

func NewRepoPermissionChecker(store UserStore) *RepoPermissionChecker {
	return &RepoPermissionChecker{store: store}
}

// HasRepoAccess checks if a user has access to a specific repository
func (pc *RepoPermissionChecker) HasRepoAccess(user *User, owner, repo string) error {
	if !user.IsGitHubUser() {
		return fmt.Errorf("repository access restricted to GitHub users")
	}

	// Check if the user is the owner
	if strings.EqualFold(user.Git.Username, owner) {
		return nil
	}

	// Query GitHub API to check repository permissions
	req, err := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo), nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+user.Git.AccessToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil // User has access
	case http.StatusNotFound:
		return fmt.Errorf("repository not found or no access")
	case http.StatusUnauthorized:
		return fmt.Errorf("invalid GitHub token")
	default:
		return fmt.Errorf("GitHub API returned unexpected status: %d", resp.StatusCode)
	}
}

// RequireRepoAccess creates middleware that checks if the user has access to the repository
func (pc *RepoPermissionChecker) RequireRepoAccess(owner, repo string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := r.Context().Value(UserContextKey).(*User)
			if !ok {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			if err := pc.HasRepoAccess(user, owner, repo); err != nil {
				http.Error(w, "Repository access denied: "+err.Error(), http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
