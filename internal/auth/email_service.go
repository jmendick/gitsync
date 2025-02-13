package auth

// NoopEmailService is a no-op implementation of EmailService that doesn't actually send emails
type NoopEmailService struct{}

func NewNoopEmailService() *NoopEmailService {
	return &NoopEmailService{}
}

func (s *NoopEmailService) SendVerificationEmail(to, token string) error {
	// In a production environment, this would actually send an email
	return nil
}

func (s *NoopEmailService) SendPasswordResetEmail(to, token string) error {
	// In a production environment, this would actually send an email
	return nil
}
