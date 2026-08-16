package main

import (
	"fmt"
	"time"
)

// Event is one webhook event from a message provider.
type Event struct {
	EventID    string            `json:"event_id"`
	CampaignID string            `json:"campaign_id"`
	ContactID  string            `json:"contact_id"`
	Type       string            `json:"type"`
	Timestamp  string            `json:"timestamp"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// validEventTypes is the set of accepted event type values.
var validEventTypes = map[string]bool{
	"sent":      true,
	"delivered": true,
	"opened":    true,
	"clicked":   true,
}

// Validate checks that all required fields are present and correct.
// Returns nil if the event is valid, or an error describing the problem.
func (e *Event) Validate() error {
	if e.EventID == "" {
		return fmt.Errorf("missing event_id")
	}
	if e.CampaignID == "" {
		return fmt.Errorf("missing campaign_id")
	}
	if e.ContactID == "" {
		return fmt.Errorf("missing contact_id")
	}
	if !validEventTypes[e.Type] {
		return fmt.Errorf("invalid event type: %q", e.Type)
	}
	if e.Timestamp == "" {
		return fmt.Errorf("missing timestamp")
	}
	if _, err := time.Parse(time.RFC3339, e.Timestamp); err != nil {
		return fmt.Errorf("invalid timestamp: %q", e.Timestamp)
	}
	return nil
}

// BatchResponse is the response shape for POST /events.
type BatchResponse struct {
	Accepted   int           `json:"accepted"`
	Duplicates int           `json:"duplicates"`
	Rejected   int           `json:"rejected"`
	Errors     []EventError  `json:"errors,omitempty"`
}

// EventError describes why a specific event was rejected.
type EventError struct {
	Index   int    `json:"index"`
	EventID string `json:"event_id,omitempty"`
	Reason  string `json:"reason"`
}
