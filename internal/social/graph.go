package social

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"qorvexus/internal/types"
)

type Boundary string

const (
	BoundaryOwnerDirect      Boundary = "owner_direct"
	BoundaryTrustedReview    Boundary = "trusted_review"
	BoundaryTrustedAutopilot Boundary = "trusted_autopilot"
	BoundaryExternalReview   Boundary = "external_review"
	BoundaryExternalLimited  Boundary = "external_limited"
)

type InteractionKind string

const (
	InteractionInbound  InteractionKind = "inbound"
	InteractionOutbound InteractionKind = "outbound"
	InteractionOutbox   InteractionKind = "outbox"
)

type Interaction struct {
	Kind        InteractionKind
	Channel     string
	ThreadID    string
	ContactID   string
	ContactName string
	Trust       types.TrustLevel
	Message     string
	OccurredAt  time.Time
}

type ContactNode struct {
	ID               string    `json:"id"`
	Channel          string    `json:"channel"`
	ContactID        string    `json:"contact_id,omitempty"`
	DisplayName      string    `json:"display_name,omitempty"`
	Trust            string    `json:"trust,omitempty"`
	Boundary         Boundary  `json:"boundary"`
	InteractionCount int       `json:"interaction_count"`
	InboundCount     int       `json:"inbound_count,omitempty"`
	OutboundCount    int       `json:"outbound_count,omitempty"`
	OutboxCount      int       `json:"outbox_count,omitempty"`
	LastThreadID     string    `json:"last_thread_id,omitempty"`
	LastMessage      string    `json:"last_message,omitempty"`
	FirstSeenAt      time.Time `json:"first_seen_at"`
	LastInboundAt    time.Time `json:"last_inbound_at,omitempty"`
	LastOutboundAt   time.Time `json:"last_outbound_at,omitempty"`
	LastOutboxAt     time.Time `json:"last_outbox_at,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type RelationshipEdge struct {
	ID               string    `json:"id"`
	From             string    `json:"from"`
	To               string    `json:"to"`
	Kind             string    `json:"kind"`
	Trust            string    `json:"trust,omitempty"`
	Boundary         Boundary  `json:"boundary"`
	InteractionCount int       `json:"interaction_count"`
	InboundCount     int       `json:"inbound_count,omitempty"`
	OutboundCount    int       `json:"outbound_count,omitempty"`
	OutboxCount      int       `json:"outbox_count,omitempty"`
	LastActiveAt     time.Time `json:"last_active_at,omitempty"`
}

type Graph struct {
	Nodes     []ContactNode      `json:"nodes"`
	Edges     []RelationshipEdge `json:"edges"`
	UpdatedAt time.Time          `json:"updated_at"`
}

type GraphStore struct {
	path string
	mu   sync.Mutex
}

func NewGraphStore(path string) *GraphStore {
	return &GraphStore{path: path}
}

func ContactKey(channel string, contactID string, contactName string) string {
	channel = strings.ToLower(strings.TrimSpace(channel))
	contactID = strings.TrimSpace(contactID)
	contactName = strings.TrimSpace(strings.ToLower(contactName))
	switch {
	case channel == "" && contactID == "" && contactName == "":
		return ""
	case contactID != "":
		return channel + ":" + contactID
	default:
		return channel + ":" + strings.ReplaceAll(strings.Join(strings.Fields(contactName), "-"), "--", "-")
	}
}

func DefaultBoundary(trust types.TrustLevel, interactions int) Boundary {
	switch trust {
	case types.TrustOwner, types.TrustSystem:
		return BoundaryOwnerDirect
	case types.TrustTrusted:
		if interactions >= 3 {
			return BoundaryTrustedAutopilot
		}
		return BoundaryTrustedReview
	default:
		if interactions >= 6 {
			return BoundaryExternalLimited
		}
		return BoundaryExternalReview
	}
}

func (s *GraphStore) RecordInteraction(interaction Interaction) (ContactNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	graph, err := s.loadLocked()
	if err != nil {
		return ContactNode{}, err
	}
	now := interaction.OccurredAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	key := ContactKey(interaction.Channel, interaction.ContactID, interaction.ContactName)
	if key == "" {
		return ContactNode{}, fmt.Errorf("contact key is empty")
	}
	node := s.findNodeLocked(&graph, key)
	if node == nil {
		graph.Nodes = append(graph.Nodes, ContactNode{
			ID:          key,
			Channel:     interaction.Channel,
			ContactID:   interaction.ContactID,
			DisplayName: strings.TrimSpace(interaction.ContactName),
			Trust:       string(interaction.Trust),
			Boundary:    DefaultBoundary(interaction.Trust, 0),
			FirstSeenAt: now,
		})
		node = &graph.Nodes[len(graph.Nodes)-1]
	}
	node.Channel = chooseString(interaction.Channel, node.Channel)
	node.ContactID = chooseString(interaction.ContactID, node.ContactID)
	node.DisplayName = chooseString(interaction.ContactName, node.DisplayName)
	if interaction.Trust != "" {
		node.Trust = string(interaction.Trust)
	}
	node.LastThreadID = chooseString(interaction.ThreadID, node.LastThreadID)
	if compacted := compactText(interaction.Message, 240); compacted != "" {
		node.LastMessage = compacted
	}
	node.InteractionCount++
	switch interaction.Kind {
	case InteractionInbound:
		node.InboundCount++
		node.LastInboundAt = now
	case InteractionOutbound:
		node.OutboundCount++
		node.LastOutboundAt = now
	case InteractionOutbox:
		node.OutboxCount++
		node.LastOutboxAt = now
	}
	node.Boundary = DefaultBoundary(parseTrust(node.Trust), node.InteractionCount)
	node.UpdatedAt = now

	edge := s.findEdgeLocked(&graph, "owner", key)
	if edge == nil {
		graph.Edges = append(graph.Edges, RelationshipEdge{
			ID:       fmt.Sprintf("owner->%s", key),
			From:     "owner",
			To:       key,
			Kind:     "owner_contact",
			Trust:    node.Trust,
			Boundary: node.Boundary,
		})
		edge = &graph.Edges[len(graph.Edges)-1]
	}
	edge.Trust = node.Trust
	edge.Boundary = node.Boundary
	edge.InteractionCount++
	switch interaction.Kind {
	case InteractionInbound:
		edge.InboundCount++
	case InteractionOutbound:
		edge.OutboundCount++
	case InteractionOutbox:
		edge.OutboxCount++
	}
	edge.LastActiveAt = now
	graph.UpdatedAt = now
	if err := s.saveLocked(graph); err != nil {
		return ContactNode{}, err
	}
	return *node, nil
}

func (s *GraphStore) Get(contactKey string) (ContactNode, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	graph, err := s.loadLocked()
	if err != nil {
		return ContactNode{}, false, err
	}
	node := s.findNodeLocked(&graph, contactKey)
	if node == nil {
		return ContactNode{}, false, nil
	}
	return *node, true, nil
}

func (s *GraphStore) Snapshot() (Graph, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	graph, err := s.loadLocked()
	if err != nil {
		return Graph{}, err
	}
	sort.Slice(graph.Nodes, func(i, j int) bool {
		if graph.Nodes[i].UpdatedAt.Equal(graph.Nodes[j].UpdatedAt) {
			return graph.Nodes[i].ID < graph.Nodes[j].ID
		}
		return graph.Nodes[i].UpdatedAt.After(graph.Nodes[j].UpdatedAt)
	})
	sort.Slice(graph.Edges, func(i, j int) bool {
		if graph.Edges[i].LastActiveAt.Equal(graph.Edges[j].LastActiveAt) {
			return graph.Edges[i].ID < graph.Edges[j].ID
		}
		return graph.Edges[i].LastActiveAt.After(graph.Edges[j].LastActiveAt)
	})
	return graph, nil
}

func (s *GraphStore) findNodeLocked(graph *Graph, key string) *ContactNode {
	for i := range graph.Nodes {
		if graph.Nodes[i].ID == key {
			return &graph.Nodes[i]
		}
	}
	return nil
}

func (s *GraphStore) findEdgeLocked(graph *Graph, from string, to string) *RelationshipEdge {
	for i := range graph.Edges {
		if graph.Edges[i].From == from && graph.Edges[i].To == to {
			return &graph.Edges[i]
		}
	}
	return nil
}

func (s *GraphStore) loadLocked() (Graph, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return Graph{}, nil
		}
		return Graph{}, err
	}
	if len(raw) == 0 {
		return Graph{}, nil
	}
	var graph Graph
	if err := json.Unmarshal(raw, &graph); err != nil {
		return Graph{}, err
	}
	return graph, nil
}

func (s *GraphStore) saveLocked(graph Graph) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(graph, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0o644)
}

func parseTrust(value string) types.TrustLevel {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(types.TrustOwner):
		return types.TrustOwner
	case string(types.TrustTrusted):
		return types.TrustTrusted
	case string(types.TrustSystem):
		return types.TrustSystem
	default:
		return types.TrustExternal
	}
}

func compactText(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" || limit <= 0 || len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit-3]) + "..."
}
