package main

import (
	"fmt"
	"log"
	"net/http"

	"gorun/app/web/ui"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := ui.Index().Render(r.Context(), w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	addr := ":8080"
	fmt.Printf("gorun web listening on %s\n", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
