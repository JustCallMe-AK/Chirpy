package main

import (
	"fmt"
	"net/http"
)

func main() {
	serverMux := http.NewServeMux()

	var server http.Server
	server.Handler = serverMux
	server.Addr = ":8080"

	if serverError := server.ListenAndServe(); serverError != nil {
		fmt.Println(fmt.Errorf("we have a problem getting the server started: %w", serverError))
	}
}
