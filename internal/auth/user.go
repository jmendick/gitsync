package auth

import (
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserExists         = errors.New("user already exists")
	ErrInvalidToken       = errors.New("invalid or expired token")
	ErrTOTPRequired       = errors.New("2FA code required")
	ErrInvalidTOTPCode    = errors.New("invalid 2FA code")
)

type User struct {
	Username          string          `json:"username"`
	Password          []byte          `json:"-"`
	Email             string          `json:"email"`
	EmailVerified     bool            `json:"email_verified"`
	VerificationToken string          `json:"-"`
	ResetToken        string          `json:"-"`
	ResetTokenExpiry  time.Time       `json:"-"`
	Role              Role            `json:"role"`
	CreatedAt         time.Time       `json:"created_at"`
	LastLoginAt       time.Time       `json:"last_login_at"`
	Git               *GitCredentials `json:"git,omitempty"`
	AuthType          string          `json:"auth_type"`
	FailedAttempts    int             `json:"-"`
	LockedUntil       time.Time       `json:"-"`
	TOTPEnabled       bool            `json:"totp_enabled"`
	TOTPSecret        string          `json:"-"`
	TOTPBackupCodes   []string        `json:"-"`
}

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleGuest  Role = "guest"
)

type UserStore interface {
	CreateUser(username string, password string, role Role) error
	GetUser(username string) (*User, error)
	GetUserByEmail(email string) (*User, error)
	UpdateUser(user *User) error
	DeleteUser(username string) error
	ListUsers() ([]*User, error)
	Authenticate(username, password string) (*User, error)
}

// NewUser creates a new user with a hashed password
func NewUser(username, password string, role Role) (*User, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	return &User{
		Username:    username,
		Password:    hashedPassword,
		Role:        role,
		CreatedAt:   time.Now(),
		LastLoginAt: time.Now(),
	}, nil
}

// ValidatePassword checks if the provided password matches the stored hash
func (u *User) ValidatePassword(password string) bool {
	err := bcrypt.CompareHashAndPassword(u.Password, []byte(password))
	return err == nil
}

type GitHubOrganization struct {
	Name  string `json:"name"`
	Login string `json:"login"`
}

type GitHubTeam struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// GitCredentials stores Git-specific credentials and org memberships
type GitCredentials struct {
	AccessToken   string               `json:"-"` // GitHub access token
	Username      string               `json:"username"`
	Organizations []GitHubOrganization `json:"organizations,omitempty"`
	Teams         []GitHubTeam         `json:"teams,omitempty"`
}

// IsGitHubUser returns true if the user was authenticated via GitHub
func (u *User) IsGitHubUser() bool {
	return u.AuthType == "github" && u.Git != nil && u.Git.AccessToken != ""
}

// HasGitHubOrg checks if the user is a member of the specified GitHub organization
func (u *User) HasGitHubOrg(orgName string) bool {
	if !u.IsGitHubUser() || u.Git == nil {
		return false
	}

	for _, org := range u.Git.Organizations {
		if org.Login == orgName {
			return true
		}
	}
	return false
}

// HasGitHubTeam checks if the user is a member of the specified GitHub team
func (u *User) HasGitHubTeam(orgName, teamSlug string) bool {
	if !u.IsGitHubUser() || u.Git == nil {
		return false
	}

	// First check org membership
	if !u.HasGitHubOrg(orgName) {
		return false
	}

	for _, team := range u.Git.Teams {
		if team.Slug == teamSlug {
			return true
		}
	}
	return false
}

// IsAccountLocked checks if the account is temporarily locked due to too many failed attempts
func (u *User) IsAccountLocked() bool {
	return time.Now().Before(u.LockedUntil)
}

// IncrementFailedAttempts increases the failed login attempts counter and locks the account if necessary
func (u *User) IncrementFailedAttempts() {
	u.FailedAttempts++
	if u.FailedAttempts >= 5 { // Lock after 5 failed attempts
		u.LockedUntil = time.Now().Add(15 * time.Minute)
	}
}

// ResetFailedAttempts resets the failed attempts counter after successful login
func (u *User) ResetFailedAttempts() {
	u.FailedAttempts = 0
	u.LockedUntil = time.Time{}
}

// IsEmailVerificationPending returns true if email verification is pending
func (u *User) IsEmailVerificationPending() bool {
	return u.Email != "" && !u.EmailVerified && u.VerificationToken != ""
}

// IsPasswordResetTokenValid checks if the password reset token is valid and not expired
func (u *User) IsPasswordResetTokenValid(token string) bool {
	return u.ResetToken != "" &&
		u.ResetToken == token &&
		time.Now().Before(u.ResetTokenExpiry)
}

// IsTOTPRequired returns true if 2FA is enabled and required
func (u *User) IsTOTPRequired() bool {
	return u.TOTPEnabled && u.TOTPSecret != ""
}

// ValidateTOTPCode checks if the provided TOTP code is valid
func (u *User) ValidateTOTPCode(code string) bool {
	if !u.TOTPEnabled {
		return true
	}

	// Verify and remove the used backup code
	for i, backupCode := range u.TOTPBackupCodes {
		if backupCode == code {
			// Remove the used backup code
			u.TOTPBackupCodes = append(u.TOTPBackupCodes[:i], u.TOTPBackupCodes[i+1:]...)
			return true
		}
	}

	// Validate TOTP code
	return validateTOTP(u.TOTPSecret, code)
}
