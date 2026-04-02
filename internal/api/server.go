// Package api provides the HTTP server for rss2rm, including REST
// endpoints for feed, digest, and destination management, an SSE
// broker for real-time poll events, and background polling.
package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"rss2rm/internal/db"
	"rss2rm/internal/mailer"
	"rss2rm/internal/service"
)

// Broker manages Server-Sent Events client connections and broadcasts
// [service.PollEvent] values to all connected SSE clients.
type Broker struct {
	eventChan chan service.PollEvent
	add       chan chan service.PollEvent
	remove    chan chan service.PollEvent
}

func newBroker() *Broker {
	b := &Broker{
		eventChan: make(chan service.PollEvent, 100),
		add:       make(chan chan service.PollEvent),
		remove:    make(chan chan service.PollEvent),
	}
	go b.run()
	return b
}

func (b *Broker) run() {
	clients := make(map[chan service.PollEvent]bool)
	for {
		select {
		case c := <-b.add:
			clients[c] = true
		case c := <-b.remove:
			delete(clients, c)
			close(c)
		case e := <-b.eventChan:
			for c := range clients {
				select {
				case c <- e:
				default:
				}
			}
		}
	}
}

// Send broadcasts a poll event to all connected SSE clients.
func (b *Broker) Send(e service.PollEvent) {
	select {
	case b.eventChan <- e:
	default:
	}
}

// RegistrationMode controls who can register.
type RegistrationMode string

const (
	RegistrationOpen      RegistrationMode = "open"
	RegistrationClosed    RegistrationMode = "closed"
	RegistrationAllowlist RegistrationMode = "allowlist"
)

// errPollInProgress is returned when a poll is already running.
var errPollInProgress = errors.New("poll already in progress")

// ServerConfig holds configuration for the HTTP server.
type ServerConfig struct {
	EnablePolling         bool
	PollInterval          time.Duration
	WebDir                string
	RegistrationMode      RegistrationMode
	RegistrationAllowlist []string
	VerifyEmail           bool
	VerifyTimeout         time.Duration
	BaseURL               string // e.g., "https://feeds.example.org" for verification links
	SMTP                  mailer.Config
}

// Server is the rss2rm HTTP server.
type Server struct {
	svc        service.Service
	db         *db.DB
	config     ServerConfig
	mux        *http.ServeMux
	broker     *Broker
	pollMu     sync.Mutex
	pollActive bool
}

// NewServer creates a new [Server] with registered routes and an SSE
// broker. If EnablePolling is true, a background goroutine polls feeds
// at the given interval.
func NewServer(database *db.DB, svc service.Service, cfg ServerConfig) http.Handler {
	s := &Server{
		svc:    svc,
		db:     database,
		config: cfg,
		mux:    http.NewServeMux(),
		broker: newBroker(),
	}

	s.registerRoutes()

	if cfg.EnablePolling {
		go s.startBackgroundPoller()
	}
	go s.cleanExpiredSessions()
	if cfg.VerifyEmail {
		go s.cleanUnverifiedUsers()
	}

	return s.withMiddleware(s.mux)
}

func (s *Server) registerRoutes() {
	// Public routes (no auth required)
	s.mux.HandleFunc("POST /api/v1/auth/register", s.handleRegister)
	s.mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	s.mux.HandleFunc("GET /api/v1/auth/verify", s.handleVerifyEmail)
	s.mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := s.db.Ping(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			writeJSON(w, map[string]string{"status": "unhealthy", "error": err.Error()})
			return
		}
		w.Write([]byte(`{"status":"ok"}`))
	})
	// OAuth callback must be public (called by OAuth provider)
	s.mux.HandleFunc("GET /api/v1/oauth/callback", s.handleOAuthCallback)

	// Authenticated routes
	authed := http.NewServeMux()

	// Current user
	authed.HandleFunc("GET /api/v1/auth/me", s.handleGetCurrentUser)
	authed.HandleFunc("POST /api/v1/auth/change-password", s.handleChangePassword)

	// Feeds
	authed.HandleFunc("GET /api/v1/feeds", s.handleListFeeds)
	authed.HandleFunc("POST /api/v1/feeds", s.handleAddFeed)
	authed.HandleFunc("DELETE /api/v1/feeds", s.handleRemoveFeedByURL)
	authed.HandleFunc("DELETE /api/v1/feeds/{id}", s.handleRemoveFeed)
	authed.HandleFunc("PUT /api/v1/feeds/{id}", s.handleEditFeed)

	// Polling
	authed.HandleFunc("POST /api/v1/poll", s.handlePoll)
	authed.HandleFunc("GET /api/v1/poll/events", s.handlePollEvents)

	// Destinations
	authed.HandleFunc("GET /api/v1/destinations", s.handleListDestinations)
	authed.HandleFunc("GET /api/v1/destination-types", s.handleListDestinationTypes)
	authed.HandleFunc("POST /api/v1/destinations", s.handleAddDestination)
	authed.HandleFunc("DELETE /api/v1/destinations/{id}", s.handleRemoveDestination)
	authed.HandleFunc("PUT /api/v1/destinations/{id}", s.handleEditDestination)
	authed.HandleFunc("PUT /api/v1/destinations/{id}/default", s.handleSetDefaultDestination)
	authed.HandleFunc("POST /api/v1/destinations/{id}/test", s.handleTestDestination)
	authed.HandleFunc("GET /api/v1/destinations/{id}/auth-url", s.handleGetAuthURL)

	// Digests
	authed.HandleFunc("GET /api/v1/digests", s.handleListDigests)
	authed.HandleFunc("POST /api/v1/digests", s.handleAddDigest)
	authed.HandleFunc("DELETE /api/v1/digests/{id}", s.handleRemoveDigest)
	authed.HandleFunc("PUT /api/v1/digests/{id}", s.handleEditDigest)
	authed.HandleFunc("POST /api/v1/digests/{id}/generate", s.handleGenerateDigest)
	authed.HandleFunc("GET /api/v1/digests/{id}/feeds", s.handleListDigestFeeds)
	authed.HandleFunc("GET /api/v1/digests/{id}/pending", s.handleListDigestPending)
	authed.HandleFunc("POST /api/v1/digests/{id}/feeds", s.handleAddFeedToDigest)
	authed.HandleFunc("DELETE /api/v1/digests/{digestId}/feeds/{feedId}", s.handleRemoveFeedFromDigest)

	// Deliveries
	authed.HandleFunc("GET /api/v1/deliveries", s.handleListDeliveries)

	// Article ingest
	authed.HandleFunc("POST /api/v1/articles", s.handleIngestArticle)

	// Webhooks
	authed.HandleFunc("GET /api/v1/webhooks", s.handleListWebhooks)
	authed.HandleFunc("POST /api/v1/webhooks", s.handleAddWebhook)
	authed.HandleFunc("DELETE /api/v1/webhooks/{id}", s.handleRemoveWebhook)

	// Webhook receiver (HMAC-authenticated, not bearer-authenticated)
	// Must be registered before the /api/v1/ catch-all.
	s.mux.HandleFunc("POST /api/v1/webhook/miniflux", s.handleMinifluxWebhook)

	// Wrap all authenticated routes
	s.mux.Handle("/api/v1/", s.requireAuth(authed))

	// Static files
	if s.config.WebDir != "" {
		s.mux.Handle("/", http.FileServer(http.Dir(s.config.WebDir)))
	}
}

