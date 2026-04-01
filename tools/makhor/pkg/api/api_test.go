package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	t.Run("writes JSON with correct content type", func(t *testing.T) {
		rr := httptest.NewRecorder()
		data := map[string]string{"key": "value"}

		writeJSON(rr, http.StatusOK, data)

		if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want %q", ct, "application/json")
		}
		if rr.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
		}

		var got map[string]string
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		if got["key"] != "value" {
			t.Errorf("got[key] = %q, want %q", got["key"], "value")
		}
	})

	t.Run("sets custom status code", func(t *testing.T) {
		rr := httptest.NewRecorder()
		writeJSON(rr, http.StatusCreated, nil)

		if rr.Code != http.StatusCreated {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusCreated)
		}
	})
}

func TestSuccess(t *testing.T) {
	rr := httptest.NewRecorder()
	data := map[string]int{"count": 42}

	success(rr, data)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp Response
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if !resp.Success {
		t.Error("Success = false, want true")
	}
	if resp.Error != "" {
		t.Errorf("Error = %q, want empty", resp.Error)
	}
}

func TestSuccessWithMeta(t *testing.T) {
	rr := httptest.NewRecorder()
	data := []string{"item1", "item2"}
	meta := &Meta{
		Page:       2,
		PerPage:    10,
		Total:      25,
		TotalPages: 3,
	}

	successWithMeta(rr, data, meta)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp Response
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if !resp.Success {
		t.Error("Success = false, want true")
	}
	if resp.Meta == nil {
		t.Fatal("Meta is nil")
	}
	if resp.Meta.Page != 2 {
		t.Errorf("Meta.Page = %d, want 2", resp.Meta.Page)
	}
	if resp.Meta.PerPage != 10 {
		t.Errorf("Meta.PerPage = %d, want 10", resp.Meta.PerPage)
	}
	if resp.Meta.Total != 25 {
		t.Errorf("Meta.Total = %d, want 25", resp.Meta.Total)
	}
	if resp.Meta.TotalPages != 3 {
		t.Errorf("Meta.TotalPages = %d, want 3", resp.Meta.TotalPages)
	}
}

func TestErrorResponse(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		message string
	}{
		{"bad request", http.StatusBadRequest, "Invalid input"},
		{"unauthorized", http.StatusUnauthorized, "Authentication required"},
		{"forbidden", http.StatusForbidden, "Permission denied"},
		{"not found", http.StatusNotFound, "Resource not found"},
		{"internal error", http.StatusInternalServerError, "Something went wrong"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			errorResponse(rr, tt.status, tt.message)

			if rr.Code != tt.status {
				t.Errorf("status = %d, want %d", rr.Code, tt.status)
			}

			var resp Response
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			if resp.Success {
				t.Error("Success = true, want false")
			}
			if resp.Error != tt.message {
				t.Errorf("Error = %q, want %q", resp.Error, tt.message)
			}
		})
	}
}

func TestNew(t *testing.T) {
	api := New(nil)
	if api == nil {
		t.Fatal("New() returned nil")
	}
	if api.DB != nil {
		t.Error("DB should be nil when passed nil")
	}
}

func TestRouter(t *testing.T) {
	api := New(nil)
	router := api.Router()

	if router == nil {
		t.Fatal("Router() returned nil")
	}

	// Verify router is an http.Handler
	_, ok := router.(http.Handler)
	if !ok {
		t.Error("Router() does not return http.Handler")
	}
}

func TestResponseStruct(t *testing.T) {
	t.Run("success response serialization", func(t *testing.T) {
		resp := Response{
			Success: true,
			Data:    map[string]string{"name": "test"},
		}

		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}

		var got Response
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if !got.Success {
			t.Error("Success not preserved")
		}
	})

	t.Run("error response serialization", func(t *testing.T) {
		resp := Response{
			Success: false,
			Error:   "test error",
		}

		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}

		var got Response
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		if got.Success {
			t.Error("Success should be false")
		}
		if got.Error != "test error" {
			t.Errorf("Error = %q, want %q", got.Error, "test error")
		}
	})

	t.Run("omits empty fields", func(t *testing.T) {
		resp := Response{
			Success: true,
			Data:    "hello",
		}

		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}

		// Should not contain "error" key since it's empty and has omitempty
		var raw map[string]interface{}
		json.Unmarshal(data, &raw)

		if _, exists := raw["error"]; exists {
			t.Error("empty error field should be omitted")
		}
		if _, exists := raw["meta"]; exists {
			t.Error("nil meta field should be omitted")
		}
	})
}

func TestMetaStruct(t *testing.T) {
	meta := Meta{
		Page:       5,
		PerPage:    20,
		Total:      100,
		TotalPages: 5,
	}

	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got Meta
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got.Page != 5 {
		t.Errorf("Page = %d, want 5", got.Page)
	}
	if got.PerPage != 20 {
		t.Errorf("PerPage = %d, want 20", got.PerPage)
	}
	if got.Total != 100 {
		t.Errorf("Total = %d, want 100", got.Total)
	}
	if got.TotalPages != 5 {
		t.Errorf("TotalPages = %d, want 5", got.TotalPages)
	}
}

