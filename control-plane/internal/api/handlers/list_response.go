package handlers

import (
	"net/http"
)

// ListResponse is the standard wrapper for list endpoints.
// Every list endpoint returns this shape so clients can add
// pagination support without breaking changes.
type ListResponse struct {
	Data    interface{} `json:"data"`
	Total   int         `json:"total"`
	Page    int         `json:"page"`
	PerPage int         `json:"per_page"`
}

// RespondList writes a paginated list response.
func RespondList(w http.ResponseWriter, data interface{}, total, page, perPage int) {
	if data == nil {
		data = []interface{}{}
	}
	RespondJSON(w, http.StatusOK, ListResponse{
		Data:    data,
		Total:   total,
		Page:    page,
		PerPage: perPage,
	})
}
