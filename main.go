package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/hugoivankm/chirpy/internal/database"
	"github.com/joho/godotenv"

	_ "github.com/lib/pq"
)

const maxChirpLength = 140

var profaneWords = map[string]struct{}{
	"kerfuffle": {},
	"sharbert":  {},
	"fornax":    {},
}

type apiConfig struct {
	fileserverHits atomic.Int32
	db             *database.Queries
	platform       string
}

type Chirp struct {
	Body string `json:"body"`
}

type User struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func readinessHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK\n"))
}

func (cfg *apiConfig) metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf(`
	<html>
  	  <body>
        <h1>Welcome, Chirpy Admin</h1>
        <p>Chirpy has been visited %d times!</p>
     </body>
    </html>`, cfg.fileserverHits.Load())))
}

func (cfg *apiConfig) resetHandler(w http.ResponseWriter, r *http.Request) {

	if cfg.platform != "dev" {
		respondWithError(w, http.StatusForbidden, "Forbidden")
		return
	}

	err := cfg.db.DeleteUsers(r.Context())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to reset users")
		return
	}

	w.Header().Add("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	cfg.fileserverHits.Store(0)
	w.Write(fmt.Appendf(nil, "Hits: %d\n", 0))

}

func respondWithError(w http.ResponseWriter, code int, msg string) {

	type ErrorResponse struct {
		ErrorMsg string `json:"error"`
	}

	respondWithJSON(w, code, ErrorResponse{ErrorMsg: msg})
}

func respondWithJSON(w http.ResponseWriter, code int, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Something went wrong")
		return
	}

	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(data)
}

func isProfaneWord(word string) bool {
	_, ok := profaneWords[strings.ToLower(word)]
	return ok

}

func strToWordSlice(str string) []string {
	return strings.Split(str, " ")
}

func replaceProfaneWords(words string) string {
	w := strToWordSlice(words)
	for i, s := range w {
		if isProfaneWord(s) {
			w[i] = "****"
		}
	}
	return strings.Join(w, " ")
}

func validateChirpHandler(w http.ResponseWriter, r *http.Request) {
	type ReturnValues struct {
		CleanedBody string `json:"cleaned_body"`
	}

	decoder := json.NewDecoder(r.Body)
	chirp := Chirp{}
	err := decoder.Decode(&chirp)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Something went wrong")
		return
	}

	if len(chirp.Body) > maxChirpLength {
		respondWithError(w, http.StatusBadRequest, "Chirp is too long")
		return
	}

	respondWithJSON(w, http.StatusOK, ReturnValues{CleanedBody: replaceProfaneWords(chirp.Body)})

}

func (cfg *apiConfig) createUserHandler(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Email string `json:"email"`
	}
	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse request body")
		return
	}
	_, err = mail.ParseAddress(params.Email)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid email")
		return
	}

	userFromDb, err := cfg.db.CreateUser(r.Context(), params.Email)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create new user")
		return
	}
	user := User{
		ID:        userFromDb.ID,
		CreatedAt: userFromDb.CreatedAt.Time,
		UpdatedAt: userFromDb.UpdatedAt.Time,
		Email:     userFromDb.Email,
	}

	data, err := json.Marshal(user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	w.Write(data)

}

func main() {
	godotenv.Load()

	dbURL := os.Getenv("DB_URL")
	platform := os.Getenv("PLATFORM")

	if strings.TrimSpace(platform) == "" {
		log.Fatal("PLATFORM must be set")
	}

	dbConn, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Unable to connect to database with error: %v", err)
	}

	dbQueries := database.New(dbConn)

	apiCfg := apiConfig{
		db:       dbQueries,
		platform: platform,
	}

	// Endpoints
	mux := http.NewServeMux()

	mux.Handle("/app/", apiCfg.middlewareMetricsInc(
		http.StripPrefix("/app", http.FileServer(http.Dir(".")))))

	mux.HandleFunc("GET /admin/metrics", apiCfg.metricsHandler)
	mux.HandleFunc("POST /admin/reset", apiCfg.resetHandler)

	mux.HandleFunc("GET /api/healthz", readinessHandler)
	mux.HandleFunc("POST /api/validate_chirp", validateChirpHandler)
	mux.HandleFunc("POST /api/users", apiCfg.createUserHandler)

	srv := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	fmt.Printf("Serving on port: %v\n", strings.Trim(srv.Addr, ":"))
	err = srv.ListenAndServe()
	if err != nil {
		log.Fatal(err)
	}
}
