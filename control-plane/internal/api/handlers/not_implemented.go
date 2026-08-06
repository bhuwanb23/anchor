package handlers

import "net/http"

// NotImplemented is the shared handler for routes in the Layer 6 Step 1 route
// table whose business logic is implemented in later steps (Step 5 handlers).
// It returns 501 so clients can distinguish "route exists but not built yet"
// from "route does not exist" (404).
func NotImplemented(w http.ResponseWriter, r *http.Request) {
	writeAPIError(w, r, http.StatusNotImplemented, "not_implemented", "This endpoint is registered but not implemented yet (Layer 6 Step 5)")
}
