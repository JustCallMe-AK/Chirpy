package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/JustCallMe-AK/Chirpy/internal/auth"
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
	secret         string
	polkaKey       string
}

type jsonUser struct {
	ID          uuid.UUID `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Email       string    `json:"email"`
	IsChirpyRed bool      `json:"is_chirpy_red"`
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
	db, databaseConnectionOpenError := sql.Open("postgres", os.Getenv("DB_URL"))
	if databaseConnectionOpenError != nil {
		log.Fatal("error opening connection to database: %w", databaseConnectionOpenError)
	}
	defer db.Close()

	serverMux := http.NewServeMux()
	apiCfg := &apiConfig{
		fileserverHits: new(atomic.Int32),
		dbQueries:      database.New(db),
		platform:       os.Getenv("PLATFORM"),
		secret:         os.Getenv("SECRET"),
		polkaKey:       os.Getenv("POLKA_KEY"),
	}

	// Admin
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

	// Chirps
	serverMux.HandleFunc("GET /api/chirps", func(w http.ResponseWriter, r *http.Request) {
		authorIDParam := r.URL.Query().Get("author_id")
		sortParam := strings.ToLower(r.URL.Query().Get("sort"))

		var chirps []database.Chirp
		var chipGatheringError error

		if authorIDParam != "" {
			authorID, parseErr := uuid.Parse(authorIDParam)
			if parseErr != nil {
				http.Error(w, "invalid author_id", http.StatusBadRequest)
				return
			}
			chirps, chipGatheringError = apiCfg.dbQueries.GetChirpsByAuthor(r.Context(), authorID)
		} else {
			chirps, chipGatheringError = apiCfg.dbQueries.GetAllChirps(r.Context())
		}
		if chipGatheringError != nil {
			log.Printf("failure to get all chirps: %s", chipGatheringError)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// 🧭 Sort in-memory
		switch sortParam {
		case "desc":
			sort.Slice(chirps, func(i, j int) bool {
				return chirps[i].CreatedAt.After(chirps[j].CreatedAt)
			})
		default: // "asc" or empty
			sort.Slice(chirps, func(i, j int) bool {
				return chirps[i].CreatedAt.Before(chirps[j].CreatedAt)
			})
		}

		jsonChirps := make([]jsonChirp, len(chirps))
		for idx, chirp := range chirps {
			jsonChirps[idx] = jsonChirp{
				ID:        chirp.ID,
				CreatedAt: chirp.CreatedAt,
				UpdatedAt: chirp.UpdatedAt,
				Body:      chirp.Body,
				UserID:    chirp.UserID,
			}
		}

		data, jsonEncodingError := json.Marshal(jsonChirps)
		if jsonEncodingError != nil {
			log.Printf("failure to encode JSON response: %s", jsonEncodingError)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(data))
	})
	serverMux.HandleFunc("GET /api/chirps/{chirpID}", func(w http.ResponseWriter, r *http.Request) {
		chirpID, uuidParseError := uuid.Parse(r.PathValue("chirpID"))
		if uuidParseError != nil {
			log.Printf("failure to parse for UUID: %s", uuidParseError)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		desiredChirp, chirpFetchError := apiCfg.dbQueries.GetChirp(r.Context(), chirpID)
		if chirpFetchError != nil {
			log.Printf("failure to fetch chirp: %s", chirpFetchError)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		data, jsonEncodingError := json.Marshal(jsonChirp{
			ID:        desiredChirp.ID,
			CreatedAt: desiredChirp.CreatedAt,
			UpdatedAt: desiredChirp.UpdatedAt,
			Body:      desiredChirp.Body,
			UserID:    desiredChirp.UserID,
		})
		if jsonEncodingError != nil {
			log.Printf("failure to properly encode response: %s", jsonEncodingError)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	})
	serverMux.HandleFunc("POST /api/chirps", func(w http.ResponseWriter, r *http.Request) {
		type parameters struct {
			Body string `json:"body"`
		}

		// Extract Bearer token
		tokenString, err := auth.GetBearerToken(r.Header)
		if err != nil {
			log.Printf("missing or invalid bearer token: %s", err)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("unauthorized"))
			return
		}

		//  Validate JWT and extract user ID
		userID, err := auth.ValidateJWT(tokenString, apiCfg.secret)
		if err != nil {
			log.Printf("invalid JWT: %s", err)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("unauthorized"))
			return
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
			UserID: userID,
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
	serverMux.HandleFunc("DELETE /api/chirps/{chirpID}", func(w http.ResponseWriter, r *http.Request) {
		// Extract and validate the token
		tokenString, err := auth.GetBearerToken(r.Header)
		if err != nil {
			http.Error(w, "missing or invalid Authorization header", http.StatusUnauthorized)
			return
		}
		userID, err := auth.ValidateJWT(tokenString, apiCfg.secret)
		if err != nil {
			http.Error(w, "invalid or expired token", http.StatusUnauthorized)
			return
		}

		// Get chirp ID from the path
		chirpID, uuidParseError := uuid.Parse(r.PathValue("chirpID"))
		if uuidParseError != nil {
			http.Error(w, "invalid chirp ID", http.StatusBadRequest)
			return
		}

		// Fetch chirp to verify ownership
		chirp, err := apiCfg.dbQueries.GetChirp(r.Context(), chirpID)
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "chirp not found", http.StatusNotFound)
			return
		} else if err != nil {
			http.Error(w, "error fetching chirp", http.StatusInternalServerError)
			return
		}

		// Check authorization
		if chirp.UserID != userID {
			http.Error(w, "forbidden: not the chirp author", http.StatusForbidden)
			return
		}

		// Delete chirp
		err = apiCfg.dbQueries.DeleteChirpByID(r.Context(), chirpID)
		if err != nil {
			http.Error(w, "failed to delete chirp", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})

	// Readiness endpoint
	serverMux.HandleFunc("GET /api/healthz", func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Add("Content-Type", "text/plain; charset=utf-8")
		responseWriter.Write([]byte("OK"))
	})

	// Authentication
	serverMux.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) {
		type parameters struct {
			Password string `json:"password"`
			Email    string `json:"email"`
		}

		// Decode Request Body
		decoder := json.NewDecoder(r.Body)
		params := &parameters{}
		if decodingError := decoder.Decode(&params); decodingError != nil {
			log.Printf("failure to decode JSON body: %s", decodingError)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Retrieve user credentials
		user, userFetchingError := apiCfg.dbQueries.GetUserByEmail(r.Context(), params.Email)
		if userFetchingError != nil {
			log.Printf("failure to fetch user credentials: %s", userFetchingError)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("incorrect email or password"))
		}

		// Compare hash of submitted password with retrieved hashed password
		if hashesMatch := auth.CheckPasswordHash(user.HashedPassword, params.Password); hashesMatch != nil {
			log.Printf("hashed passwords do not match: %s", hashesMatch)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("incorrect email or password"))
		}

		// Generate JWT
		token, err := auth.MakeJWT(user.ID, apiCfg.secret, time.Hour)
		if err != nil {
			log.Printf("failed to make JWT: %s", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Create refresh token (60 days)
		refreshToken, err := auth.MakeRefreshToken()
		if err != nil {
			http.Error(w, "failed to generate refresh token", http.StatusInternalServerError)
			return
		}
		now := time.Now().UTC()
		expiresAt := now.Add(60 * 24 * time.Hour) // 60 days

		// Store refresh token in database
		if _, err := apiCfg.dbQueries.CreateRefreshToken(r.Context(), database.CreateRefreshTokenParams{
			Token:     refreshToken,
			UserID:    user.ID,
			CreatedAt: now,
			UpdatedAt: now,
			ExpiresAt: expiresAt,
			RevokedAt: sql.NullTime{Valid: false}, // NULL on creation
		}); err != nil {
			http.Error(w, "failed to store refresh token", http.StatusInternalServerError)
			return
		}

		data, jsonEncodingError := json.Marshal(map[string]interface{}{
			"id":            user.ID,
			"created_at":    user.CreatedAt,
			"updated_at":    user.UpdatedAt,
			"email":         user.Email,
			"is_chirpy_red": user.IsChirpyRed,
			"token":         token,
			"refresh_token": refreshToken,
		})
		if jsonEncodingError != nil {
			log.Printf("failure to encode JSON response: %s", jsonEncodingError)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Send JSON Response Body
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	})
	serverMux.HandleFunc("POST /api/refresh", func(w http.ResponseWriter, r *http.Request) {
		type response struct {
			Token string `json:"token"`
		}

		// Step 1: Extract token
		rawToken, err := auth.GetBearerToken(r.Header)
		if err != nil {
			http.Error(w, "missing or invalid Authorization header", http.StatusUnauthorized)
			return
		}

		// Step 2: Query user from refresh token
		user, err := apiCfg.dbQueries.GetUserFromRefreshToken(r.Context(), rawToken)
		if err != nil {
			http.Error(w, "invalid or expired refresh token", http.StatusUnauthorized)
			return
		}

		// Step 3: Generate new access token
		accessToken, err := auth.MakeJWT(user.ID, apiCfg.secret, time.Hour)
		if err != nil {
			http.Error(w, "could not create access token", http.StatusInternalServerError)
			return
		}

		// Step 4: Respond with new token
		resp := response{Token: accessToken}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	serverMux.HandleFunc("POST /api/revoke", func(w http.ResponseWriter, r *http.Request) {
		// Step 1: Extract token
		refreshToken, err := auth.GetBearerToken(r.Header)
		if err != nil {
			http.Error(w, "missing or invalid Authorization header", http.StatusUnauthorized)
			return
		}

		// Step 2: Attempt revocation
		err = apiCfg.dbQueries.RevokeRefreshToken(r.Context(), refreshToken)
		if err != nil {
			http.Error(w, "failed to revoke refresh token", http.StatusUnauthorized)
			return
		}

		// Step 3: Respond with 204 No Content
		w.WriteHeader(http.StatusNoContent)
	})

	// Users
	serverMux.HandleFunc("POST /api/users", func(w http.ResponseWriter, r *http.Request) {
		type parameters struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}

		// Decode JSON Body
		decoder := json.NewDecoder(r.Body)
		params := parameters{}
		if decodingError := decoder.Decode(&params); decodingError != nil {
			log.Printf("failure to decode JSON body: %s", decodingError)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Create new user
		hashedPassword, hashingError := auth.HashedPassword(params.Password)
		if hashingError != nil {
			log.Printf("failure to hash submitted password: %s", hashingError)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		newUser, userCreationError := apiCfg.dbQueries.CreateUser(r.Context(), database.CreateUserParams{
			Email:          params.Email,
			HashedPassword: hashedPassword,
		})
		if userCreationError != nil {
			log.Printf("failure to create new user: %s", userCreationError)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		responseUser := &jsonUser{
			ID:          newUser.ID,
			CreatedAt:   newUser.CreatedAt,
			UpdatedAt:   newUser.UpdatedAt,
			Email:       newUser.Email,
			IsChirpyRed: newUser.IsChirpyRed,
		}

		// Encode JSON Response
		data, jsonResponseError := json.Marshal(responseUser)
		if jsonResponseError != nil {
			log.Printf("failure to encode JSON response: %s", jsonResponseError)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Send JSON Response Body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write(data)
	})
	serverMux.HandleFunc("PUT /api/users", func(w http.ResponseWriter, r *http.Request) {
		// Step 1: Extract and validate access token
		tokenString, err := auth.GetBearerToken(r.Header)
		if err != nil {
			http.Error(w, "missing or invalid Authorization header", http.StatusUnauthorized)
			return
		}
		userID, err := auth.ValidateJWT(tokenString, apiCfg.secret)
		if err != nil {
			http.Error(w, "invalid or expired token", http.StatusUnauthorized)
			return
		}

		// Step 2: Parse request body
		type parameters struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		var params parameters
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&params); err != nil {
			http.Error(w, "failed to parse request body", http.StatusBadRequest)
			return
		}

		// Step 3: Hash the new password
		hashedPassword, err := auth.HashedPassword(params.Password)
		if err != nil {
			http.Error(w, "failed to hash password", http.StatusInternalServerError)
			return
		}

		// Step 4: Update the user in the DB
		updatedUser, err := apiCfg.dbQueries.UpdateUserByID(r.Context(), database.UpdateUserByIDParams{
			Email:          params.Email,
			HashedPassword: hashedPassword,
			ID:             userID,
		})
		if err != nil {
			http.Error(w, "failed to update user", http.StatusInternalServerError)
			return
		}

		// Step 5: Return updated user (without password)
		response := jsonUser{
			ID:          updatedUser.ID,
			CreatedAt:   updatedUser.CreatedAt,
			UpdatedAt:   updatedUser.UpdatedAt,
			Email:       updatedUser.Email,
			IsChirpyRed: updatedUser.IsChirpyRed,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	})

	// Webhooks
	serverMux.HandleFunc("POST /api/polka/webhooks", func(w http.ResponseWriter, r *http.Request) {
		apiKey, err := auth.GetAPIKey(r.Header)
		if err != nil || apiKey != apiCfg.polkaKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		type webhookData struct {
			Event string `json:"event"`
			Data  struct {
				UserID uuid.UUID `json:"user_id"`
			} `json:"data"`
		}

		var payload webhookData
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		if payload.Event != "user.upgraded" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if err := apiCfg.dbQueries.UpgradeUserToChirpyRed(r.Context(), payload.Data.UserID); err != nil {
			http.Error(w, "failed to upgrade user", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
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
