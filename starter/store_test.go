package main

import (
	"sync"
	"testing"
)

func TestAddEvent_Valid(t *testing.T) {
	s := NewStore()
	ev := Event{
		EventID:    "evt_001",
		CampaignID: "cmp_1",
		ContactID:  "ct_1",
		Type:       "sent",
		Timestamp:  "2026-08-10T06:01:00Z",
	}
	result := s.AddEvent(ev)
	if result != ResultAccepted {
		t.Fatalf("expected ResultAccepted, got %d", result)
	}
	if s.EventCount() != 1 {
		t.Fatalf("expected 1 event, got %d", s.EventCount())
	}
}

func TestAddEvent_ExactDuplicate(t *testing.T) {
	s := NewStore()
	ev := Event{
		EventID:    "evt_001",
		CampaignID: "cmp_1",
		ContactID:  "ct_1",
		Type:       "sent",
		Timestamp:  "2026-08-10T06:01:00Z",
	}
	s.AddEvent(ev)
	result := s.AddEvent(ev)
	if result != ResultDuplicate {
		t.Fatalf("expected ResultDuplicate, got %d", result)
	}
	if s.EventCount() != 1 {
		t.Fatalf("expected 1 event after duplicate, got %d", s.EventCount())
	}
}

func TestAddEvent_ConflictingDuplicate(t *testing.T) {
	s := NewStore()
	ev1 := Event{
		EventID:    "evt_001",
		CampaignID: "cmp_1",
		ContactID:  "ct_1",
		Type:       "sent",
		Timestamp:  "2026-08-10T06:01:00Z",
	}
	ev2 := Event{
		EventID:    "evt_001",
		CampaignID: "cmp_1",
		ContactID:  "ct_1",
		Type:       "clicked", // different type!
		Timestamp:  "2026-08-10T06:01:00Z",
	}
	s.AddEvent(ev1)
	result := s.AddEvent(ev2)
	if result != ResultConflict {
		t.Fatalf("expected ResultConflict, got %d", result)
	}
	if s.EventCount() != 1 {
		t.Fatalf("expected 1 event (original), got %d", s.EventCount())
	}
}

func TestStats_BasicCounts(t *testing.T) {
	s := NewStore()
	events := []Event{
		{EventID: "e1", CampaignID: "cmp_1", ContactID: "ct_1", Type: "sent", Timestamp: "2026-08-10T06:00:00Z"},
		{EventID: "e2", CampaignID: "cmp_1", ContactID: "ct_1", Type: "delivered", Timestamp: "2026-08-10T06:01:00Z"},
		{EventID: "e3", CampaignID: "cmp_1", ContactID: "ct_1", Type: "opened", Timestamp: "2026-08-10T06:02:00Z"},
		{EventID: "e4", CampaignID: "cmp_1", ContactID: "ct_1", Type: "clicked", Timestamp: "2026-08-10T06:03:00Z"},
	}
	for _, ev := range events {
		s.AddEvent(ev)
	}

	stats, ok := s.GetStats("cmp_1")
	if !ok {
		t.Fatal("expected campaign to exist")
	}
	if stats.Sent != 1 {
		t.Errorf("sent: want 1, got %d", stats.Sent)
	}
	if stats.Delivered != 1 {
		t.Errorf("delivered: want 1, got %d", stats.Delivered)
	}
	if stats.Opened != 1 {
		t.Errorf("opened: want 1, got %d", stats.Opened)
	}
	if stats.Clicked != 1 {
		t.Errorf("clicked: want 1, got %d", stats.Clicked)
	}
}

func TestStats_UniqueOpens_SameContact(t *testing.T) {
	s := NewStore()
	// Same contact opens 3 times in same campaign
	events := []Event{
		{EventID: "e1", CampaignID: "cmp_1", ContactID: "ct_A", Type: "opened", Timestamp: "2026-08-10T06:00:00Z"},
		{EventID: "e2", CampaignID: "cmp_1", ContactID: "ct_A", Type: "opened", Timestamp: "2026-08-10T07:00:00Z"},
		{EventID: "e3", CampaignID: "cmp_1", ContactID: "ct_A", Type: "opened", Timestamp: "2026-08-10T08:00:00Z"},
		{EventID: "e4", CampaignID: "cmp_1", ContactID: "ct_B", Type: "opened", Timestamp: "2026-08-10T09:00:00Z"},
	}
	for _, ev := range events {
		s.AddEvent(ev)
	}

	stats, _ := s.GetStats("cmp_1")
	if stats.Opened != 4 {
		t.Errorf("opened: want 4, got %d", stats.Opened)
	}
	if stats.UniqueOpens != 2 {
		t.Errorf("unique_opens: want 2, got %d", stats.UniqueOpens)
	}
}

