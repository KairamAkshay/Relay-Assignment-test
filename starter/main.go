package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// store is the global event store, created at startup.
var store = NewStore()

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /events", handlePostEvents)
	mux.HandleFunc("GET /campaigns/{campaignID}/stats", handleGetStats)

	addr := ":8080"
	fmt.Printf("listening on %s\n", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

// handlePostEvents ingests a JSON array of events.
//
// We decode the request body as []json.RawMessage so that one malformed
// element does not prevent the others from being processed. Each element
// is then individually unmarshalled, validated, and stored.
func handlePostEvents(w http.ResponseWriter, r *http.Request) {
	// Decode the outer array as raw messages.
	var rawEvents []json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawEvents); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "request body must be a JSON array: " + err.Error(),
		})
		return
	}

	if len(rawEvents) == 0 {
		writeJSON(w, http.StatusOK, BatchResponse{})
		return
	}

	resp := BatchResponse{}

	for i, raw := range rawEvents {
		// Try to unmarshal this individual element.
		var ev Event
		if err := json.Unmarshal(raw, &ev); err != nil {
			resp.Rejected++
			resp.Errors = append(resp.Errors, EventError{
				Index:  i,
				Reason: "malformed JSON: " + err.Error(),
			})
			continue
		}

		// Validate required fields and formats.
		if err := ev.Validate(); err != nil {
			resp.Rejected++
			resp.Errors = append(resp.Errors, EventError{
				Index:   i,
				EventID: ev.EventID,
				Reason:  err.Error(),
			})
			continue
		}

		// Attempt to store the event (dedup check happens inside).
		result := store.AddEvent(ev)
		switch result {
		case ResultAccepted:
			resp.Accepted++
		case ResultDuplicate:
			resp.Duplicates++
		case ResultConflict:
			resp.Rejected++
			resp.Errors = append(resp.Errors, EventError{
				Index:   i,
				EventID: ev.EventID,
				Reason:  "conflicting duplicate: event_id already exists with different data",
			})
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleGetStats returns aggregated stats for one campaign.
func handleGetStats(w http.ResponseWriter, r *http.Request) {
	campaignID := r.PathValue("campaignID")

	stats, ok := store.GetStats(campaignID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "campaign not found: " + campaignID,
		})
		return
	}

	writeJSON(w, http.StatusOK, stats)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}