func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CORS
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Request logging (skip SSE to avoid spam)
		if r.URL.Path != "/api/v1/poll/events" {
			start := time.Now()
			defer func() {
				log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
			}()
		}

		next.ServeHTTP(w, r)
	})
}

// requireAuth is middleware that validates the session token from either
// the Authorization header (Bearer token) or the session cookie.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := s.extractToken(r)
		if token == "" {
			log.Printf("[Auth] Unauthorized request: path=%s ip=%s", r.URL.Path, r.RemoteAddr)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		session, err := s.db.GetSession(token)
		if err != nil || session == nil {
			log.Printf("[Auth] Invalid/expired token: path=%s ip=%s", r.URL.Path, r.RemoteAddr)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), service.UserIDKey, session.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) extractToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		if strings.HasPrefix(auth, "Bearer ") {
			return strings.TrimPrefix(auth, "Bearer ")
		}
	}
	if cookie, err := r.Cookie("session"); err == nil {
		return cookie.Value
	}
	return ""
}

// oauthRedirectURL builds the OAuth callback URL from the request,
// detecting HTTPS from TLS or the X-Forwarded-Proto header.
func oauthRedirectURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/api/v1/oauth/callback", scheme, r.Host)
}

// requirePathValue extracts a path parameter and writes a 400 response
// if it is empty. Returns the value and true on success.
func requirePathValue(w http.ResponseWriter, r *http.Request, key string) (string, bool) {
	v := r.PathValue(key)
	if v == "" {
		http.Error(w, key+" required", http.StatusBadRequest)
		return "", false
	}
	return v, true
}

// decodeJSON reads JSON from the request body into v. Returns false
// and writes a 400 response if decoding fails.
func decodeJSON(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return false
	}
	return true
}

