package dashboard

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//go:embed ui/*
var uiFS embed.FS

// ─────────────────────────────────────────────────────────────
//  Server
// ─────────────────────────────────────────────────────────────

// Server is the embedded HTTP dashboard server.
// It serves a static single-page app and a JSON API.
type Server struct {
	collector *Collector
	token     string
	mux       *http.ServeMux
}

// NewServer creates a Server backed by the given Collector.
func NewServer(c *Collector, token string) *Server {
	s := &Server{collector: c, token: token, mux: http.NewServeMux()}
	s.registerRoutes()
	return s
}

// Start starts the HTTP server on port in a background goroutine.
func (s *Server) Start(port int) error {
	addr := fmt.Sprintf(":%d", port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      s.mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	go func() {
		log.Printf("[godb dashboard] Listening at http://localhost%s  (token required)", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[godb dashboard] server error: %v", err)
		}
	}()
	return nil
}

// ─────────────────────────────────────────────────────────────
//  Route registration
// ─────────────────────────────────────────────────────────────

func (s *Server) registerRoutes() {
	// Static UI (embedded)
	s.mux.Handle("/", http.FileServer(http.FS(uiFS)))

	// Public health probe (no auth)
	s.mux.HandleFunc("/health", s.handleHealth)

	// API (auth required)
	s.mux.HandleFunc("/api/queries/recent", s.auth(s.handleRecentQueries))
	s.mux.HandleFunc("/api/queries/slow", s.auth(s.handleSlowQueries))
	s.mux.HandleFunc("/api/queries/top", s.auth(s.handleTopQueries))
	s.mux.HandleFunc("/api/queries/errors", s.auth(s.handleErrorQueries))
	s.mux.HandleFunc("/api/stats", s.auth(s.handleStats))
}

// ─────────────────────────────────────────────────────────────
//  Auth middleware
// ─────────────────────────────────────────────────────────────

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Accept token via header or query param
		token := r.Header.Get("X-Dashboard-Token")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if token == "" {
			// Try Authorization: Bearer <token>
			if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				token = strings.TrimPrefix(auth, "Bearer ")
			}
		}

		if token != s.token {
			if !s.collector.IsLicensed() {
				jsonError(w, http.StatusPaymentRequired,
					"godb Pro license required — visit https://godb.dev/pricing")
				return
			}
			jsonError(w, http.StatusUnauthorized, "invalid dashboard token")
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "X-Dashboard-Token, Authorization")
		next(w, r)
	}
}

// ─────────────────────────────────────────────────────────────
//  Handlers
// ─────────────────────────────────────────────────────────────

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	jsonOK(w, map[string]interface{}{
		"status":   "ok",
		"licensed": s.collector.IsLicensed(),
		"version":  "1.0.0",
	})
}

func (s *Server) handleRecentQueries(w http.ResponseWriter, r *http.Request) {
	n := intParam(r, "limit", 50)
	records := s.collector.RecentQueries(n)
	jsonOK(w, map[string]interface{}{
		"queries": serializeRecords(records),
		"total":   len(records),
	})
}

func (s *Server) handleSlowQueries(w http.ResponseWriter, r *http.Request) {
	n := intParam(r, "limit", 50)
	records := s.collector.SlowQueries(n)
	jsonOK(w, map[string]interface{}{
		"slow_queries": serializeRecords(records),
		"threshold_ms": s.collector.SlowThresholdMS(),
		"total":        len(records),
	})
}

func (s *Server) handleTopQueries(w http.ResponseWriter, r *http.Request) {
	n := intParam(r, "limit", 20)
	stats := s.collector.TopQueries(n)
	jsonOK(w, map[string]interface{}{
		"queries": serializeStats(stats),
	})
}

func (s *Server) handleErrorQueries(w http.ResponseWriter, r *http.Request) {
	n := intParam(r, "limit", 20)
	stats := s.collector.ErrorQueries(n)
	jsonOK(w, map[string]interface{}{
		"queries": serializeStats(stats),
	})
}

func (s *Server) handleStats(w http.ResponseWriter, _ *http.Request) {
	total, errs, slow := s.collector.Counters()
	jsonOK(w, map[string]interface{}{
		"total_queries": total,
		"total_errors":  errs,
		"total_slow":    slow,
		"threshold_ms":  s.collector.SlowThresholdMS(),
		"licensed":      s.collector.IsLicensed(),
	})
}

// ─────────────────────────────────────────────────────────────
//  Serialisation helpers
// ─────────────────────────────────────────────────────────────

func serializeRecords(records []QueryRecord) []map[string]interface{} {
	out := make([]map[string]interface{}, len(records))
	for i, r := range records {
		errMsg := ""
		if r.Error != nil {
			errMsg = r.Error.Error()
		}
		out[i] = map[string]interface{}{
			"sql":         r.SQL,
			"duration_ms": float64(r.Duration.Microseconds()) / 1000.0,
			"executed_at": r.ExecutedAt.Format(time.RFC3339Nano),
			"table":       r.Table,
			"operation":   r.Operation,
			"error":       errMsg,
			"trace_id":    r.TraceID,
		}
	}
	return out
}

func serializeStats(stats []*QueryStats) []map[string]interface{} {
	out := make([]map[string]interface{}, len(stats))
	for i, s := range stats {
		out[i] = map[string]interface{}{
			"fingerprint": s.Fingerprint,
			"sql":         s.SQL,
			"count":       s.Count,
			"total_ms":    float64(s.TotalTime.Microseconds()) / 1000.0,
			"avg_ms":      float64(s.AvgTime.Microseconds()) / 1000.0,
			"max_ms":      float64(s.MaxTime.Microseconds()) / 1000.0,
			"min_ms":      float64(s.MinTime.Microseconds()) / 1000.0,
			"error_count": s.ErrorCount,
			"last_seen":   s.LastSeen.Format(time.RFC3339),
			"table":       s.Table,
			"operation":   s.Operation,
		}
	}
	return out
}

func jsonOK(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func intParam(r *http.Request, name string, def int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}
