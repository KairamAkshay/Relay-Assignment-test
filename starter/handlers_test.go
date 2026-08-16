package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// resetStore replaces the global store with a fresh one for test isolation.
func resetStore() {
	store = NewStore()
}

func postEvents(t *testing.T, mux *http.ServeMux, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func getStats(t *testing.T, mux *http.ServeMux, campaignID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/campaigns/"+campaignID+"/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func setupMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /events", handlePostEvents)
	mux.HandleFunc("GET /campaigns/{campaignID}/stats", handleGetStats)
	return mux
}

func TestPostEvents_ValidEvent(t *testing.T) {
	resetStore()
	mux := setupMux()

	body := `[{"event_id":"evt_1","campaign_id":"cmp_1","contact_id":"ct_1","type":"sent","timestamp":"2026-08-10T06:00:00Z"}]`
	w := postEvents(t, mux, body)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}

	var resp BatchResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Accepted != 1 {
		t.Errorf("accepted: want 1, got %d", resp.Accepted)
	}
}

func TestPostEvents_DuplicateEvent(t *testing.T) {
	resetStore()
	mux := setupMux()

	body := `[{"event_id":"evt_1","campaign_id":"cmp_1","contact_id":"ct_1","type":"sent","timestamp":"2026-08-10T06:00:00Z"}]`
	postEvents(t, mux, body) // first time
	w := postEvents(t, mux, body) // second time — duplicate

	var resp BatchResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Duplicates != 1 {
		t.Errorf("duplicates: want 1, got %d", resp.Duplicates)
	}
	if resp.Accepted != 0 {
		t.Errorf("accepted: want 0, got %d", resp.Accepted)
	}
}

func TestPostEvents_DuplicateAcrossRequests(t *testing.T) {
	resetStore()
	mux := setupMux()

	body1 := `[{"event_id":"evt_1","campaign_id":"cmp_1","contact_id":"ct_1","type":"sent","timestamp":"2026-08-10T06:00:00Z"}]`
	body2 := `[{"event_id":"evt_1","campaign_id":"cmp_1","contact_id":"ct_1","type":"sent","timestamp":"2026-08-10T06:00:00Z"}]`

	postEvents(t, mux, body1)
	w := postEvents(t, mux, body2)

	var resp BatchResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Duplicates != 1 {
		t.Errorf("duplicate across requests: want 1 duplicate, got %d", resp.Duplicates)
	}
}

func TestPostEvents_InvalidEventType(t *testing.T) {
	resetStore()
	mux := setupMux()

	body := `[{"event_id":"evt_1","campaign_id":"cmp_1","contact_id":"ct_1","type":"spam_report","timestamp":"2026-08-10T06:00:00Z"}]`
	w := postEvents(t, mux, body)

	var resp BatchResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Rejected != 1 {
		t.Errorf("rejected: want 1, got %d", resp.Rejected)
	}
	if resp.Accepted != 0 {
		t.Errorf("accepted: want 0, got %d", resp.Accepted)
	}
}

func TestPostEvents_InvalidTimestamp(t *testing.T) {
	resetStore()
	mux := setupMux()

	body := `[{"event_id":"evt_1","campaign_id":"cmp_1","contact_id":"ct_1","type":"sent","timestamp":"not-a-time"}]`
	w := postEvents(t, mux, body)

	var resp BatchResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Rejected != 1 {
		t.Errorf("rejected: want 1, got %d", resp.Rejected)
	}
}

func TestPostEvents_MixedBatch(t *testing.T) {
	resetStore()
	mux := setupMux()

	// 5 events: 3 valid, 1 invalid type, 1 missing contact_id
	body := `[
		{"event_id":"e1","campaign_id":"cmp_1","contact_id":"ct_1","type":"sent","timestamp":"2026-08-10T06:00:00Z"},
		{"event_id":"e2","campaign_id":"cmp_1","contact_id":"ct_2","type":"delivered","timestamp":"2026-08-10T06:01:00Z"},
		{"event_id":"e3","campaign_id":"cmp_1","contact_id":"ct_3","type":"OPENED","timestamp":"2026-08-10T06:02:00Z"},
		{"event_id":"e4","campaign_id":"cmp_1","contact_id":"","type":"clicked","timestamp":"2026-08-10T06:03:00Z"},
		{"event_id":"e5","campaign_id":"cmp_1","contact_id":"ct_5","type":"clicked","timestamp":"2026-08-10T06:04:00Z"}
	]`
	w := postEvents(t, mux, body)

	var resp BatchResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Accepted != 3 {
		t.Errorf("accepted: want 3, got %d", resp.Accepted)
	}
	if resp.Rejected != 2 {
		t.Errorf("rejected: want 2, got %d", resp.Rejected)
	}
}

func TestPostEvents_MalformedJSON(t *testing.T) {
	resetStore()
	mux := setupMux()

	body := `not json at all`
	w := postEvents(t, mux, body)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: want 400, got %d", w.Code)
	}
}

