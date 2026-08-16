package main

import (
	"fmt"
	"sync"
)

// Store is a concurrency-safe in-memory event store.
// It tracks events by event_id for deduplication and maintains
// per-campaign counters for statistics.
type Store struct {
	mu     sync.RWMutex
	events map[string]Event // keyed by event_id

	// Per-campaign stats: sent, delivered, opened, clicked counts
	// and unique openers set.
	campaignCounts map[string]*campaignData
}

type campaignData struct {
	sent        int
	delivered   int
	opened      int
	clicked     int
	uniqueOpens map[string]bool // set of contact_ids that have opened
}

// CampaignStats is the read-only view returned by GetStats.
type CampaignStats struct {
	CampaignID  string `json:"campaign_id"`
	Sent        int    `json:"sent"`
	Delivered   int    `json:"delivered"`
	Opened      int    `json:"opened"`
	Clicked     int    `json:"clicked"`
	UniqueOpens int    `json:"unique_opens"`
}

// NewStore creates an empty Store.
func NewStore() *Store {
	return &Store{
		events:         make(map[string]Event),
		campaignCounts: make(map[string]*campaignData),
	}
}

// AddEventResult describes what happened when we tried to add an event.
type AddEventResult int

const (
	ResultAccepted  AddEventResult = iota
	ResultDuplicate                // exact same event_id with identical data
	ResultConflict                 // same event_id but different data
)

// AddEvent attempts to store an event. It returns the result of the attempt.
// This method is safe for concurrent use.
func (s *Store) AddEvent(ev Event) AddEventResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if we already have this event_id.
	if existing, ok := s.events[ev.EventID]; ok {
		// Same event_id exists. Is it an exact duplicate or a conflict?
		if existing.CampaignID == ev.CampaignID &&
			existing.ContactID == ev.ContactID &&
			existing.Type == ev.Type &&
			existing.Timestamp == ev.Timestamp {
			return ResultDuplicate
		}
		return ResultConflict
	}

	// Store the event.
	s.events[ev.EventID] = ev

	// Update campaign counters.
	cd := s.getOrCreateCampaign(ev.CampaignID)
	switch ev.Type {
	case "sent":
		cd.sent++
	case "delivered":
		cd.delivered++
	case "opened":
		cd.opened++
		// Track unique opens per campaign.
		cd.uniqueOpens[ev.ContactID] = true
	case "clicked":
		cd.clicked++
	}

	return ResultAccepted
}

// getOrCreateCampaign returns (or creates) the campaign data for the given ID.
// Must be called with s.mu held.
func (s *Store) getOrCreateCampaign(campaignID string) *campaignData {
	cd, ok := s.campaignCounts[campaignID]
	if !ok {
		cd = &campaignData{
			uniqueOpens: make(map[string]bool),
		}
		s.campaignCounts[campaignID] = cd
	}
	return cd
}

// GetStats returns statistics for a campaign.
// Returns the stats and true if the campaign exists, or zero stats and false otherwise.
func (s *Store) GetStats(campaignID string) (CampaignStats, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cd, ok := s.campaignCounts[campaignID]
	if !ok {
		return CampaignStats{}, false
	}

	return CampaignStats{
		CampaignID:  campaignID,
		Sent:        cd.sent,
		Delivered:   cd.delivered,
		Opened:      cd.opened,
		Clicked:     cd.clicked,
		UniqueOpens: len(cd.uniqueOpens),
	}, true
}

// EventCount returns the total number of stored events (for testing).
func (s *Store) EventCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.events)
}

// HasEvent checks if an event_id is already stored (for testing).
func (s *Store) HasEvent(eventID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.events[eventID]
	return ok
}

// String returns a debug-friendly representation showing campaign names.
func (s *Store) String() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fmt.Sprintf("Store{events=%d, campaigns=%d}", len(s.events), len(s.campaignCounts))
}
