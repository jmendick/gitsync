package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type EmailService interface {
	SendVerificationEmail(to, token string) error
	SendPasswordResetEmail(to, token string) error
}

type UserService struct {
	store        UserStore
	emailService EmailService
	tokenExpiry  time.Duration
}

func NewUserService(store UserStore, emailService EmailService) *UserService {
	return &UserService{
		store:        store,
		emailService: emailService,
		tokenExpiry:  24 * time.Hour, // tokens expire after 24 hours
	}
}

func generateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func (s *UserService) InitiateEmailVerification(user *User) error {
	token, err := generateToken()
	if err != nil {
		return fmt.Errorf("failed to generate verification token: %w", err)
	}

	user.VerificationToken = token
	if err := s.store.UpdateUser(user); err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	return s.emailService.SendVerificationEmail(user.Email, token)
}

func (s *UserService) VerifyEmail(username, token string) error {
	user, err := s.store.GetUser(username)
	if err != nil {
		return ErrUserNotFound
	}

	if user.VerificationToken != token {
		return ErrInvalidToken
	}

	user.EmailVerified = true
	user.VerificationToken = "" // Clear the token
	return s.store.UpdateUser(user)
}

func (s *UserService) InitiatePasswordReset(email string) error {
	user, err := s.store.GetUserByEmail(email)
	if err != nil {
		// Don't reveal if email exists
		return nil
	}

	token, err := generateToken()
	if err != nil {
		return fmt.Errorf("failed to generate reset token: %w", err)
	}

	user.ResetToken = token
	user.ResetTokenExpiry = time.Now().Add(s.tokenExpiry)
	if err := s.store.UpdateUser(user); err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	return s.emailService.SendPasswordResetEmail(email, token)
}

func (s *UserService) ResetPassword(username, token, newPassword string) error {
	user, err := s.store.GetUser(username)
	if err != nil {
		return ErrUserNotFound
	}

	if !user.IsPasswordResetTokenValid(token) {
		return ErrInvalidToken
	}

	// Hash the new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	user.Password = hashedPassword
	user.ResetToken = ""
	user.ResetTokenExpiry = time.Time{}

	return s.store.UpdateUser(user)
}
