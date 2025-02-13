package p2p

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/jmendick/gitsync/internal/auth"
	"github.com/jmendick/gitsync/internal/config"
	"github.com/jmendick/gitsync/internal/git"
)

type Server struct {
	config       *config.Config
	authStore    auth.UserStore
	tokenMgr     *auth.TokenManager
	gitManager   *git.GitRepositoryManager
	mux          *http.ServeMux
	emailService auth.EmailService
	tlsConfig    *tls.Config
}

func NewServer(cfg *config.Config) (*Server, error) {
	userStorePath := filepath.Join(cfg.GetRepositoryDir(), "users.json")
	authStore, err := auth.NewFileUserStore(userStorePath)
	if err != nil {
		return nil, err
	}

	tokenMgr := auth.NewTokenManager(cfg.Auth.TokenSecret, authStore)
	gitManager := git.NewGitRepositoryManager(cfg.GetRepositoryDir(), authStore)
	emailService := auth.NewNoopEmailService()

	// Set up TLS configuration if enabled
	var tlsConfig *tls.Config
	if cfg.Security.AuthMode == "tls" {
		cert, err := tls.LoadX509KeyPair(cfg.Security.TLSCertFile, cfg.Security.TLSKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS certificates: %w", err)
		}

		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			MinVersion:   tls.VersionTLS12,
		}
	}

	srv := &Server{
		config:       cfg,
		authStore:    authStore,
		tokenMgr:     tokenMgr,
		gitManager:   gitManager,
		mux:          http.NewServeMux(),
		emailService: emailService,
		tlsConfig:    tlsConfig,
	}

	if err := srv.setupRoutes(); err != nil {
		return nil, err
	}
	return srv, nil
}

type RepositoryHandler struct {
	gitManager *git.GitRepositoryManager
	authStore  auth.UserStore
}

func (h *RepositoryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(auth.UserContextKey).(*auth.User)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case "POST":
		// Handle repository creation/cloning
		var req struct {
			Owner string `json:"owner"`
			Repo  string `json:"repo"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		// Clone repository and set up permissions
		if err := h.gitManager.Clone(r.Context(), user, req.Owner, req.Repo); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)

	case "GET":
		// Check repository access
		owner := r.URL.Query().Get("owner")
		repo := r.URL.Query().Get("repo")
		if owner == "" || repo == "" {
			http.Error(w, "Missing owner or repo parameter", http.StatusBadRequest)
			return
		}

		hasAccess, err := h.gitManager.CheckAccess(user, owner, repo)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if !hasAccess {
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

// PeerAuthMiddleware ensures proper peer authentication based on configured auth mode
func (s *Server) PeerAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch s.config.Security.AuthMode {
		case "shared_key":
			key := r.Header.Get("X-Peer-Key")
			if key == "" {
				http.Error(w, "Missing peer key", http.StatusUnauthorized)
				return
			}
			// Validate against trusted peer keys
			valid := false
			for _, trustedPeer := range s.config.Security.TrustedPeers {
				if key == trustedPeer {
					valid = true
					break
				}
			}
			if !valid {
				http.Error(w, "Invalid peer key", http.StatusUnauthorized)
				return
			}
		case "tls":
			// TLS client cert verification is handled by TLS config
			if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
				http.Error(w, "Client certificate required", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) setupRoutes() error {
	// Create auth handler with email service
	authHandler := auth.NewAuthHandler(s.authStore, s.tokenMgr, s.config, s.emailService)
	authHandler.RegisterRoutes(s.mux)

	// Repository handler with permission checks
	repoHandler := &RepositoryHandler{
		gitManager: s.gitManager,
		authStore:  s.authStore,
	}

	// Protected API routes with both user and peer authentication
	apiMux := http.NewServeMux()
	apiMux.Handle("/api/repos/", repoHandler)

	// Apply both peer and user authentication middleware
	protected := s.PeerAuthMiddleware(s.tokenMgr.AuthMiddleware(apiMux))
	s.mux.Handle("/api/", protected)
	return nil
}

func (s *Server) ListenAndServe(addr string) error {
	server := &http.Server{
		Addr:      addr,
		Handler:   s.mux,
		TLSConfig: s.tlsConfig,
	}

	if s.config.Security.AuthMode == "tls" {
		return server.ListenAndServeTLS(s.config.Security.TLSCertFile, s.config.Security.TLSKeyFile)
	}
	return server.ListenAndServe()
}