// writeJSON encodes v as JSON to w with the correct Content-Type header.
func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	// Check registration policy
	switch s.config.RegistrationMode {
	case RegistrationClosed:
		http.Error(w, "registration is disabled", http.StatusForbidden)
		return
	case RegistrationAllowlist:
		// Decode first to get email, then check allowlist
	case RegistrationOpen:
		// Allow
	}

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Email == "" || req.Password == "" {
		http.Error(w, "email and password are required", http.StatusBadRequest)
		return
	}
	if len(req.Password) < 8 {
		http.Error(w, "password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	// Check allowlist after decoding
	if s.config.RegistrationMode == RegistrationAllowlist {
		allowed := false
		for _, email := range s.config.RegistrationAllowlist {
			if strings.EqualFold(email, req.Email) {
				allowed = true
				break
			}
		}
		if !allowed {
			log.Printf("[Auth] Registration rejected (not in allowlist): email=%s ip=%s", req.Email, r.RemoteAddr)
			http.Error(w, "registration is not available for this email", http.StatusForbidden)
			return
		}
	}

	// Create user — verified or unverified depending on config
	if s.config.VerifyEmail && s.config.SMTP.IsConfigured() {
		id, token, err := s.db.CreateUnverifiedUser(req.Email, req.Password, s.config.VerifyTimeout)
		if err != nil {
			if errors.Is(err, db.ErrAlreadyExists) {
				log.Printf("[Auth] Registration failed (duplicate): email=%s ip=%s", req.Email, r.RemoteAddr)
				http.Error(w, "email already registered", http.StatusConflict)
				return
			}
			log.Printf("[Auth] Registration failed: email=%s error=%v", req.Email, err)
			http.Error(w, "registration failed", http.StatusInternalServerError)
			return
		}

		// Send verification email
		if err := mailer.SendVerification(s.config.SMTP, req.Email, token, s.config.BaseURL); err != nil {
			log.Printf("[Auth] Verification email failed: email=%s error=%v", req.Email, err)
			// User was created but email failed — delete user to avoid orphan
			s.db.DeleteUser(id)
			http.Error(w, "failed to send verification email", http.StatusInternalServerError)
			return
		}

		log.Printf("[Auth] User registered (pending verification): email=%s id=%s ip=%s", req.Email, id, r.RemoteAddr)
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, map[string]interface{}{
			"status":  "verification_required",
			"message": "Check your email for a verification link.",
		})
		return
	}

	id, err := s.db.CreateUser(req.Email, req.Password)
	if err != nil {
		if errors.Is(err, db.ErrAlreadyExists) {
			log.Printf("[Auth] Registration failed (duplicate): email=%s ip=%s", req.Email, r.RemoteAddr)
			http.Error(w, "email already registered", http.StatusConflict)
			return
		}
		log.Printf("[Auth] Registration failed: email=%s error=%v", req.Email, err)
		http.Error(w, "registration failed", http.StatusInternalServerError)
		return
	}

	log.Printf("[Auth] User registered: email=%s id=%s ip=%s", req.Email, id, r.RemoteAddr)

	// Auto-login after registration
	session, err := s.db.CreateSession(id)
	if err != nil {
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, map[string]interface{}{"status": "created", "id": id})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    session.Token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 60 * 60,
	})

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]interface{}{
		"status": "created",
		"token":  session.Token,
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	user, err := s.db.GetUserByEmail(req.Email)
	if err != nil || user == nil || !db.CheckPassword(user, req.Password) {
		log.Printf("[Auth] Login failed: email=%s ip=%s", req.Email, r.RemoteAddr)
		http.Error(w, "invalid email or password", http.StatusUnauthorized)
		return
	}

	// Check verification status when email verification is enabled
	if s.config.VerifyEmail && !user.Verified {
		log.Printf("[Auth] Login rejected (unverified): email=%s ip=%s", req.Email, r.RemoteAddr)
		http.Error(w, "email not verified — check your inbox for the verification link", http.StatusForbidden)
		return
	}

	session, err := s.db.CreateSession(user.ID)
	if err != nil {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	log.Printf("[Auth] Login success: email=%s user_id=%s ip=%s", req.Email, user.ID, r.RemoteAddr)

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    session.Token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 60 * 60,
	})

	writeJSON(w, map[string]interface{}{
		"status": "ok",
		"token":  session.Token,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := s.extractToken(r)
	if token != "" {
		// Log before deleting so we can still resolve user
		if session, _ := s.db.GetSession(token); session != nil {
			log.Printf("[Auth] Logout: user_id=%s ip=%s", session.UserID, r.RemoteAddr)
		}
		s.db.DeleteSession(token)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	userID := service.UserIDFromContext(r.Context())
	user, err := s.db.GetUserByID(userID)
	if err != nil || user == nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]interface{}{
		"id":    user.ID,
		"email": user.Email,
	})
}

func (s *Server) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "verification token required", http.StatusBadRequest)
		return
	}

	userID, err := s.db.VerifyUserByToken(token)
	if err != nil {
		http.Error(w, "verification failed", http.StatusInternalServerError)
		return
	}
	if userID == "" {
		http.Error(w, "invalid or expired verification link", http.StatusBadRequest)
		return
	}

	log.Printf("[Auth] Email verified: user_id=%s", userID)
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<!DOCTYPE html><html><body><h2>Email verified.</h2><p>You can now <a href="/">log in</a>.</p></body></html>`)
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	userID := service.UserIDFromContext(r.Context())

	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.NewPassword == "" || len(req.NewPassword) < 8 {
		http.Error(w, "new password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	user, err := s.db.GetUserByID(userID)
	if err != nil || user == nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if !db.CheckPassword(user, req.CurrentPassword) {
		log.Printf("[Auth] Password change failed (wrong current password): user_id=%s ip=%s", userID, r.RemoteAddr)
		http.Error(w, "current password is incorrect", http.StatusUnauthorized)
		return
	}

	if err := s.db.UpdateUserPassword(userID, req.NewPassword); err != nil {
		http.Error(w, "failed to update password", http.StatusInternalServerError)
		return
	}

	log.Printf("[Auth] Password changed: user_id=%s ip=%s", userID, r.RemoteAddr)
	w.WriteHeader(http.StatusOK)
	writeJSON(w, map[string]string{"status": "password_changed"})
}

func (s *Server) handleListFeeds(w http.ResponseWriter, r *http.Request) {
	feeds, err := s.svc.ListFeeds(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type feedResponse struct {
		ID                  string    `json:"id"`
		URL                 string    `json:"url"`
		Name                string    `json:"name"`
		Directory           string    `json:"directory"`
		LastPolled          time.Time `json:"last_polled"`
		Active              bool      `json:"active"`
		Backfill            int       `json:"backfill"`
		DeliverIndividually bool      `json:"deliver_individually"`
		Retain              int       `json:"retain"`
		DigestNames         []string  `json:"digest_names,omitempty"`
	}

	var resp []feedResponse
	for _, f := range feeds {
		fr := feedResponse{
			ID: f.ID, URL: f.URL, Name: f.Name,
			LastPolled: f.LastPolled, Active: f.Active, Backfill: f.Backfill,
		}

		// Get individual delivery info
		fd, _ := s.svc.GetFeedDelivery(r.Context(), f.ID)
		if fd != nil {
			fr.DeliverIndividually = true
			fr.Directory = fd.Directory
			fr.Retain = fd.Retain
		}

		// Get digest memberships
		digests, _ := s.svc.GetDigestsForFeed(r.Context(), f.ID)
		for _, d := range digests {
			fr.DigestNames = append(fr.DigestNames, d.Name)
		}

		resp = append(resp, fr)
	}

	writeJSON(w, resp)
}

func (s *Server) handleListDestinations(w http.ResponseWriter, r *http.Request) {
	dests, err := s.svc.ListDestinations(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, dests)
}

func (s *Server) handleListDestinationTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, service.RegisteredDestinationTypes())
}

func (s *Server) handleAddDestination(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string            `json:"name"`
		Type      string            `json:"type"`
		Config    map[string]string `json:"config"`
		IsDefault bool              `json:"is_default"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	// Init via factory to validate and process config
	factory, ok := service.GetDestinationFactory(req.Type)
	if !ok {
		http.Error(w, "Unknown destination type", http.StatusBadRequest)
		return
	}

	tempDest := factory(nil)
	finalConfig, err := tempDest.Init(r.Context(), req.Config)
	if err != nil {
		http.Error(w, fmt.Sprintf("Initialization failed: %v", err), http.StatusBadRequest)
		return
	}

	id, err := s.svc.AddDestination(r.Context(), req.Type, req.Name, finalConfig, req.IsDefault)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]interface{}{"status": "created", "id": id})
}

