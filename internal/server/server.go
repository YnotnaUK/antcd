package server

import (
	"crypto/subtle"
	"fmt"
	"log"
	"net/http"
	"strings"
)

type Server struct {
	port          int
	webhookSecret string
	syncSignal    chan struct{}
}

func NewServer(port int, webhookSecret string, syncSignal chan struct{}) *Server {
	return &Server{
		port:          port,
		webhookSecret: webhookSecret,
		syncSignal:    syncSignal,
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// K8s health check (unauthenticated)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Secured webhook sync
	mux.HandleFunc("/api/v1/sync", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Reject immediately if no secret is configured in AntCD
		if s.webhookSecret == "" {
			http.Error(w, "Webhook sync is disabled (no secret configured)", http.StatusForbidden)
			return
		}

		// Verify Bearer Token
		authHeader := r.Header.Get("Authorization")
		token := strings.TrimPrefix(authHeader, "Bearer ")

		// Constant-time compare to prevent timing attacks
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.webhookSecret)) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		select {
		case s.syncSignal <- struct{}{}:
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte("Sync triggered\n"))
		default:
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Sync already pending\n"))
		}
	})

	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("Web server listening on %s", addr)
	return http.ListenAndServe(addr, mux)
}