func TestStats_UniqueOpens_AcrossCampaigns(t *testing.T) {
	s := NewStore()
	// Same contact opens in TWO different campaigns
	events := []Event{
		{EventID: "e1", CampaignID: "cmp_A", ContactID: "ct_1", Type: "opened", Timestamp: "2026-08-10T06:00:00Z"},
		{EventID: "e2", CampaignID: "cmp_B", ContactID: "ct_1", Type: "opened", Timestamp: "2026-08-10T07:00:00Z"},
	}
	for _, ev := range events {
		s.AddEvent(ev)
	}

	statsA, _ := s.GetStats("cmp_A")
	statsB, _ := s.GetStats("cmp_B")

	if statsA.UniqueOpens != 1 {
		t.Errorf("cmp_A unique_opens: want 1, got %d", statsA.UniqueOpens)
	}
	if statsB.UniqueOpens != 1 {
		t.Errorf("cmp_B unique_opens: want 1, got %d", statsB.UniqueOpens)
	}
}

func TestStats_DuplicateDoesNotInflate(t *testing.T) {
	s := NewStore()
	ev := Event{EventID: "e1", CampaignID: "cmp_1", ContactID: "ct_1", Type: "sent", Timestamp: "2026-08-10T06:00:00Z"}
	s.AddEvent(ev)
	s.AddEvent(ev) // duplicate
	s.AddEvent(ev) // duplicate

	stats, _ := s.GetStats("cmp_1")
	if stats.Sent != 1 {
		t.Errorf("sent: want 1, got %d (duplicates inflated count)", stats.Sent)
	}
}

func TestStats_NonexistentCampaign(t *testing.T) {
	s := NewStore()
	_, ok := s.GetStats("cmp_doesnt_exist")
	if ok {
		t.Error("expected campaign to not exist")
	}
}

func TestConcurrentAddEvent(t *testing.T) {
	s := NewStore()
	const numGoroutines = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			// All goroutines try to add the same event.
			s.AddEvent(Event{
				EventID:    "evt_concurrent",
				CampaignID: "cmp_1",
				ContactID:  "ct_1",
				Type:       "sent",
				Timestamp:  "2026-08-10T06:00:00Z",
			})
		}()
	}
	wg.Wait()

	// Despite 100 concurrent adds, only 1 should be stored.
	if s.EventCount() != 1 {
		t.Errorf("expected 1 event, got %d", s.EventCount())
	}
	stats, _ := s.GetStats("cmp_1")
	if stats.Sent != 1 {
		t.Errorf("sent: want 1, got %d", stats.Sent)
	}
}

func TestOutOfOrderEvents(t *testing.T) {
	s := NewStore()
	// Events arrive out of order: opened before sent
	events := []Event{
		{EventID: "e3", CampaignID: "cmp_1", ContactID: "ct_1", Type: "opened", Timestamp: "2026-08-10T09:00:00Z"},
		{EventID: "e2", CampaignID: "cmp_1", ContactID: "ct_1", Type: "delivered", Timestamp: "2026-08-10T07:00:00Z"},
		{EventID: "e1", CampaignID: "cmp_1", ContactID: "ct_1", Type: "sent", Timestamp: "2026-08-10T06:00:00Z"},
	}
	for _, ev := range events {
		s.AddEvent(ev)
	}

	stats, _ := s.GetStats("cmp_1")
	if stats.Sent != 1 || stats.Delivered != 1 || stats.Opened != 1 {
		t.Errorf("out-of-order failed: sent=%d delivered=%d opened=%d",
			stats.Sent, stats.Delivered, stats.Opened)
	}
}
