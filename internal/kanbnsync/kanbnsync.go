// Package kanbnsync mirrors the WMS kanban board (internal/lego.DB.KanbanBoardAll)
// into a self-hosted Kan.bn board (https://kan.bn), one way only. Kan.bn is a
// nicer, embeddable display layer -- it is never consulted as a source of
// truth, and nothing here ever reads Kan.bn's own webhooks or lets a card
// drag there change a real ticket. The TUI's own kanban screens already
// document this same read-only relationship for the existing SSE board (see
// internal/uiapp/kanban_screens.go); this package extends it to a second,
// prettier renderer rather than replacing it.
//
// A ticket's real identity (Ticket.ID) is embedded as a hidden marker in
// each Kan.bn card's description, so a process restart can rediscover which
// Kan.bn card already represents which ticket (via seedFromExistingCards)
// without a second persistent store of its own.
package kanbnsync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

const pollInterval = 10 * time.Second

func marker(ticketID int64) string { return fmt.Sprintf("[wms:ticket:%d]", ticketID) }

func ticketIDFromMarker(description string) (int64, bool) {
	start := strings.Index(description, "[wms:ticket:")
	if start == -1 {
		return 0, false
	}
	end := strings.Index(description[start:], "]")
	if end == -1 {
		return 0, false
	}
	var id int64
	if _, err := fmt.Sscanf(description[start:start+end+1], "[wms:ticket:%d]", &id); err != nil {
		return 0, false
	}
	return id, true
}

type client struct {
	http    *http.Client
	baseURL string
	apiKey  string
}

func newClient() *client {
	return &client{
		http:    &http.Client{Timeout: 10 * time.Second},
		baseURL: strings.TrimSuffix(config.Get(config.KanbnBaseURL), "/"),
		apiKey:  config.Get(config.KanbnAPIKey),
	}
}

