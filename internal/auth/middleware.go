package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const (
	UserContextKey       contextKey = "user"
	tokenDuration                   = 1 * time.Hour
	refreshTokenDuration            = 30 * 24 * time.Hour
)

type TokenManager struct {
	secretKey     []byte
	store         UserStore
	revokedTokens sync.Map
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func NewTokenManager(secretKey string, store UserStore) *TokenManager {
	return &TokenManager{
		secretKey: []byte(secretKey),
		store:     store,
	}
}

func (tm *TokenManager) GenerateTokenPair(user *User) (*TokenPair, error) {
	// Generate access token
	accessToken, err := tm.generateToken(user, tokenDuration)
	if err != nil {
		return nil, err
	}

	// Generate refresh token
	refreshToken, err := tm.generateToken(user, refreshTokenDuration)
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}

func (tm *TokenManager) generateToken(user *User, duration time.Duration) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"username": user.Username,
		"role":     string(user.Role),
		"exp":      time.Now().Add(duration).Unix(),
		"iat":      time.Now().Unix(),
	})

	return token.SignedString(tm.secretKey)
}

func (tm *TokenManager) RevokeToken(tokenString string) {
	tm.revokedTokens.Store(tokenString, time.Now())
}

func (tm *TokenManager) IsTokenRevoked(tokenString string) bool {
	_, revoked := tm.revokedTokens.Load(tokenString)
	return revoked
}

func (tm *TokenManager) RefreshToken(refreshToken string) (*TokenPair, error) {
	// Validate the refresh token
	user, err := tm.ValidateToken(refreshToken)
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token")
	}

	// Check if token is revoked
	if tm.IsTokenRevoked(refreshToken) {
		return nil, fmt.Errorf("refresh token has been revoked")
	}

	// Revoke the old refresh token
	tm.RevokeToken(refreshToken)

	// Generate new token pair
	return tm.GenerateTokenPair(user)
}

func (tm *TokenManager) ValidateToken(tokenString string) (*User, error) {
	// Check if token is revoked
	if tm.IsTokenRevoked(tokenString) {
		return nil, fmt.Errorf("token has been revoked")
	}

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return tm.secretKey, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		username, ok := claims["username"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid token claims")
		}

		return tm.store.GetUser(username)
	}

	return nil, fmt.Errorf("invalid token")
}

// AuthMiddleware creates a middleware that validates JWT tokens
func (tm *TokenManager) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Authorization header required", http.StatusUnauthorized)
			return
		}

		tokenParts := strings.Split(authHeader, " ")
		if len(tokenParts) != 2 || tokenParts[0] != "Bearer" {
			http.Error(w, "Invalid authorization header format", http.StatusUnauthorized)
			return
		}

		user, err := tm.ValidateToken(tokenParts[1])
		if err != nil {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), UserContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRole creates middleware that checks if the authenticated user has the required role
func RequireRole(roles ...Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := r.Context().Value(UserContextKey).(*User)
			if !ok {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			for _, role := range roles {
				if user.Role == role {
					next.ServeHTTP(w, r)
					return
				}
			}

			http.Error(w, "Forbidden", http.StatusForbidden)
		})
	}
}
