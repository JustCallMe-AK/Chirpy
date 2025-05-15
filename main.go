package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	serverMux := http.NewServeMux()
	serverMux.Handle("/", http.FileServer(http.Dir(".")))
	serverMux.Handle("/assets/", http.StripPrefix("/assets", http.FileServer(http.Dir("./assets/"))))

	server := &http.Server{
		Addr:    ":8080",
		Handler: serverMux,
	}

	log.Println("Serving files from Chirpy on port 8080")
	if serverError := server.ListenAndServe(); serverError != nil {
		log.Fatal(fmt.Errorf("we have a problem getting the server started: %w", serverError))
	}
}
