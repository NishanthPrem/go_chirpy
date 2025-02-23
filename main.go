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

	"github.com/NishanthPrem/go_chirpy/internal/database"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileServerHits atomic.Int32
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileServerHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (cfg *apiConfig) metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
  <body>
    <h1>Welcome, Chirpy Admin</h1>
    <p>Chirpy has been visited %d times!</p>
  </body>
</html>`, cfg.fileServerHits.Load())
}

func (cfg *apiConfig) resetHandler(dbQueries *database.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := dbQueries.DeleteAllUsers(r.Context())
		if err != nil {
			log.Printf("Failed to delete users: %s", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		cfg.fileServerHits.Store(0)
		w.WriteHeader(http.StatusOK)
	}
}

func main() {
	apiCfg := apiConfig{}

	dbURL := os.Getenv("DB_URL")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database", err)
	}
	defer db.Close()
	dbQueries := database.New(db)

	mux := http.NewServeMux()
	srv := http.Server{
		Handler: mux,
		Addr:    ":8080",
	}
	mux.HandleFunc("POST /admin/reset", apiCfg.resetHandler(dbQueries))

	// Serve the "Welcome to Chirpy" message for /app
	mux.HandleFunc("/app", apiCfg.middlewareMetricsInc(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Welcome to Chirpy"))
	})).ServeHTTP)
	mux.Handle("/app/assets/", apiCfg.middlewareMetricsInc(http.StripPrefix("/app/assets/", http.FileServer(http.Dir("./assets")))))

	mux.HandleFunc("GET /admin/metrics", apiCfg.metricsHandler)

	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/healthz" {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	mux.HandleFunc("POST /api/validate_chirp", func(w http.ResponseWriter, r *http.Request) {
		type parameters struct {
			Body string `json:"body"`
		}

		decoder := json.NewDecoder(r.Body)
		params := parameters{}
		err := decoder.Decode(&params)
		if err != nil {
			log.Printf("Error decoding parameters: %s", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Something went wrong"})
			return
		}
		if len(params.Body) > 140 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Chirp is too long"})
		}

		profaneWords := []string{"kerfuffle", "sharbert", "fornax"}
		words := strings.Fields(params.Body)

		for i, word := range words {
			for _, profane := range profaneWords {
				if profane == strings.ToLower(word) {
					words[i] = "****"
					break
				}
			}
		}

		cleanedBody := strings.Join(words, " ")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"cleaned_body": cleanedBody})
	})

	mux.HandleFunc("POST /api/users", func(w http.ResponseWriter, r *http.Request) {
		type userRequest struct {
			Email string `json:"email"`
		}

		type userRespone struct {
			ID        string `json:"id"`
			CreatedAt string `json:"created_at"`
			UpdatedAt string `json:"updated_at"`
			Email     string `json:"email"`
		}

		decoder := json.NewDecoder(r.Body)
		params := userRequest{}
		if err := decoder.Decode(&params); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request"})
			return
		}

		userID := uuid.New().String()
		timestamp := time.Now().UTC().Format(time.RFC3339)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(userRespone{
			ID:        userID,
			CreatedAt: timestamp,
			UpdatedAt: timestamp,
			Email:     params.Email,
		})

	})
	if err := srv.ListenAndServe(); err != nil {
		fmt.Println("Failed to start server", err)
	}
}
