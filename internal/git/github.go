package git

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/jmendick/gitsync/internal/auth"
)

// ParseGitHubURL extracts owner and repo name from GitHub URL
func ParseGitHubURL(repoURL string) (*RepositoryMetadata, error) {
	u, err := url.Parse(repoURL)
	if err != nil {
		return nil, err
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid GitHub repository URL")
	}

	return &RepositoryMetadata{
		Owner:    parts[0],
		Name:     parts[1],
		CloneURL: repoURL,
	}, nil
}

func (m *GitRepositoryManager) GetRepoMetadata(ctx context.Context, user *auth.User, owner, repo string) (*RepositoryMetadata, error) {
	if !user.IsGitHubUser() {
		return nil, fmt.Errorf("GitHub authentication required")
	}

	req, err := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+user.Git.AccessToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get repository metadata: %d", resp.StatusCode)
	}

	var repoData struct {
		CloneURL      string `json:"clone_url"`
		Private       bool   `json:"private"`
		DefaultBranch string `json:"default_branch"`
	}
	err = json.NewDecoder(resp.Body).Decode(&repoData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode repository data: %w", err)
	}

	return &RepositoryMetadata{
		Owner:         owner,
		Name:          repo,
		CloneURL:      repoData.CloneURL,
		Private:       repoData.Private,
		DefaultBranch: repoData.DefaultBranch,
	}, nil
}