func TestGetStats_BasicCounts(t *testing.T) {
	resetStore()
	mux := setupMux()

	body := `[
		{"event_id":"e1","campaign_id":"cmp_1","contact_id":"ct_1","type":"sent","timestamp":"2026-08-10T06:00:00Z"},
		{"event_id":"e2","campaign_id":"cmp_1","contact_id":"ct_1","type":"delivered","timestamp":"2026-08-10T06:01:00Z"},
		{"event_id":"e3","campaign_id":"cmp_1","contact_id":"ct_1","type":"opened","timestamp":"2026-08-10T06:02:00Z"},
		{"event_id":"e4","campaign_id":"cmp_1","contact_id":"ct_1","type":"clicked","timestamp":"2026-08-10T06:03:00Z"}
	]`
	postEvents(t, mux, body)

	w := getStats(t, mux, "cmp_1")
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}

	var stats CampaignStats
	json.NewDecoder(w.Body).Decode(&stats)
	if stats.Sent != 1 || stats.Delivered != 1 || stats.Opened != 1 || stats.Clicked != 1 {
		t.Errorf("counts wrong: %+v", stats)
	}
	if stats.UniqueOpens != 1 {
		t.Errorf("unique_opens: want 1, got %d", stats.UniqueOpens)
	}
}

func TestGetStats_UniqueOpens(t *testing.T) {
	resetStore()
	mux := setupMux()

	body := `[
		{"event_id":"e1","campaign_id":"cmp_1","contact_id":"ct_A","type":"opened","timestamp":"2026-08-10T06:00:00Z"},
		{"event_id":"e2","campaign_id":"cmp_1","contact_id":"ct_A","type":"opened","timestamp":"2026-08-10T07:00:00Z"},
		{"event_id":"e3","campaign_id":"cmp_1","contact_id":"ct_A","type":"opened","timestamp":"2026-08-10T08:00:00Z"},
		{"event_id":"e4","campaign_id":"cmp_1","contact_id":"ct_B","type":"opened","timestamp":"2026-08-10T09:00:00Z"}
	]`
	postEvents(t, mux, body)

	w := getStats(t, mux, "cmp_1")
	var stats CampaignStats
	json.NewDecoder(w.Body).Decode(&stats)

	if stats.Opened != 4 {
		t.Errorf("opened: want 4, got %d", stats.Opened)
	}
	if stats.UniqueOpens != 2 {
		t.Errorf("unique_opens: want 2, got %d", stats.UniqueOpens)
	}
}

func TestGetStats_NotFound(t *testing.T) {
	resetStore()
	mux := setupMux()

	w := getStats(t, mux, "cmp_doesnt_exist")
	if w.Code != http.StatusNotFound {
		t.Errorf("status: want 404, got %d", w.Code)
	}
}

func TestPostEvents_ConcurrentIngestion(t *testing.T) {
	resetStore()
	mux := setupMux()

	const numRequests = 50
	var wg sync.WaitGroup
	wg.Add(numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			defer wg.Done()
			// All goroutines submit the same event.
			body := `[{"event_id":"evt_race","campaign_id":"cmp_1","contact_id":"ct_1","type":"sent","timestamp":"2026-08-10T06:00:00Z"}]`
			req := httptest.NewRequest("POST", "/events", bytes.NewReader([]byte(body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
		}()
	}
	wg.Wait()

	// Only one should have been accepted.
	stats, _ := store.GetStats("cmp_1")
	if stats.Sent != 1 {
		t.Errorf("concurrent: sent should be 1, got %d", stats.Sent)
	}
}

func TestPostEvents_OutOfOrder(t *testing.T) {
	resetStore()
	mux := setupMux()

	// Events arrive out of chronological order.
	body := `[
		{"event_id":"e3","campaign_id":"cmp_1","contact_id":"ct_1","type":"opened","timestamp":"2026-08-10T09:00:00Z"},
		{"event_id":"e1","campaign_id":"cmp_1","contact_id":"ct_1","type":"sent","timestamp":"2026-08-10T06:00:00Z"},
		{"event_id":"e2","campaign_id":"cmp_1","contact_id":"ct_1","type":"delivered","timestamp":"2026-08-10T07:00:00Z"}
	]`
	postEvents(t, mux, body)

	w := getStats(t, mux, "cmp_1")
	var stats CampaignStats
	json.NewDecoder(w.Body).Decode(&stats)
	if stats.Sent != 1 || stats.Delivered != 1 || stats.Opened != 1 {
		t.Errorf("out-of-order: sent=%d delivered=%d opened=%d", stats.Sent, stats.Delivered, stats.Opened)
	}
}

func TestPostEvents_CaseSensitiveType(t *testing.T) {
	resetStore()
	mux := setupMux()

	body := `[{"event_id":"e1","campaign_id":"cmp_1","contact_id":"ct_1","type":"OPENED","timestamp":"2026-08-10T06:00:00Z"}]`
	w := postEvents(t, mux, body)

	var resp BatchResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Rejected != 1 {
		t.Errorf("OPENED should be rejected: rejected=%d", resp.Rejected)
	}
}
