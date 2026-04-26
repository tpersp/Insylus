package httpx

import (
	"encoding/json"
	"net/http"
)

const JSONContentType = "application/json; charset=utf-8"

func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", JSONContentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func MethodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func InvalidRequest(w http.ResponseWriter) {
	http.Error(w, "invalid request", http.StatusBadRequest)
}