func (c *client) do(method, path string, body any, out any) error {
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var errBody bytes.Buffer
		_, _ = errBody.ReadFrom(resp.Body)
		return fmt.Errorf("kanbn API %s %s: %d %s", method, path, resp.StatusCode, errBody.String())
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

type cardState struct {
	cardID string
	listID string
}

// Syncer holds the in-memory ticket-ID -> Kan.bn-card mapping. Safe for
// concurrent use, though in practice only the single poll loop in Run calls it.
type Syncer struct {
	client *client
	listID map[string]string // lego.KanbanCol* -> Kan.bn list publicId
	label  map[bool]string   // Abandoned -> Kan.bn label publicId (best-effort; "" if not configured)

	mu     sync.Mutex
	known  map[int64]cardState
	seeded bool
}

func NewSyncer() *Syncer {
	return &Syncer{
		client: newClient(),
		listID: map[string]string{
			lego.KanbanColBacklog: config.Get(config.KanbnListBacklog),
			lego.KanbanColActive:  config.Get(config.KanbnListActive),
			lego.KanbanColDone:    config.Get(config.KanbnListDone),
		},
		known: map[int64]cardState{},
	}
}

// Configured reports whether every setting Sync needs is present -- callers
// use this to skip the poll loop entirely rather than fail on every tick.
func (s *Syncer) Configured() bool {
	return s.client.apiKey != "" && s.listID[lego.KanbanColBacklog] != "" && s.listID[lego.KanbanColActive] != "" && s.listID[lego.KanbanColDone] != ""
}

type kanbnBoard struct {
	Lists []struct {
		PublicID string `json:"publicId"`
		Cards    []struct {
			PublicID    string `json:"publicId"`
			Description string `json:"description"`
		} `json:"cards"`
	} `json:"lists"`
}

// seedFromExistingCards rebuilds s.known from Kan.bn's own board state --
// called once, lazily, so a process restart doesn't create duplicate cards
// for tickets Kan.bn already has (matched via each card's hidden marker).
func (s *Syncer) seedFromExistingCards() {
	boardID := config.Get(config.KanbnBoardID)
	if boardID == "" {
		return
	}
	var board kanbnBoard
	if err := s.client.do(http.MethodGet, "/boards/"+boardID, nil, &board); err != nil {
		log.Printf("kanbnsync: seeding from existing cards: %v", err)
		return
	}
	for _, list := range board.Lists {
		for _, card := range list.Cards {
			if id, ok := ticketIDFromMarker(card.Description); ok {
				s.known[id] = cardState{cardID: card.PublicID, listID: list.PublicID}
			}
		}
	}
}

// Sync pushes cards into Kan.bn to match the given snapshot -- creates any
// ticket Kan.bn doesn't know about yet, moves any whose column changed, and
// otherwise leaves Kan.bn alone. It never deletes a Kan.bn card: a ticket
// aging out of "done" on the WMS side just leaves Kan.bn showing its last
// known state, same as the existing SSE board's own history has no "forget"
// path either.
func (s *Syncer) Sync(cards []lego.KanbanCard) {
	if !s.Configured() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.seeded {
		s.seedFromExistingCards()
		s.seeded = true
	}
	for _, card := range cards {
		listID := s.listID[card.Column]
		if listID == "" {
			continue
		}
		if state, ok := s.known[card.Ticket.ID]; ok {
			if state.listID != listID {
				if err := s.move(state.cardID, listID); err != nil {
					log.Printf("kanbnsync: moving card for ticket %d: %v", card.Ticket.ID, err)
					continue
				}
				s.known[card.Ticket.ID] = cardState{cardID: state.cardID, listID: listID}
			}
			continue
		}
		cardID, err := s.create(card, listID)
		if err != nil {
			log.Printf("kanbnsync: creating card for ticket %d: %v", card.Ticket.ID, err)
			continue
		}
		s.known[card.Ticket.ID] = cardState{cardID: cardID, listID: listID}
	}
}

func title(card lego.KanbanCard) string {
	if card.Ticket.Label != "" {
		return card.Ticket.Label
	}
	return fmt.Sprintf("%s %s", card.Ticket.Kind, card.Ticket.Target)
}

func description(card lego.KanbanCard) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s\n", strings.ToUpper(card.Ticket.Kind[:1])+card.Ticket.Kind[1:], card.Ticket.Target)
	if card.Ticket.AssignedTo != "" {
		fmt.Fprintf(&b, "Assigned: %s\n", card.Ticket.AssignedTo)
	}
	if card.Summary != "" {
		fmt.Fprintf(&b, "Summary: %s\n", card.Summary)
	}
	if card.Ticket.Priority != "" {
		fmt.Fprintf(&b, "Priority: %s\n", card.Ticket.Priority)
	}
	fmt.Fprintf(&b, "\n%s", marker(card.Ticket.ID))
	return b.String()
}

func (s *Syncer) create(card lego.KanbanCard, listID string) (string, error) {
	req := map[string]any{
		"title":           title(card),
		"description":     description(card),
		"listPublicId":    listID,
		"labelPublicIds":  s.labelsFor(card),
		"memberPublicIds": []string{},
		"position":        "end",
	}
	var resp struct {
		PublicID string `json:"publicId"`
	}
	if err := s.client.do(http.MethodPost, "/cards", req, &resp); err != nil {
		return "", err
	}
	return resp.PublicID, nil
}

func (s *Syncer) labelsFor(card lego.KanbanCard) []string {
	if card.Abandoned && s.label[true] != "" {
		return []string{s.label[true]}
	}
	return []string{}
}

func (s *Syncer) move(cardID, listID string) error {
	req := map[string]any{"listPublicId": listID, "index": 0}
	return s.client.do(http.MethodPut, "/cards/"+cardID, req, nil)
}

// Run polls board() every pollInterval and syncs the result into Kan.bn,
// until ctx is cancelled. A no-op (returns nil immediately) when Kan.bn
// syncing isn't configured, so callers can always start it unconditionally.
func Run(ctx context.Context, board func() ([]lego.KanbanCard, error)) error {
	s := NewSyncer()
	if !s.Configured() {
		return nil
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		cards, err := board()
		if err != nil {
			log.Printf("kanbnsync: fetching the WMS kanban board: %v", err)
		} else {
			s.Sync(cards)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
