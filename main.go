package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileServerHits atomic.Int32
}

type Chirp struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserID    string    `json:"user_id"`
}

type ChirpRequest struct {
	Body   string `json:"body"`
	UserID string `json:"user_id"`
}

type User struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
}

type UserRequest struct {
	Email string `json:"email"`
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileServerHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (cfg *apiConfig) metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
  <body>
    <h1>Welcome, Chirpy Admin</h1>
    <p>Chirpy has been visited %d times!</p>
  </body>
</html>`, cfg.fileServerHits.Load())
}

func (cfg *apiConfig) resetHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, err := db.Exec("DELETE FROM users")
		if err != nil {
			log.Printf("Failed to delete users: %s", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		cfg.fileServerHits.Store(0)
		w.WriteHeader(http.StatusOK)
	}
}

func validateChirp(body string) error {
	if len(body) > 140 {
		return fmt.Errorf("chirp is too long")
	}
	if len(body) == 0 {
		return fmt.Errorf("chirp body cannot be empty")
	}
	return nil
}

func cleanChirpBody(body string) string {
	profaneWords := []string{"kerfuffle", "sharbert", "fornax"}
	words := strings.Fields(body)

	for i, word := range words {
		for _, profane := range profaneWords {
			if strings.ToLower(word) == profane {
				words[i] = "****"
				break
			}
		}
	}

	return strings.Join(words, " ")
}

func saveChirp(db *sql.DB, chirp Chirp) error {
	_, err := db.Exec(
		"INSERT INTO chirps (id, created_at, updated_at, body, user_id) VALUES ($1, $2, $3, $4, $5)",
		chirp.ID,
		chirp.CreatedAt,
		chirp.UpdatedAt,
		chirp.Body,
		chirp.UserID,
	)
	return err
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(payload)
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, map[string]string{"error": message})
}

func createChirpHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ChirpRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondWithError(w, http.StatusBadRequest, "Invalid request payload")
			return
		}

		if err := validateChirp(req.Body); err != nil {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}

		cleanedBody := cleanChirpBody(req.Body)

		chirp := Chirp{
			ID:        uuid.New().String(),
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
			Body:      cleanedBody,
			UserID:    req.UserID,
		}

		if err := saveChirp(db, chirp); err != nil {
			log.Printf("Error saving chirp: %v", err)
			respondWithError(w, http.StatusInternalServerError, "Could not save chirp")
			return
		}

		respondWithJSON(w, http.StatusCreated, chirp)
	}
}

func getChirpHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := db.Query("SELECT id, created_at, updated_at, body, user_id FROM chirps ORDER BY created_at ASC")
		if err != nil {
			log.Printf("Error fetching chirps: %v", err)
			respondWithError(w, http.StatusInternalServerError, "Could not retrieve chirps")
			return
		}
		defer conn.Close()

		var chirps []Chirp
		for conn.Next() {
			var chirp Chirp
			if err := conn.Scan(&chirp.ID, &chirp.CreatedAt, &chirp.UpdatedAt, &chirp.Body, &chirp.UserID); err != nil {
				log.Printf("Error scanning chirp: %v", err)
				respondWithError(w, http.StatusInternalServerError, "Could not retrieve chirp")
				return
			}
			chirps = append(chirps, chirp)
		}
		respondWithJSON(w, http.StatusOK, chirps)
	}
}

func getChirpByIDHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		chirpID := r.PathValue("chirpID")
		var chirp Chirp

		err := db.QueryRow("SELECT id, created_at, updated_at, body, user_id FROM chirps WHERE id = $1", chirpID).
			Scan(&chirp.ID, &chirp.CreatedAt, &chirp.UpdatedAt, &chirp.Body, &chirp.UserID)

		if err == sql.ErrNoRows {
			respondWithError(w, http.StatusNotFound, "Chirp not found")
			return
		} else if err != nil {
			log.Printf("Error fetching chirp: %v", err)
			respondWithError(w, http.StatusInternalServerError, "Could not retrieve chirp")
			return
		}

		respondWithJSON(w, http.StatusOK, chirp)
	}
}

func createUserHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req UserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondWithError(w, http.StatusBadRequest, "Invalid request payload")
			return
		}

		user := User{
			ID:        uuid.New().String(),
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
			Email:     req.Email,
		}

		_, err := db.Exec(
			"INSERT INTO users (id, created_at, updated_at, email) VALUES ($1, $2, $3, $4)",
			user.ID,
			user.CreatedAt,
			user.UpdatedAt,
			user.Email,
		)

		if err != nil {
			log.Printf("Error creating user: %v", err)
			respondWithError(w, http.StatusInternalServerError, "Could not create user")
			return
		}

		respondWithJSON(w, http.StatusCreated, user)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func main() {
	apiCfg := apiConfig{}

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		log.Fatal("DB_URL environment variable is not set")
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Test database connection
	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	mux := http.NewServeMux()

	// Static file server with metrics
	fileServer := http.FileServer(http.Dir("./assets"))
	mux.Handle("/app/assets/", apiCfg.middlewareMetricsInc(http.StripPrefix("/app/assets/", fileServer)))

	// API routes
	mux.HandleFunc("GET /api/healthz", healthHandler)
	mux.HandleFunc("GET /api/chirps", getChirpHandler(db))
	mux.HandleFunc("GET /api/chirps/{chirpID}", getChirpByIDHandler(db))
	mux.HandleFunc("POST /api/users", createUserHandler(db))
	mux.HandleFunc("POST /api/chirps", createChirpHandler(db))

	// Admin routes
	mux.HandleFunc("GET /admin/metrics", apiCfg.metricsHandler)
	mux.HandleFunc("POST /admin/reset", apiCfg.resetHandler(db))

	// Welcome route
	mux.HandleFunc("/app", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Welcome to Chirpy"))
	})

	// Start server
	srv := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	log.Printf("Server starting on :8080")
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
