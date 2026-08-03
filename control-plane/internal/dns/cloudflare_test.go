package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// mockCloudflare simulates the Cloudflare API for testing.
type mockCloudflare struct {
	mu      sync.Mutex
	records []Record
	nextID  int
}

func newMockCloudflare() *mockCloudflare {
	return &mockCloudflare{nextID: 1}
}

func (m *mockCloudflare) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// GET /zones/{zoneID}/dns_records — list records
		if r.Method == http.MethodGet && path == "/zones/test-zone/dns_records" {
			m.mu.Lock()
			defer m.mu.Unlock()

			name := r.URL.Query().Get("name")
			recordType := r.URL.Query().Get("type")

			var matched []Record
			for _, rec := range m.records {
				if (name == "" || rec.Name == name) && (recordType == "" || rec.Type == recordType) {
					matched = append(matched, rec)
				}
			}

			cfResp := cfResponse{Success: true, Result: mustMarshal(matched)}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(cfResp)
			return
		}

		// POST /zones/{zoneID}/dns_records — create record
		if r.Method == http.MethodPost && path == "/zones/test-zone/dns_records" {
			m.mu.Lock()
			defer m.mu.Unlock()

			body, _ := io.ReadAll(r.Body)
			var newRecord Record
			json.Unmarshal(body, &newRecord)
			newRecord.ID = "rec-" + string(rune(m.nextID+'0'))
			m.nextID++
			m.records = append(m.records, newRecord)

			cfResp := cfResponse{Success: true, Result: mustMarshal(newRecord)}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(cfResp)
			return
		}

		// PATCH /zones/{zoneID}/dns_records/{id} — update record
		if r.Method == http.MethodPatch && strings.HasPrefix(path, "/zones/test-zone/dns_records/") {
			m.mu.Lock()
			defer m.mu.Unlock()

			id := path[len("/zones/test-zone/dns_records/"):]
			body, _ := io.ReadAll(r.Body)
			var update struct {
				Content string `json:"content"`
			}
			json.Unmarshal(body, &update)

			for i, rec := range m.records {
				if rec.ID == id {
					m.records[i].Content = update.Content
					break
				}
			}

			cfResp := cfResponse{Success: true}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(cfResp)
			return
		}

		// DELETE /zones/{zoneID}/dns_records/{id} — delete record
		if r.Method == http.MethodDelete && strings.HasPrefix(path, "/zones/test-zone/dns_records/") {
			m.mu.Lock()
			defer m.mu.Unlock()

			id := path[len("/zones/test-zone/dns_records/"):]
			for i, rec := range m.records {
				if rec.ID == id {
					m.records = append(m.records[:i], m.records[i+1:]...)
					break
				}
			}

			cfResp := cfResponse{Success: true}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(cfResp)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	})
}

func mustMarshal(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

func TestUpsertWildcard_CreatesRecord(t *testing.T) {
	mock := newMockCloudflare()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	client := NewClientWithBase("test-token", "test-zone", srv.URL)

	err := client.UpsertWildcard(context.Background(), "srv-a1b2c3d4", "1.2.3.4")
	if err != nil {
		t.Fatalf("UpsertWildcard: %v", err)
	}

	if len(mock.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(mock.records))
	}
	if mock.records[0].Name != "*.srv-a1b2c3d4" {
		t.Errorf("record name = %q, want *.srv-a1b2c3d4", mock.records[0].Name)
	}
	if mock.records[0].Content != "1.2.3.4" {
		t.Errorf("record content = %q, want 1.2.3.4", mock.records[0].Content)
	}
}

func TestUpsertWildcard_UpdatesRecord(t *testing.T) {
	mock := newMockCloudflare()
	mock.records = append(mock.records, Record{
		ID:      "rec-1",
		Type:    "A",
		Name:    "*.srv-a1b2c3d4",
		Content: "1.2.3.4",
	})
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	client := NewClientWithBase("test-token", "test-zone", srv.URL)

	err := client.UpsertWildcard(context.Background(), "srv-a1b2c3d4", "5.6.7.8")
	if err != nil {
		t.Fatalf("UpsertWildcard: %v", err)
	}

	if len(mock.records) != 1 {
		t.Fatalf("expected 1 record (update, not create), got %d", len(mock.records))
	}
	if mock.records[0].Content != "5.6.7.8" {
		t.Errorf("record content = %q, want 5.6.7.8", mock.records[0].Content)
	}
}

func TestGetRecord_Found(t *testing.T) {
	mock := newMockCloudflare()
	mock.records = append(mock.records, Record{
		ID:      "rec-1",
		Type:    "A",
		Name:    "*.srv-a1b2c3d4",
		Content: "1.2.3.4",
	})
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	client := NewClientWithBase("test-token", "test-zone", srv.URL)

	record, err := client.GetRecord(context.Background(), "*.srv-a1b2c3d4", "A")
	if err != nil {
		t.Fatalf("GetRecord: %v", err)
	}
	if record == nil {
		t.Fatal("expected record, got nil")
	}
	if record.Content != "1.2.3.4" {
		t.Errorf("record content = %q, want 1.2.3.4", record.Content)
	}
}

