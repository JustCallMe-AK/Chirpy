package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/JustCallMe-AK/Chirpy/internal/database"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

// func respondWithError(w http.ResponseWriter, code int, msg string) {

// }

// func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {

// }

func no_expletives(chirp string) string {
	expletives := []string{"kerfuffle", "sharbert", "fornax"}
	words := strings.Split(chirp, " ")
	for x, word := range words {
		if slices.Contains(expletives, strings.ToLower(word)) {
			words[x] = "****"
		}
	}
	return strings.Join(words, " ")
}

type apiConfig struct {
	fileserverHits *atomic.Int32
	dbQueries      *database.Queries
	platform       string
}

type jsonUser struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
}

type jsonChirp struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserID    uuid.UUID `json:"user_id"`
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (cfg *apiConfig) reset() {
	cfg.fileserverHits.Store(0)
}

// Logging middleware for future use
// func middlewareLog(next http.Handler) http.Handler {
// 	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		log.Printf("%s %s", r.Method, r.URL.Path)
// 		next.ServeHTTP(w, r)
// 	})
// }

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	platform := os.Getenv("PLATFORM")
	db, databaseConnectionOpenError := sql.Open("postgres", dbURL)
	if databaseConnectionOpenError != nil {
		log.Fatal("error opening connection to database: %w", databaseConnectionOpenError)
	}
	defer db.Close()
	dbQueries := database.New(db)

	serverMux := http.NewServeMux()
	apiCfg := &apiConfig{
		fileserverHits: new(atomic.Int32),
		dbQueries:      dbQueries,
		platform:       platform,
	}

	serverMux.HandleFunc("POST /admin/reset", func(w http.ResponseWriter, r *http.Request) {
		if apiCfg.platform != "dev" {
			http.Error(w, "403 Forbidden", http.StatusForbidden)
			return
		} else if usersDeletionError := apiCfg.dbQueries.DeleteUsers(r.Context()); usersDeletionError != nil {
			log.Printf("error deleting users: %v\n", usersDeletionError)
			http.Error(w, "Failed to reset", http.StatusInternalServerError)
			return
		} else {
			apiCfg.reset()
			w.Write([]byte("hit counter has been reset and all users have been deleted"))
		}
	})
	serverMux.HandleFunc("GET /admin/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(
			w,
			`<html>
				<body>
					<h1>Welcome, Chirpy Admin</h1>
					<p>Chirpy has been visited %d times!</p>
				</body>
			</html>`,
			apiCfg.fileserverHits.Load())
	})
	serverMux.HandleFunc("POST /api/chirps", func(w http.ResponseWriter, r *http.Request) {
		type parameters struct {
			Body   string    `json:"body"`
			UserID uuid.UUID `json:"user_id"`
		}

		// Decode JSON Request Body
		decoder := json.NewDecoder(r.Body)
		params := parameters{}

		if decodingError := decoder.Decode(&params); decodingError != nil {
			log.Printf("Error decoding parameters: %s", decodingError)
			w.WriteHeader(500)
			return
		}

		// Length validation
		if len(params.Body) > 140 {
			log.Printf("Chirp was too long")
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Create new Chirp
		newChirp, chirpCreationError := apiCfg.dbQueries.CreateChirp(r.Context(), database.CreateChirpParams{
			Body:   params.Body,
			UserID: params.UserID,
		})
		if chirpCreationError != nil {
			log.Printf("failure to create new chirp: %s", chirpCreationError)
			w.WriteHeader(500)
			return
		}

		// Create server response
		responseChirp := &jsonChirp{
			ID:        newChirp.ID,
			CreatedAt: newChirp.CreatedAt,
			UpdatedAt: newChirp.UpdatedAt,
			Body:      no_expletives(newChirp.Body),
			UserID:    newChirp.UserID,
		}

		// Encode JSON Response Body
		data, jsonError := json.Marshal(responseChirp)
		if jsonError != nil {
			log.Printf("Error marshalling JSON %s", jsonError)
		}

		// Send JSON Response Body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write(data)
	})
	serverMux.HandleFunc("GET /api/healthz", func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Add("Content-Type", "text/plain; charset=utf-8")
		responseWriter.Write([]byte("OK"))
	})
	serverMux.HandleFunc("POST /api/users", func(w http.ResponseWriter, r *http.Request) {
		type parameters struct {
			Email string `json:"email"`
		}

		// Decode JSON Body
		decoder := json.NewDecoder(r.Body)
		params := parameters{}
		if decodingError := decoder.Decode(&params); decodingError != nil {
			log.Printf("failure to decode JSON body: %s", decodingError)
			w.WriteHeader(500)
			return
		}

		// Create new user
		newUser, userCreationError := apiCfg.dbQueries.CreateUser(r.Context(), params.Email)
		if userCreationError != nil {
			log.Printf("failure to create new user: %s", userCreationError)
			w.WriteHeader(500)
			return
		}

		responseUser := &jsonUser{
			ID:        newUser.ID,
			CreatedAt: newUser.CreatedAt,
			UpdatedAt: newUser.UpdatedAt,
			Email:     newUser.Email,
		}

		// Encode JSON Response
		data, jsonResponseError := json.Marshal(responseUser)
		if jsonResponseError != nil {
			log.Printf("failure to encode JSON response: %s", jsonResponseError)
			w.WriteHeader(500)
			return
		}

		// Send JSON Response Body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		w.Write(data)
	})
	serverMux.Handle("/app/", apiCfg.middlewareMetricsInc(http.StripPrefix("/app/", http.FileServer(http.Dir("./app")))))

	server := &http.Server{
		Addr:    ":8080",
		Handler: serverMux,
	}

	log.Println("Serving files from Chirpy on port 8080")
	if serverError := server.ListenAndServe(); serverError != nil {
		log.Fatal(fmt.Errorf("we have a problem getting the server started: %w", serverError))
	}
}
