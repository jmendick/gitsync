package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jmendick/gitsync/internal/config"
)

const (
	defaultTokenDuration = 24 * time.Hour
	tempTokenDuration    = 5 * time.Minute
)

// Request types
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     Role   `json:"role"`
}

// Add new request/response types
type resetPasswordRequest struct {
	Email string `json:"email"`
}

type verifyEmailRequest struct {
	Username string `json:"username"`
	Token    string `json:"token"`
}

type changePasswordRequest struct {
	Username    string `json:"username"`
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// Add new request/response types for 2FA
type setup2FAResponse struct {
	Secret      string   `json:"secret"`
	QRCode      string   `json:"qr_code"`
	BackupCodes []string `json:"backup_codes"`
}

type verify2FARequest struct {
	Code string `json:"code"`
}

type loginResponse struct {
	Token       string `json:"token,omitempty"`
	User        *User  `json:"user,omitempty"`
	Requires2FA bool   `json:"requires_2fa,omitempty"`
	TempToken   string `json:"temp_token,omitempty"`
}

// Update AuthHandler to include UserService
type AuthHandler struct {
	store       UserStore
	tokenMgr    *TokenManager
	githubAuth  *GitHubOAuth
	userSvc     *UserService
	rateLimiter *RateLimiter
}

// Update constructor
func NewAuthHandler(store UserStore, tokenMgr *TokenManager, cfg *config.Config, emailSvc EmailService) *AuthHandler {
	var githubAuth *GitHubOAuth
	if cfg.Auth.GitHubOAuth.Enabled {
		githubAuth = NewGitHubOAuth(
			cfg.Auth.GitHubOAuth.ClientID,
			cfg.Auth.GitHubOAuth.ClientSecret,
			cfg.Auth.GitHubOAuth.CallbackURL,
			store,
			tokenMgr,
		)
	}

	rateLimiter := NewRateLimiter(5, time.Hour, 15*time.Minute)
	userSvc := NewUserService(store, emailSvc)

	return &AuthHandler{
		store:       store,
		tokenMgr:    tokenMgr,
		githubAuth:  githubAuth,
		userSvc:     userSvc,
		rateLimiter: rateLimiter,
	}
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	user, err := h.store.Authenticate(req.Username, req.Password)
	if err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	if user.IsAccountLocked() {
		http.Error(w, "Account is temporarily locked", http.StatusForbidden)
		return
	}

	// If 2FA is enabled, return a temporary token and require 2FA code
	if user.IsTOTPRequired() {
		tempToken, err := h.tokenMgr.generateToken(user, tempTokenDuration)
		if err != nil {
			http.Error(w, "Failed to generate temporary token", http.StatusInternalServerError)
			return
		}

		respondJSON(w, http.StatusOK, loginResponse{
			Requires2FA: true,
			TempToken:   tempToken,
		})
		return
	}

	// Generate final token for successful login
	token, err := h.tokenMgr.generateToken(user, defaultTokenDuration)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	// Reset failed attempts on successful login
	user.ResetFailedAttempts()
	if err := h.store.UpdateUser(user); err != nil {
		// Log error but continue
		fmt.Printf("Failed to update user failed attempts: %v\n", err)
	}

	respondJSON(w, http.StatusOK, loginResponse{
		Token: token,
		User:  user,
	})
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate password complexity
	if err := validatePasswordComplexity(req.Password); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Create the user
	if err := h.store.CreateUser(req.Username, req.Password, req.Role); err != nil {
		if err == ErrUserExists {
			http.Error(w, "Username already taken", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	// Return success
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "User created successfully",
	})
}

func (h *AuthHandler) GitHubCallback(w http.ResponseWriter, r *http.Request) {
	if h.githubAuth == nil {
		http.Error(w, "GitHub authentication not configured", http.StatusNotImplemented)
		return
	}
	h.githubAuth.HandleCallback(w, r)
}

func (h *AuthHandler) GitHubLogin(w http.ResponseWriter, r *http.Request) {
	if h.githubAuth == nil {
		http.Error(w, "GitHub authentication not configured", http.StatusNotImplemented)
		return
	}
	h.githubAuth.HandleLogin(w, r)
}

// Add new handler methods
func (h *AuthHandler) InitiatePasswordReset(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.userSvc.InitiatePasswordReset(req.Email); err != nil {
		// Don't reveal if email exists or any other errors
		respondJSON(w, http.StatusOK, map[string]string{
			"message": "If an account exists with this email, you will receive password reset instructions",
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"message": "Password reset email sent",
	})
}

func (h *AuthHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := validatePasswordComplexity(req.NewPassword); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.userSvc.ResetPassword(req.Username, req.Token, req.NewPassword); err != nil {
		switch err {
		case ErrUserNotFound, ErrInvalidToken:
			http.Error(w, "Invalid or expired reset token", http.StatusBadRequest)
		default:
			http.Error(w, "Failed to reset password", http.StatusInternalServerError)
		}
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"message": "Password reset successful",
	})
}

func (h *AuthHandler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req verifyEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.userSvc.VerifyEmail(req.Username, req.Token); err != nil {
		switch err {
		case ErrUserNotFound, ErrInvalidToken:
			http.Error(w, "Invalid verification token", http.StatusBadRequest)
		default:
			http.Error(w, "Failed to verify email", http.StatusInternalServerError)
		}
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"message": "Email verified successfully",
	})
}

func (h *AuthHandler) Setup2FA(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(UserContextKey).(*User)

	if user.TOTPEnabled {
		http.Error(w, "2FA is already enabled", http.StatusBadRequest)
		return
	}

	config := NewDefaultTOTPConfig("GitSync")
	key, err := GenerateTOTPSecret(user.Username, config)
	if err != nil {
		http.Error(w, "Failed to generate 2FA secret", http.StatusInternalServerError)
		return
	}

	backupCodes, err := GenerateBackupCodes()
	if err != nil {
		http.Error(w, "Failed to generate backup codes", http.StatusInternalServerError)
		return
	}

	// Store secret and backup codes temporarily until verified
	user.TOTPSecret = key.Secret()
	user.TOTPBackupCodes = backupCodes
	if err := h.store.UpdateUser(user); err != nil {
		http.Error(w, "Failed to update user", http.StatusInternalServerError)
		return
	}

	resp := setup2FAResponse{
		Secret:      key.Secret(),
		QRCode:      GetTOTPQRCode(key),
		BackupCodes: backupCodes,
	}

	respondJSON(w, http.StatusOK, resp)
}

func (h *AuthHandler) Verify2FA(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(UserContextKey).(*User)

	var req verify2FARequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if !user.ValidateTOTPCode(req.Code) {
		http.Error(w, "Invalid 2FA code", http.StatusBadRequest)
		return
	}

	// Enable 2FA after successful verification
	user.TOTPEnabled = true
	if err := h.store.UpdateUser(user); err != nil {
		http.Error(w, "Failed to enable 2FA", http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"message": "2FA enabled successfully",
	})
}

func (h *AuthHandler) Disable2FA(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(UserContextKey).(*User)

	var req verify2FARequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if !user.ValidateTOTPCode(req.Code) {
		http.Error(w, "Invalid 2FA code", http.StatusBadRequest)
		return
	}

	// Disable 2FA
	user.TOTPEnabled = false
	user.TOTPSecret = ""
	user.TOTPBackupCodes = nil
	if err := h.store.UpdateUser(user); err != nil {
		http.Error(w, "Failed to disable 2FA", http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"message": "2FA disabled successfully",
	})
}

func (h *AuthHandler) Verify2FALogin(w http.ResponseWriter, r *http.Request) {
	var req verify2FARequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get user from temporary token in auth header
	user := r.Context().Value(UserContextKey).(*User)

	if !user.ValidateTOTPCode(req.Code) {
		user.IncrementFailedAttempts()
		if err := h.store.UpdateUser(user); err != nil {
			fmt.Printf("Failed to update user failed attempts: %v\n", err)
		}
		http.Error(w, "Invalid 2FA code", http.StatusUnauthorized)
		return
	}

	// Generate final token after successful 2FA
	token, err := h.tokenMgr.generateToken(user, defaultTokenDuration)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	// Reset failed attempts on successful verification
	user.ResetFailedAttempts()
	if err := h.store.UpdateUser(user); err != nil {
		fmt.Printf("Failed to update user failed attempts: %v\n", err)
	}

	respondJSON(w, http.StatusOK, loginResponse{
		Token: token,
		User:  user,
	})
}

// Update RegisterRoutes to include new endpoints
func (h *AuthHandler) RegisterRoutes(mux *http.ServeMux) {
	// Wrap routes that need rate limiting
	loginHandler := h.rateLimiter.Middleware(http.HandlerFunc(h.Login))
	registerHandler := h.rateLimiter.Middleware(http.HandlerFunc(h.Register))

	// Basic auth endpoints
	mux.Handle("/auth/login", loginHandler)
	mux.Handle("/auth/register", registerHandler)
	mux.HandleFunc("/auth/reset-password", h.InitiatePasswordReset)
	mux.HandleFunc("/auth/reset-password/confirm", h.ResetPassword)
	mux.HandleFunc("/auth/verify-email", h.VerifyEmail)

	// GitHub OAuth endpoints
	if h.githubAuth != nil {
		mux.HandleFunc("/auth/github/login", h.GitHubLogin)
		mux.HandleFunc("/auth/github/callback", h.GitHubCallback)
	}

	// 2FA endpoints (protected by authentication middleware)
	mux.Handle("/auth/2fa/setup", h.tokenMgr.AuthMiddleware(http.HandlerFunc(h.Setup2FA)))
	mux.Handle("/auth/2fa/verify", h.tokenMgr.AuthMiddleware(http.HandlerFunc(h.Verify2FA)))
	mux.Handle("/auth/2fa/disable", h.tokenMgr.AuthMiddleware(http.HandlerFunc(h.Disable2FA)))

	// Add 2FA verification endpoint for login
	mux.Handle("/auth/2fa/verify-login", h.tokenMgr.AuthMiddleware(http.HandlerFunc(h.Verify2FALogin)))
}

// Helper function to validate password complexity
func validatePasswordComplexity(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters long")
	}
	// Add more password complexity rules as needed
	return nil
}

// Helper function to respond with JSON
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}