func TestGetRecord_NotFound(t *testing.T) {
	mock := newMockCloudflare()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	client := NewClientWithBase("test-token", "test-zone", srv.URL)

	record, err := client.GetRecord(context.Background(), "*.nonexistent", "A")
	if err != nil {
		t.Fatalf("GetRecord: %v", err)
	}
	if record != nil {
		t.Errorf("expected nil record, got %+v", record)
	}
}

func TestDeleteWildcard(t *testing.T) {
	mock := newMockCloudflare()
	mock.records = append(mock.records, Record{
		ID:      "rec-1",
		Type:    "A",
		Name:    "*.srv-a1b2c3d4",
		Content: "1.2.3.4",
	})
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	client := NewClientWithBase("test-token", "test-zone", srv.URL)

	err := client.DeleteWildcard(context.Background(), "srv-a1b2c3d4")
	if err != nil {
		t.Fatalf("DeleteWildcard: %v", err)
	}

	if len(mock.records) != 0 {
		t.Errorf("expected 0 records after delete, got %d", len(mock.records))
	}
}

func TestDeleteWildcard_NotFound(t *testing.T) {
	mock := newMockCloudflare()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	client := NewClientWithBase("test-token", "test-zone", srv.URL)

	err := client.DeleteWildcard(context.Background(), "srv-nonexistent")
	if err != nil {
		t.Fatalf("DeleteWildcard should not error for missing record: %v", err)
	}
}

func TestFullLifecycle_UpsertFindDelete(t *testing.T) {
	mock := newMockCloudflare()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	client := NewClientWithBase("test-token", "test-zone", srv.URL)
	ctx := context.Background()

	// 1. Create
	err := client.UpsertWildcard(ctx, "srv-test1234", "10.0.0.1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// 2. Find
	record, err := client.GetRecord(ctx, "*.srv-test1234", "A")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if record == nil {
		t.Fatal("expected record after create, got nil")
	}
	if record.Content != "10.0.0.1" {
		t.Errorf("content = %q, want 10.0.0.1", record.Content)
	}

	// 3. Delete
	err = client.DeleteWildcard(ctx, "srv-test1234")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	// 4. Verify gone
	record, err = client.GetRecord(ctx, "*.srv-test1234", "A")
	if err != nil {
		t.Fatalf("find after delete: %v", err)
	}
	if record != nil {
		t.Errorf("expected nil after delete, got %+v", record)
	}
}

func TestMultipleRecords_Independent(t *testing.T) {
	mock := newMockCloudflare()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	client := NewClientWithBase("test-token", "test-zone", srv.URL)
	ctx := context.Background()

	err := client.UpsertWildcard(ctx, "srv-aaa11111", "1.1.1.1")
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	err = client.UpsertWildcard(ctx, "srv-bbb22222", "2.2.2.2")
	if err != nil {
		t.Fatalf("create second: %v", err)
	}

	if len(mock.records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(mock.records))
	}

	err = client.DeleteWildcard(ctx, "srv-aaa11111")
	if err != nil {
		t.Fatalf("delete first: %v", err)
	}

	if len(mock.records) != 1 {
		t.Fatalf("expected 1 record after delete, got %d", len(mock.records))
	}
	if mock.records[0].Name != "*.srv-bbb22222" {
		t.Errorf("remaining record = %q, want *.srv-bbb22222", mock.records[0].Name)
	}
}

func TestMockServer_HttpMethods(t *testing.T) {
	mock := newMockCloudflare()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	// POST create
	createBody := map[string]interface{}{
		"type":    "A",
		"name":    "*.test.example",
		"content": "10.0.0.1",
		"ttl":     1,
		"proxied": false,
	}
	data, _ := json.Marshal(createBody)
	resp, err := http.Post(srv.URL+"/zones/test-zone/dns_records", "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("POST create: %v", err)
	}
	resp.Body.Close()
	if len(mock.records) != 1 {
		t.Fatalf("expected 1 record after POST, got %d", len(mock.records))
	}

	// GET find
	resp2, err := http.Get(srv.URL + "/zones/test-zone/dns_records?name=*.test.example&type=A")
	if err != nil {
		t.Fatalf("GET find: %v", err)
	}
	defer resp2.Body.Close()
	var cfResp cfResponse
	json.NewDecoder(resp2.Body).Decode(&cfResp)
	var records []Record
	json.Unmarshal(cfResp.Result, &records)
	if len(records) != 1 {
		t.Fatalf("expected 1 record from GET, got %d", len(records))
	}

	// PATCH update
	updateBody := map[string]interface{}{"content": "20.0.0.2"}
	updateData, _ := json.Marshal(updateBody)
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/zones/test-zone/dns_records/"+records[0].ID, bytes.NewReader(updateData))
	req.Header.Set("Content-Type", "application/json")
	resp3, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH update: %v", err)
	}
	resp3.Body.Close()
	if mock.records[0].Content != "20.0.0.2" {
		t.Errorf("after PATCH, content = %q, want 20.0.0.2", mock.records[0].Content)
	}

	// DELETE
	req2, _ := http.NewRequest(http.MethodDelete, srv.URL+"/zones/test-zone/dns_records/"+records[0].ID, nil)
	resp4, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	resp4.Body.Close()
	if len(mock.records) != 0 {
		t.Errorf("expected 0 records after DELETE, got %d", len(mock.records))
	}
}