func (s *Server) handleRemoveDestination(w http.ResponseWriter, r *http.Request) {
	id, ok := requirePathValue(w, r, "id")
	if !ok {
		return
	}
	if err := s.svc.RemoveDestination(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleEditDestination(w http.ResponseWriter, r *http.Request) {
	id, ok := requirePathValue(w, r, "id")
	if !ok {
		return
	}

	var req struct {
		Name   string            `json:"name"`
		Config map[string]string `json:"config"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	// Fetch existing to handle partial updates
	d, err := s.svc.GetDestinationByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if d == nil {
		http.Error(w, "Destination not found", http.StatusNotFound)
		return
	}

	name := req.Name
	if name == "" {
		name = d.Name
	}

	var finalConfig map[string]string
	if req.Config != nil {
		finalConfig = req.Config
	} else {
		if d.Config != "" {
			json.Unmarshal([]byte(d.Config), &finalConfig)
		}
	}

	if err := s.svc.UpdateDestination(r.Context(), id, name, finalConfig); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleSetDefaultDestination(w http.ResponseWriter, r *http.Request) {
	id, ok := requirePathValue(w, r, "id")
	if !ok {
		return
	}
	if err := s.svc.SetDefaultDestination(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleTestDestination(w http.ResponseWriter, r *http.Request) {
	id, ok := requirePathValue(w, r, "id")
	if !ok {
		return
	}
	if err := s.svc.TestDestination(r.Context(), id); err != nil {
		http.Error(w, fmt.Sprintf("Connection test failed: %v", err), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusOK)
	writeJSON(w, map[string]string{"status": "ok", "message": "Connection successful"})
}

func (s *Server) handleGetAuthURL(w http.ResponseWriter, r *http.Request) {
	id, ok := requirePathValue(w, r, "id")
	if !ok {
		return
	}

	// Get destination record
	dest, err := s.svc.GetDestinationByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if dest == nil {
		http.Error(w, "Destination not found", http.StatusNotFound)
		return
	}

	// Create destination instance and check if it supports OAuth
	destInstance, err := service.CreateDestinationInstance(dest.Type, dest.Config)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create destination: %v", err), http.StatusInternalServerError)
		return
	}

	oauthDest, ok := destInstance.(service.OAuthDestination)
	if !ok {
		http.Error(w, "OAuth not supported for this destination type", http.StatusBadRequest)
		return
	}

	// State parameter contains destination ID
	state := id
	authURL := oauthDest.GetAuthURL(oauthRedirectURL(r), state)

	writeJSON(w, map[string]string{"auth_url": authURL})
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" {
		// Check for error
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			http.Error(w, fmt.Sprintf("OAuth error: %s", errMsg), http.StatusBadRequest)
			return
		}
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}

	if state == "" {
		http.Error(w, "Missing state parameter", http.StatusBadRequest)
		return
	}

	// Parse destination ID from state
	destID := state
	if destID == "" {
		http.Error(w, "Invalid state parameter", http.StatusBadRequest)
		return
	}

	// Get destination record
	dest, err := s.svc.GetDestinationByID(r.Context(), destID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if dest == nil {
		http.Error(w, "Destination not found", http.StatusNotFound)
		return
	}

	// Create destination instance and check if it supports OAuth
	destInstance, err := service.CreateDestinationInstance(dest.Type, dest.Config)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create destination: %v", err), http.StatusInternalServerError)
		return
	}

	oauthDest, ok := destInstance.(service.OAuthDestination)
	if !ok {
		http.Error(w, "OAuth not supported for this destination type", http.StatusBadRequest)
		return
	}

	// Exchange code for tokens via the destination
	if err := oauthDest.ExchangeCode(r.Context(), oauthRedirectURL(r), code); err != nil {
		http.Error(w, fmt.Sprintf("Token exchange failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Get updated config and persist it
	if updater, ok := destInstance.(service.ConfigUpdater); ok {
		if newConfig := updater.GetUpdatedConfig(); newConfig != nil {
			if err := s.svc.UpdateDestinationConfig(r.Context(), destID, newConfig); err != nil {
				http.Error(w, fmt.Sprintf("Failed to save tokens: %v", err), http.StatusInternalServerError)
				return
			}
		}
	}

	// Redirect to success page or return HTML
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>Authorization Successful</title></head>
<body>
<h1>Authorization Successful</h1>
<p>Your destination has been authorized. You can close this window.</p>
<script>
if (window.opener) {
	window.opener.postMessage({type: 'oauth_complete', success: true}, '*');
	setTimeout(() => window.close(), 2000);
}
</script>
</body>
</html>`))
}

func (s *Server) handleAddFeed(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL                 string `json:"url"`
		Name                string `json:"name"`
		Directory           string `json:"directory"`
		Backfill            int    `json:"backfill"`
		DeliverIndividually *bool  `json:"deliver_individually,omitempty"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.URL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	feed := db.Feed{
		URL:      req.URL,
		Name:     req.Name,
		Backfill: req.Backfill,
	}

	if err := s.svc.AddFeed(r.Context(), feed); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Find the feed we just added
	feeds, _ := s.svc.ListFeeds(r.Context())
	for _, f := range feeds {
		if service.NormalizeURL(f.URL) == service.NormalizeURL(req.URL) {
			// Update directory if provided
			if req.Directory != "" {
				fd, _ := s.svc.GetFeedDelivery(r.Context(), f.ID)
				if fd != nil {
					fd.Directory = req.Directory
					s.svc.SetFeedDelivery(r.Context(), *fd)
				}
			}
			// Remove individual delivery if explicitly disabled
			if req.DeliverIndividually != nil && !*req.DeliverIndividually {
				s.svc.RemoveFeedDelivery(r.Context(), f.ID)
			}
			break
		}
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]string{"status": "created"})
}

func (s *Server) handleEditFeed(w http.ResponseWriter, r *http.Request) {
	id, ok := requirePathValue(w, r, "id")
	if !ok {
		return
	}

	var req struct {
		Name                string `json:"name"`
		Directory           string `json:"directory"`
		DeliverIndividually *bool  `json:"deliver_individually,omitempty"`
		Retain              *int   `json:"retain,omitempty"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	// Update feed name if provided
	if req.Name != "" {
		feeds, err := s.svc.ListFeeds(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, f := range feeds {
			if f.ID == id {
				f.Name = req.Name
				s.svc.UpdateFeed(r.Context(), f)
				break
			}
		}
	}

	// Update feed_delivery
	fd, err := s.svc.GetFeedDelivery(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Handle deliver_individually toggle
	if req.DeliverIndividually != nil {
		if *req.DeliverIndividually {
			// Enable individual delivery
			if fd == nil {
				newFD := db.FeedDelivery{FeedID: id, Directory: req.Directory}
				if req.Retain != nil {
					newFD.Retain = *req.Retain
				}
				if err := s.svc.SetFeedDelivery(r.Context(), newFD); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			} else {
				fd.Directory = req.Directory
				if req.Retain != nil {
					fd.Retain = *req.Retain
				}
				if err := s.svc.SetFeedDelivery(r.Context(), *fd); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
		} else {
			// Disable individual delivery
			if fd != nil {
				if err := s.svc.RemoveFeedDelivery(r.Context(), id); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
		}
	} else if fd != nil {
		// No toggle requested, just update directory
		fd.Directory = req.Directory
		if req.Retain != nil {
			fd.Retain = *req.Retain
		}
		if err := s.svc.SetFeedDelivery(r.Context(), *fd); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleRemoveFeedByURL(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	if url == "" {
		http.Error(w, "url query parameter required", http.StatusBadRequest)
		return
	}
	if err := s.svc.RemoveFeed(r.Context(), url); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRemoveFeed(w http.ResponseWriter, r *http.Request) {
	id, ok := requirePathValue(w, r, "id")
	if !ok {
		return
	}
	if err := s.svc.RemoveFeedByID(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) executePoll(ctx context.Context, urls []string) error {
	s.pollMu.Lock()
	if s.pollActive {
		s.pollMu.Unlock()
		return errPollInProgress
	}
	s.pollActive = true
	s.pollMu.Unlock()

	defer func() {
		s.pollMu.Lock()
		s.pollActive = false
		s.pollMu.Unlock()
	}()

	users, err := s.db.GetAllUsers()
	if err != nil {
		return fmt.Errorf("failed to get users: %w", err)
	}

	for _, u := range users {
		userCtx := context.WithValue(ctx, service.UserIDKey, u.ID)
		if err := s.svc.PollFeeds(userCtx, urls, func(e service.PollEvent) {
			s.broker.Send(e)
		}); err != nil {
			log.Printf("Poll error for user %s: %v", u.Email, err)
		}
	}

	// Send a global "poll complete" event so clients know to disconnect
	s.broker.Send(service.PollEvent{Type: service.EventPollComplete, Message: "All feeds processed"})

	return nil
}

func (s *Server) handlePoll(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URLs     []string `json:"urls"`
		Backfill int      `json:"backfill"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Check if poll is already running before spawning goroutine
	s.pollMu.Lock()
	if s.pollActive {
		s.pollMu.Unlock()
		http.Error(w, "Poll already in progress", http.StatusConflict)
		return
	}
	s.pollMu.Unlock()

	go func() {
		log.Println("Starting manual poll via API...")
		userCtx := context.WithValue(context.Background(), service.UserIDKey, service.UserIDFromContext(r.Context()))
		userCtx = service.WithPollOptions(userCtx, service.PollOptions{
			BackfillLimit: req.Backfill,
		})

		s.pollMu.Lock()
		if s.pollActive {
			s.pollMu.Unlock()
			log.Println("Manual poll skipped: poll already in progress")
			return
		}
		s.pollActive = true
		s.pollMu.Unlock()

		defer func() {
			s.pollMu.Lock()
			s.pollActive = false
			s.pollMu.Unlock()
		}()

		if err := s.svc.PollFeeds(userCtx, req.URLs, func(e service.PollEvent) {
			s.broker.Send(e)
		}); err != nil {
			log.Printf("Manual poll error: %v", err)
		} else {
			log.Println("Manual poll complete")
		}
		s.broker.Send(service.PollEvent{Type: service.EventPollComplete, Message: "All feeds processed"})
	}()

	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]string{"status": "polling_started"})
}

func (s *Server) handlePollEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	clientChan := make(chan service.PollEvent, 10)
	s.broker.add <- clientChan
	defer func() { s.broker.remove <- clientChan }()

	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case e := <-clientChan:
			data, _ := json.Marshal(e)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func (s *Server) startBackgroundPoller() {
	log.Printf("Background polling enabled (interval: %v)", s.config.PollInterval)

	// Initial delay
	log.Println("Waiting 20s before initial poll...")
	time.Sleep(20 * time.Second)

	// Run initial poll
	s.runBackgroundPoll()
	s.checkDigests()

	for {
		log.Printf("Next poll in %v", s.config.PollInterval)
		time.Sleep(s.config.PollInterval)
		s.runBackgroundPoll()
		s.checkDigests()
	}
}

func (s *Server) runBackgroundPoll() {
	log.Println("Starting background poll...")
	if err := s.executePoll(context.Background(), nil); err != nil {
		if errors.Is(err, errPollInProgress) {
			log.Println("Background poll skipped: poll already in progress")
		} else {
			log.Printf("Background poll error: %v", err)
		}
	} else {
		log.Println("Background poll complete")
	}
}

// cleanExpiredSessions periodically removes expired sessions from the database.
func (s *Server) cleanExpiredSessions() {
	for {
		time.Sleep(1 * time.Hour)
		if err := s.db.CleanExpiredSessions(); err != nil {
			log.Printf("Failed to clean expired sessions: %v", err)
		}
	}
}

// cleanUnverifiedUsers periodically removes unverified users whose verification has expired.
func (s *Server) cleanUnverifiedUsers() {
	for {
		time.Sleep(1 * time.Hour)
		deleted, err := s.db.DeleteExpiredUnverifiedUsers()
		if err != nil {
			log.Printf("[Auth] Failed to clean unverified users: %v", err)
		} else if deleted > 0 {
			log.Printf("[Auth] Cleaned %d expired unverified accounts", deleted)
		}
	}
}

// checkDigests generates any digests that are due based on their schedule.
func (s *Server) checkDigests() {
	users, err := s.db.GetAllUsers()
	if err != nil {
		log.Printf("Failed to get users for digest check: %v", err)
		return
	}

	now := time.Now()
	for _, u := range users {
		ctx := context.WithValue(context.Background(), service.UserIDKey, u.ID)
		digests, err := s.svc.ListDigests(ctx)
		if err != nil {
			log.Printf("Failed to list digests for user %s: %v", u.Email, err)
			continue
		}
		for _, d := range digests {
			if !d.Active || !isDigestDue(d, now) {
				continue
			}
			log.Printf("[Digest] Generating scheduled digest %q for user %s", d.Name, u.Email)
			err := s.svc.GenerateDigest(ctx, d.ID, func(e service.PollEvent) {
				s.broker.Send(e)
			})
			if err != nil {
				log.Printf("[Digest] Generation failed for %q: %v", d.Name, err)
			} else {
				log.Printf("[Digest] Generation complete for %q", d.Name)
			}
		}
	}
}

// isDigestDue returns true if the digest should be generated now.
// Schedule format is "HH:MM" (daily). A digest is due if:
// - It has never been generated, OR
// - It was last generated before today's scheduled time and we are past that time.
func isDigestDue(d db.Digest, now time.Time) bool {
	if d.Schedule == "" {
		return false
	}

	schedHour, schedMin, err := parseSchedule(d.Schedule)
	if err != nil {
		log.Printf("[Digest] Invalid schedule '%s' for digest '%s': %v", d.Schedule, d.Name, err)
		return false
	}

	// Today's scheduled time in local timezone
	scheduledTime := time.Date(now.Year(), now.Month(), now.Day(), schedHour, schedMin, 0, 0, now.Location())

	// Not yet past scheduled time today
	if now.Before(scheduledTime) {
		return false
	}

	// Never generated
	if d.LastGenerated.IsZero() {
		return true
	}

	// Last generated before today's scheduled time
	return d.LastGenerated.Before(scheduledTime)
}

// parseSchedule parses "HH:MM" format. Returns hour and minute.
func parseSchedule(schedule string) (int, int, error) {
	var hour, min int
	_, err := fmt.Sscanf(schedule, "%d:%d", &hour, &min)
	if err != nil {
		return 0, 0, fmt.Errorf("expected HH:MM format: %w", err)
	}
	if hour < 0 || hour > 23 || min < 0 || min > 59 {
		return 0, 0, fmt.Errorf("invalid time: %02d:%02d", hour, min)
	}
	return hour, min, nil
}

func (s *Server) handleListDigests(w http.ResponseWriter, r *http.Request) {
	digests, err := s.svc.ListDigests(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, digests)
}

func (s *Server) handleAddDigest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string  `json:"name"`
		Schedule      string  `json:"schedule"`
		DestinationID *string `json:"destination_id"`
		Retain        int     `json:"retain"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}
	if req.Schedule == "" {
		req.Schedule = "07:00"
	}

	digest := db.Digest{
		Name:          req.Name,
		Schedule:      req.Schedule,
		DestinationID: req.DestinationID,
		Retain:        req.Retain,
	}

	id, err := s.svc.AddDigest(r.Context(), digest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]interface{}{"status": "created", "id": id})
}

func (s *Server) handleEditDigest(w http.ResponseWriter, r *http.Request) {
	id, ok := requirePathValue(w, r, "id")
	if !ok {
		return
	}

	var req struct {
		Name      string `json:"name"`
		Schedule  string `json:"schedule"`
		Directory string `json:"directory"`
		Retain    *int   `json:"retain,omitempty"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	digests, err := s.svc.ListDigests(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var digest *db.Digest
	for i := range digests {
		if digests[i].ID == id {
			digest = &digests[i]
			break
		}
	}
	if digest == nil {
		http.Error(w, "Digest not found", http.StatusNotFound)
		return
	}

	if req.Name != "" {
		digest.Name = req.Name
	}
	if req.Schedule != "" {
		digest.Schedule = req.Schedule
	}
	digest.Directory = req.Directory // Allow clearing
	if req.Retain != nil {
		digest.Retain = *req.Retain
	}

	if err := s.svc.UpdateDigest(r.Context(), *digest); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleRemoveDigest(w http.ResponseWriter, r *http.Request) {
	id, ok := requirePathValue(w, r, "id")
	if !ok {
		return
	}
	if err := s.svc.RemoveDigest(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListDigestFeeds(w http.ResponseWriter, r *http.Request) {
	id, ok := requirePathValue(w, r, "id")
	if !ok {
		return
	}
	feeds, err := s.svc.ListDigestFeeds(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, feeds)
}

// handleListDigestPending returns entries queued for the next digest generation.
func (s *Server) handleListDigestPending(w http.ResponseWriter, r *http.Request) {
	id, ok := requirePathValue(w, r, "id")
	if !ok {
		return
	}
	digest, err := s.svc.GetDigestByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if digest == nil {
		http.Error(w, "digest not found", http.StatusNotFound)
		return
	}
	entries, err := s.svc.GetNewEntriesForDigest(r.Context(), id, digest.LastDeliveredID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type pendingEntry struct {
		Title     string `json:"title"`
		URL       string `json:"url"`
		Published string `json:"published"`
	}
	resp := make([]pendingEntry, 0, len(entries))
	for _, e := range entries {
		resp = append(resp, pendingEntry{
			Title:     e.Title,
			URL:       e.URL,
			Published: e.Published.Format("2006-01-02T15:04:05Z"),
		})
	}
	writeJSON(w, resp)
}

func (s *Server) handleAddFeedToDigest(w http.ResponseWriter, r *http.Request) {
	digestID, ok := requirePathValue(w, r, "id")
	if !ok {
		return
	}

	var req struct {
		FeedID         string `json:"feed_id"`
		AlsoIndividual bool   `json:"also_individual"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.FeedID == "" {
		http.Error(w, "feed_id is required", http.StatusBadRequest)
		return
	}

	if err := s.svc.AddFeedToDigest(r.Context(), digestID, req.FeedID, req.AlsoIndividual); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleRemoveFeedFromDigest(w http.ResponseWriter, r *http.Request) {
	digestID, ok := requirePathValue(w, r, "digestId")
	if !ok {
		return
	}
	feedID, ok := requirePathValue(w, r, "feedId")
	if !ok {
		return
	}
	if err := s.svc.RemoveFeedFromDigest(r.Context(), digestID, feedID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGenerateDigest(w http.ResponseWriter, r *http.Request) {
	id, ok := requirePathValue(w, r, "id")
	if !ok {
		return
	}

	var events []service.PollEvent
	err := s.svc.GenerateDigest(r.Context(), id, func(e service.PollEvent) {
		events = append(events, e)
		// Also broadcast to SSE clients
		s.broker.Send(e)
	})

	if err != nil {
		http.Error(w, fmt.Sprintf("Digest generation failed: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{
		"status": "ok",
		"events": events,
	})
}

func (s *Server) handleListDeliveries(w http.ResponseWriter, r *http.Request) {
	deliveries, err := s.svc.ListRecentDeliveries(r.Context(), 25)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type deliveryResponse struct {
		ID           int64  `json:"id"`
		DeliveryType string `json:"delivery_type"`
		Title        string `json:"title"`
		FeedName     string `json:"feed_name,omitempty"`
		URL          string `json:"url,omitempty"`
		DestName     string `json:"dest_name"`
		DestType     string `json:"dest_type"`
		DeliveredAt  string `json:"delivered_at"`
	}

	resp := make([]deliveryResponse, 0, len(deliveries))
	for _, d := range deliveries {
		resp = append(resp, deliveryResponse{
			ID:           d.ID,
			DeliveryType: d.DeliveryType,
			Title:        d.Title,
			FeedName:     d.FeedName,
			URL:          d.URL,
			DestName:     d.DestName,
			DestType:     d.DestType,
			DeliveredAt:  d.DeliveredAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	writeJSON(w, resp)
}

// handleIngestArticle accepts an article for immediate delivery or digest inclusion.
func (s *Server) handleIngestArticle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title         string `json:"title"`
		URL           string `json:"url"`
		Content       string `json:"content"`
		DestinationID string `json:"destination_id"`
		Directory     string `json:"directory"`
		DigestID      string `json:"digest_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.URL == "" && req.Content == "" {
		http.Error(w, "url or content is required", http.StatusBadRequest)
		return
	}
	if req.Title == "" && req.Content == "" {
		http.Error(w, "title is required when content is not provided", http.StatusBadRequest)
		return
	}

	if err := s.svc.DeliverArticle(r.Context(), req.Title, req.URL, req.Content, req.DestinationID, req.Directory, req.DigestID); err != nil {
		log.Printf("[Ingest] Failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if req.DigestID != "" {
		writeJSON(w, map[string]string{"status": "queued", "title": req.Title, "digest_id": req.DigestID})
	} else {
		writeJSON(w, map[string]string{"status": "delivered", "title": req.Title})
	}
}

// --- Webhook CRUD ---

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	webhooks, err := s.svc.ListWebhooks(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if webhooks == nil {
		webhooks = []db.Webhook{}
	}
	writeJSON(w, webhooks)
}

func (s *Server) handleAddWebhook(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type   string `json:"type"`
		Secret string `json:"secret"`
		Config string `json:"config"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Type == "" {
		http.Error(w, "type is required", http.StatusBadRequest)
		return
	}
	if req.Type != "miniflux" {
		http.Error(w, "unsupported webhook type (supported: miniflux)", http.StatusBadRequest)
		return
	}
	if req.Secret == "" {
		http.Error(w, "secret is required (copy from Miniflux Settings > Integrations > Webhook)", http.StatusBadRequest)
		return
	}

	id, err := s.svc.AddWebhook(r.Context(), req.Type, req.Secret, req.Config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return the created webhook (including generated secret)
	webhook, _ := s.svc.GetWebhookByID(r.Context(), id)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, webhook)
}

func (s *Server) handleRemoveWebhook(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.svc.RemoveWebhook(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Miniflux webhook receiver ---

// handleMinifluxWebhook receives webhook events from Miniflux.
// Authentication is via HMAC-SHA256 signature in the X-Miniflux-Signature header.
func (s *Server) handleMinifluxWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	signature := r.Header.Get("X-Miniflux-Signature")
	if signature == "" {
		log.Printf("[Webhook] Rejected: missing X-Miniflux-Signature header")
		http.Error(w, "missing X-Miniflux-Signature header", http.StatusUnauthorized)
		return
	}

	// Find the webhook by validating the HMAC against all active miniflux webhooks
	webhook, err := s.findWebhookBySignature(body, signature)
	if err != nil || webhook == nil {
		log.Printf("[Webhook] Rejected: HMAC signature mismatch (sig=%s, err=%v, bodyLen=%d)", signature[:16], err, len(body))
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	eventType := r.Header.Get("X-Miniflux-Event-Type")
	if eventType != "save_entry" {
		// Acknowledge other events without processing
		w.WriteHeader(http.StatusOK)
		return
	}

	var payload struct {
		Entry struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	// Parse webhook config for optional digest_id and directory
	var config struct {
		DigestID      string `json:"digest_id"`
		Directory     string `json:"directory"`
		DestinationID string `json:"destination_id"`
	}
	if webhook.Config != "" {
		json.Unmarshal([]byte(webhook.Config), &config)
	}

	// Deliver as the webhook's user
	ctx := context.WithValue(r.Context(), service.UserIDKey, webhook.UserID)
	if err := s.svc.DeliverArticle(ctx, payload.Entry.Title, payload.Entry.URL, payload.Entry.Content, config.DestinationID, config.Directory, config.DigestID); err != nil {
		log.Printf("[Webhook] Miniflux delivery failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("[Webhook] Miniflux: delivered %q for user %s", payload.Entry.Title, webhook.UserID)
	w.WriteHeader(http.StatusOK)
}

// findWebhookBySignature validates an HMAC-SHA256 signature against all
// active miniflux webhooks. Returns the matching webhook or nil.
// Uses s.db directly because this runs on the unauthenticated webhook
// path — there is no user context. The query is cross-tenant by design.
func (s *Server) findWebhookBySignature(body []byte, signature string) (*db.Webhook, error) {
	sigBytes, err := hex.DecodeString(signature)
	if err != nil {
		return nil, fmt.Errorf("invalid signature encoding")
	}

	webhooks, err := s.db.GetActiveWebhooksByType("miniflux")
	if err != nil {
		return nil, err
	}

	log.Printf("[Webhook] Checking signature against %d active webhook(s)", len(webhooks))

	for _, w := range webhooks {
		mac := hmac.New(sha256.New, []byte(w.Secret))
		mac.Write(body)
		expected := mac.Sum(nil)
		expectedHex := hex.EncodeToString(expected)
		log.Printf("[Webhook] Comparing: got=%s expected=%s (secret=%s...)", signature[:16], expectedHex[:16], w.Secret[:8])
		if hmac.Equal(expected, sigBytes) {
			return &w, nil
		}
	}

	return nil, nil
}
