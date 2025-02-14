package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

type githubUser struct {
	Login string `json:"login"`
	Email string `json:"email"`
	Name  string `json:"name"`
	ID    int64  `json:"id"`
}

type GitHubOAuth struct {
	config   *oauth2.Config
	store    UserStore
	tokenMgr *TokenManager
	states   *stateStore
}

func NewGitHubOAuth(clientID, clientSecret, callbackURL string, store UserStore, tokenMgr *TokenManager) *GitHubOAuth {
	return &GitHubOAuth{
		config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  callbackURL,
			Scopes:       []string{"user:email", "repo", "read:org"}, // Added read:org scope
			Endpoint:     github.Endpoint,
		},
		store:    store,
		tokenMgr: tokenMgr,
		states:   newStateStore(),
	}
}

func (gh *GitHubOAuth) HandleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := gh.states.Generate()
	if (err != nil) {
		http.Error(w, "Failed to generate state token", http.StatusInternalServerError)
		return
	}

	url := gh.config.AuthCodeURL(state)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (gh *GitHubOAuth) HandleCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if !gh.states.Validate(state) {
		http.Error(w, "Invalid or expired state parameter", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	token, err := gh.config.Exchange(context.Background(), code)
	if err != nil {
		http.Error(w, "Failed to exchange token", http.StatusInternalServerError)
		return
	}

	githubUser, err := gh.getGitHubUser(token.AccessToken)
	if err != nil {
		http.Error(w, "Failed to get GitHub user", http.StatusInternalServerError)
		return
	}

	// Fetch user's organizations and teams
	orgs, err := gh.getGitHubOrgs(token.AccessToken)
	if err != nil {
		http.Error(w, "Failed to get GitHub organizations", http.StatusInternalServerError)
		return
	}

	teams, err := gh.getGitHubTeams(token.AccessToken)
	if err != nil {
		http.Error(w, "Failed to get GitHub teams", http.StatusInternalServerError)
		return
	}

	// Get or create local user
	user, err := gh.store.GetUser(githubUser.Login)
	isNewUser := err == ErrUserNotFound

	if isNewUser {
		// Create new user with GitHub username
		user, err = NewUser(githubUser.Login, "", RoleMember)
		if err != nil {
			http.Error(w, "Failed to create user", http.StatusInternalServerError)
			return
		}
	} else if err != nil {
		http.Error(w, "Failed to process user", http.StatusInternalServerError)
		return
	}

	// Update GitHub credentials and memberships
	user.AuthType = "github"
	user.Git = &GitCredentials{
		AccessToken:   token.AccessToken,
		Username:      githubUser.Login,
		Organizations: orgs,
		Teams:         teams,
	}

	// Save or update user
	if isNewUser {
		if err := gh.store.CreateUser(user.Username, "", RoleMember); err != nil {
			http.Error(w, "Failed to save user", http.StatusInternalServerError)
			return
		}
	} else {
		if err := gh.store.UpdateUser(user); err != nil {
			http.Error(w, "Failed to update user", http.StatusInternalServerError)
			return
		}
	}

	// Generate JWT token
	jwtToken, err := gh.tokenMgr.generateToken(user, tokenDuration)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	// Update last login
	user.LastLoginAt = time.Now()
	if err := gh.store.UpdateUser(user); err != nil {
		// Log error but continue
		fmt.Printf("Failed to update last login: %v\n", err)
	}

	// Return token in response
	resp := loginResponse{
		Token: jwtToken,
		User:  user,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (gh *GitHubOAuth) HandleCode(code string) (*User, error) {
	token, err := gh.config.Exchange(context.Background(), code)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange token: %w", err)
	}

	githubUser, err := gh.getGitHubUser(token.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to get GitHub user: %w", err)
	}

	// Fetch user's organizations and teams
	orgs, err := gh.getGitHubOrgs(token.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to get GitHub organizations: %w", err)
	}

	teams, err := gh.getGitHubTeams(token.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to get GitHub teams: %w", err)
	}

	// Get or create local user
	user, err := gh.store.GetUser(githubUser.Login)
	isNewUser := err == ErrUserNotFound

	if isNewUser {
		user, err = NewUser(githubUser.Login, "", RoleMember)
		if err != nil {
			return nil, fmt.Errorf("failed to create user: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("failed to process user: %w", err)
	}

	// Update GitHub credentials and memberships
	user.AuthType = "github"
	user.Git = &GitCredentials{
		AccessToken:   token.AccessToken,
		Username:      githubUser.Login,
		Organizations: orgs,
		Teams:         teams,
	}

	// Save or update user
	if isNewUser {
		if err := gh.store.CreateUser(user.Username, "", RoleMember); err != nil {
			return nil, fmt.Errorf("failed to save user: %w", err)
		}
	} else {
		if err := gh.store.UpdateUser(user); err != nil {
			return nil, fmt.Errorf("failed to update user: %w", err)
		}
	}

	return user, nil
}

func (gh *GitHubOAuth) getGitHubUser(accessToken string) (*githubUser, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/user", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status: %d", resp.StatusCode)
	}

	var user githubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}

	return &user, nil
}

func (gh *GitHubOAuth) getGitHubOrgs(accessToken string) ([]GitHubOrganization, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/user/orgs", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status: %d", resp.StatusCode)
	}

	var orgs []GitHubOrganization
	if err := json.NewDecoder(resp.Body).Decode(&orgs); err != nil {
		return nil, err
	}

	return orgs, nil
}

func (gh *GitHubOAuth) getGitHubTeams(accessToken string) ([]GitHubTeam, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/user/teams", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status: %d", resp.StatusCode)
	}

	var teams []GitHubTeam
	if err := json.NewDecoder(resp.Body).Decode(&teams); err != nil {
		return nil, err
	}

	return teams, nil
}
