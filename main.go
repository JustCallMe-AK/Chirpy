package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
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

func main() {
	serverMux := http.NewServeMux()
	apiCfg := &apiConfig{
		fileserverHits: new(atomic.Int32),
	}

	serverMux.HandleFunc("POST /admin/reset", func(w http.ResponseWriter, r *http.Request) {
		apiCfg.reset()
		w.Write([]byte("hit counter has been reset"))
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

	serverMux.HandleFunc("POST /api/validate_chirp", func(w http.ResponseWriter, r *http.Request) {
		type parameters struct {
			Body string `json:"body"`
		}

		decoder := json.NewDecoder(r.Body)
		params := parameters{}

		if decodingError := decoder.Decode(&params); decodingError != nil {
			log.Printf("Error decoding parameters: %s", decodingError)
			w.WriteHeader(500)
			return
		}

		type response struct {
			Cleaned_Body string `json:"cleaned_body"`
			Error        string `json:"error"`
		}

		var responseBody response
		var statusCode int

		if len(params.Body) > 140 {
			responseBody = response{
				Cleaned_Body: params.Body,
				Error:        "Chirp was too long",
			}
			statusCode = 400
		} else {
			responseBody = response{
				Cleaned_Body: no_expletives(params.Body),
				Error:        "",
			}
			statusCode = 200
		}

		data, jsonError := json.Marshal(responseBody)
		if jsonError != nil {
			log.Printf("Error marshalling JSON %s", jsonError)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		w.Write(data)

	})
	serverMux.HandleFunc("GET /api/healthz", func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Add("Content-Type", "text/plain; charset=utf-8")
		responseWriter.Write([]byte("OK"))
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
