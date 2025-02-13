package auth

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const (
	totpPeriod     = 30
	totpDigits     = 6
	backupCodeLen  = 8
	numBackupCodes = 10
)

// TOTPConfig holds configuration for generating TOTP secrets
type TOTPConfig struct {
	Issuer string
	Period uint
	Digits otp.Digits
}

// NewDefaultTOTPConfig creates a default TOTP configuration
func NewDefaultTOTPConfig(issuer string) *TOTPConfig {
	return &TOTPConfig{
		Issuer: issuer,
		Period: totpPeriod,
		Digits: otp.Digits(totpDigits),
	}
}

// GenerateTOTPSecret generates a new TOTP secret for a user
func GenerateTOTPSecret(username string, config *TOTPConfig) (*otp.Key, error) {
	return totp.Generate(totp.GenerateOpts{
		Issuer:      config.Issuer,
		AccountName: username,
		Period:      config.Period,
		Digits:      config.Digits,
	})
}

// validateTOTP validates a TOTP code against a secret
func validateTOTP(secret, code string) bool {
	return totp.Validate(code, secret)
}

// GenerateBackupCodes generates a set of one-time use backup codes
func GenerateBackupCodes() ([]string, error) {
	codes := make([]string, numBackupCodes)
	for i := 0; i < numBackupCodes; i++ {
		bytes := make([]byte, backupCodeLen)
		if _, err := rand.Read(bytes); err != nil {
			return nil, fmt.Errorf("failed to generate backup code: %w", err)
		}
		codes[i] = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(bytes)[:backupCodeLen]
	}
	return codes, nil
}

// GetTOTPQRCode returns a QR code URL for the TOTP secret
func GetTOTPQRCode(key *otp.Key) string {
	return key.URL()
}
