package server

import (
	"fmt"
	"log"
	"net/http"
)

type Server struct {
	port       int
	syncSignal chan struct{}
}

func NewServer(port int, syncSignal chan struct{}) *Server {
	return &Server{
		port:       port,
		syncSignal: syncSignal,
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// K8s health check
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Webhook / Manual sync trigger
	mux.HandleFunc("/sync", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		select {
		case s.syncSignal <- struct{}{}:
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte("Sync triggered\n"))
		default:
			// If channel already has a pending sync, don't block
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Sync already pending\n"))
		}
	})

	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("Web server listening on %s", addr)
	return http.ListenAndServe(addr, mux)
}
