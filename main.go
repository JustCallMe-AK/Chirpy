package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	serverMux := http.NewServeMux()

	serverMux.HandleFunc("/healthz", func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Add("Content-Type", "text/plain; charset=utf-8")
		responseWriter.Write([]byte("OK"))
	})
	// serverMux.Handle("/assets/", http.StripPrefix("/assets", http.FileServer(http.Dir("./assets/"))))
	serverMux.Handle("/app/", http.StripPrefix("/app", http.FileServer(http.Dir("./app"))))

	server := &http.Server{
		Addr:    ":8080",
		Handler: serverMux,
	}

	log.Println("Serving files from Chirpy on port 8080")
	if serverError := server.ListenAndServe(); serverError != nil {
		log.Fatal(fmt.Errorf("we have a problem getting the server started: %w", serverError))
	}
}