// TestWriteJSONComplexData tests writing complex nested data structures.
func TestWriteJSONComplexData(t *testing.T) {
	rr := httptest.NewRecorder()
	data := map[string]interface{}{
		"posts": []map[string]interface{}{
			{"id": 1, "title": "Post 1"},
			{"id": 2, "title": "Post 2"},
		},
		"count": 2,
	}

	writeJSON(rr, http.StatusOK, data)

	var got map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	posts, ok := got["posts"].([]interface{})
	if !ok {
		t.Fatal("posts should be an array")
	}
	if len(posts) != 2 {
		t.Errorf("expected 2 posts, got %d", len(posts))
	}
}

// TestSuccessWithNilData tests success response with nil data.
func TestSuccessWithNilData(t *testing.T) {
	rr := httptest.NewRecorder()
	success(rr, nil)

	var resp Response
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if !resp.Success {
		t.Error("Success should be true")
	}
}

// TestSuccessWithEmptySlice tests success response with empty slice.
func TestSuccessWithEmptySlice(t *testing.T) {
	rr := httptest.NewRecorder()
	success(rr, []string{})

	var resp Response
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if !resp.Success {
		t.Error("Success should be true")
	}
	// Data should be present as empty array, not null
	data, _ := json.Marshal(resp.Data)
	if string(data) != "[]" {
		t.Errorf("Data should be empty array, got %s", string(data))
	}
}

// TestMetaPagination tests Meta struct edge cases.
func TestMetaPagination(t *testing.T) {
	tests := []struct {
		name       string
		page       int
		perPage    int
		total      int
		totalPages int
	}{
		{"first page", 1, 10, 100, 10},
		{"middle page", 5, 10, 100, 10},
		{"last page", 10, 10, 100, 10},
		{"single page", 1, 50, 25, 1},
		{"empty result", 1, 10, 0, 0},
		{"exact boundary", 1, 10, 10, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := &Meta{
				Page:       tt.page,
				PerPage:    tt.perPage,
				Total:      tt.total,
				TotalPages: tt.totalPages,
			}

			data, err := json.Marshal(meta)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}

			var got Meta
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}

			if got.Page != tt.page {
				t.Errorf("Page = %d, want %d", got.Page, tt.page)
			}
			if got.TotalPages != tt.totalPages {
				t.Errorf("TotalPages = %d, want %d", got.TotalPages, tt.totalPages)
			}
		})
	}
}

// TestErrorResponseVariousStatuses tests error responses with various HTTP status codes.
func TestErrorResponseVariousStatuses(t *testing.T) {
	statuses := []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusMethodNotAllowed,
		http.StatusConflict,
		http.StatusGone,
		http.StatusUnprocessableEntity,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusServiceUnavailable,
	}

	for _, status := range statuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
			rr := httptest.NewRecorder()
			errorResponse(rr, status, "test error")

			if rr.Code != status {
				t.Errorf("status = %d, want %d", rr.Code, status)
			}

			var resp Response
			json.Unmarshal(rr.Body.Bytes(), &resp)
			if resp.Success {
				t.Error("Success should be false for error response")
			}
		})
	}
}

// TestResponseJSONSerialization tests complete round-trip serialization.
func TestResponseJSONSerialization(t *testing.T) {
	t.Run("full response with all fields", func(t *testing.T) {
		resp := Response{
			Success: true,
			Data:    map[string]string{"key": "value"},
			Meta:    &Meta{Page: 1, PerPage: 10, Total: 100, TotalPages: 10},
		}

		data, _ := json.Marshal(resp)
		var got Response
		json.Unmarshal(data, &got)

		if got.Meta == nil {
			t.Fatal("Meta should not be nil")
		}
		if got.Meta.Total != 100 {
			t.Errorf("Meta.Total = %d, want 100", got.Meta.Total)
		}
	})

	t.Run("response with unicode data", func(t *testing.T) {
		resp := Response{
			Success: true,
			Data:    map[string]string{"message": "こんにちは世界"},
		}

		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}

		var got Response
		json.Unmarshal(data, &got)

		dataMap, ok := got.Data.(map[string]interface{})
		if !ok {
			t.Fatal("Data should be a map")
		}
		if dataMap["message"] != "こんにちは世界" {
			t.Error("Unicode data not preserved")
		}
	})

	t.Run("response with special characters in error", func(t *testing.T) {
		rr := httptest.NewRecorder()
		errorResponse(rr, http.StatusBadRequest, "Invalid <script>alert('xss')</script>")

		var resp Response
		json.Unmarshal(rr.Body.Bytes(), &resp)

		// JSON encoding should preserve but not execute special chars
		if resp.Error == "" {
			t.Error("Error message should be present")
		}
	})
}

// TestContentTypeHeader tests that Content-Type header is always set correctly.
func TestContentTypeHeader(t *testing.T) {
	tests := []struct {
		name string
		fn   func(w http.ResponseWriter)
	}{
		{"writeJSON", func(w http.ResponseWriter) { writeJSON(w, http.StatusOK, nil) }},
		{"success", func(w http.ResponseWriter) { success(w, nil) }},
		{"successWithMeta", func(w http.ResponseWriter) { successWithMeta(w, nil, nil) }},
		{"errorResponse", func(w http.ResponseWriter) { errorResponse(w, http.StatusBadRequest, "error") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			tt.fn(rr)

			ct := rr.Header().Get("Content-Type")
			if ct != "application/json" {
				t.Errorf("Content-Type = %q, want %q", ct, "application/json")
			}
		})
	}
}
