package memory

import (
	"bufio"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"qorvexus/internal/model"
)

const (
	EntryStateActive     = "active"
	EntryStateSummary    = "summary"
	EntryStateSuperseded = "superseded"
	EntryStateArchived   = "archived"
	EntryStateCompacted  = "compacted"
	EntryStateDisputed   = "disputed"
)

type Entry struct {
	ID             string    `json:"id"`
	Key            string    `json:"key,omitempty"`
	Layer          string    `json:"layer,omitempty"`
	Area           string    `json:"area,omitempty"`
	Kind           string    `json:"kind,omitempty"`
	Subject        string    `json:"subject,omitempty"`
	Summary        string    `json:"summary,omitempty"`
	Content        string    `json:"content"`
	Source         string    `json:"source,omitempty"`
	Tags           []string  `json:"tags,omitempty"`
	Importance     int       `json:"importance,omitempty"`
	Confidence     float64   `json:"confidence,omitempty"`
	AccessCount    int       `json:"access_count,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
	LastAccessedAt time.Time `json:"last_accessed_at,omitempty"`
	Archived       bool      `json:"archived,omitempty"`
	State          string    `json:"state,omitempty"`
	ConflictKey    string    `json:"conflict_key,omitempty"`
	ConflictStatus string    `json:"conflict_status,omitempty"`
	SupersededBy   string    `json:"superseded_by,omitempty"`
	RelatedIDs     []string  `json:"related_ids,omitempty"`
	SourceCount    int       `json:"source_count,omitempty"`
	LocalEmbedding []float64 `json:"local_embedding,omitempty"`
	Embedding      []float64 `json:"embedding,omitempty"`
	EmbeddingModel string    `json:"embedding_model,omitempty"`
}

type SearchOptions struct {
	Query             string
	Limit             int
	Areas             []string
	Layers            []string
	Kinds             []string
	Subjects          []string
	Tags              []string
	IncludeArchived   bool
	IncludeSuperseded bool
	IncludeSummaries  bool
}

type Options struct {
	Path                string
	Models              *model.Registry
	EmbeddingModel      string
	SummaryModel        string
	SemanticSearch      bool
	CompactionThreshold int
	CompactionRetain    int
	MaxSummarySources   int
	EmbeddingTimeout    time.Duration
	SummaryTimeout      time.Duration
}

type Store struct {
	path string
	opts Options
	mu   sync.Mutex
}

type persistedState struct {
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	Entries   []Entry   `json:"entries"`
}

func NewStore(path string) *Store {
	return NewStoreWithOptions(Options{
		Path:                path,
		SemanticSearch:      true,
		CompactionThreshold: 6,
		CompactionRetain:    3,
		MaxSummarySources:   6,
		EmbeddingTimeout:    12 * time.Second,
		SummaryTimeout:      20 * time.Second,
	})
}

func NewStoreWithOptions(opts Options) *Store {
	if opts.CompactionThreshold <= 0 {
		opts.CompactionThreshold = 6
	}
	if opts.CompactionRetain <= 0 {
		opts.CompactionRetain = 3
	}
	if opts.MaxSummarySources <= 0 {
		opts.MaxSummarySources = 6
	}
	if opts.EmbeddingTimeout <= 0 {
		opts.EmbeddingTimeout = 12 * time.Second
	}
	if opts.SummaryTimeout <= 0 {
		opts.SummaryTimeout = 20 * time.Second
	}
	if opts.Path == "" {
		opts.Path = "memory.jsonl"
	}
	return &Store{path: opts.Path, opts: opts}
}

func (e Entry) HasTag(tag string) bool {
	for _, item := range e.Tags {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(tag)) {
			return true
		}
	}
	return false
}

func (e Entry) MatchArea(area string) bool {
	return strings.EqualFold(strings.TrimSpace(e.Area), strings.TrimSpace(area))
}

func (e Entry) MatchLayer(layer string) bool {
	return strings.EqualFold(strings.TrimSpace(e.Layer), strings.TrimSpace(layer))
}

func (e Entry) MatchKind(kind string) bool {
	return strings.EqualFold(strings.TrimSpace(e.Kind), strings.TrimSpace(kind))
}

func (s *Store) Append(entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.loadEntriesLocked()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	entry, err = s.prepareEntry(entry, now)
	if err != nil {
		return err
	}
	entries = append(entries, entry)
	entries, err = s.reconcileEntries(entries, now)
	if err != nil {
		return err
	}
	return s.saveEntriesLocked(entries, now)
}

func (s *Store) Upsert(entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.loadEntriesLocked()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	entry, err = s.prepareEntry(entry, now)
	if err != nil {
		return err
	}
	if idx := findMergeCandidate(entries, entry); idx >= 0 {
		entries[idx] = mergeEntries(entries[idx], entry, now)
	} else {
		entries = append(entries, entry)
	}
	entries, err = s.reconcileEntries(entries, now)
	if err != nil {
		return err
	}
	return s.saveEntriesLocked(entries, now)
}

func (s *Store) Search(query string, limit int) ([]Entry, error) {
	return s.SearchWithOptions(SearchOptions{Query: query, Limit: limit})
}

func (s *Store) SearchWithOptions(opts SearchOptions) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.loadEntriesLocked()
	if err != nil {
		return nil, err
	}
	terms := tokenize(opts.Query)
	localQuery := localSemanticEmbedding(opts.Query)
	remoteQuery, remoteModel, _ := s.embedQuery(opts.Query)

	type scored struct {
		entry      Entry
		score      float64
		queryScore float64
	}
	results := make([]scored, 0, len(entries))
	for _, entry := range entries {
		if !matchesFilters(entry, opts) {
			continue
		}
		total, queryScore := scoreEntry(entry, terms, localQuery, remoteQuery, remoteModel)
		if strings.TrimSpace(opts.Query) != "" && queryScore <= 0.2 {
			continue
		}
		results = append(results, scored{entry: entry, score: total, queryScore: queryScore})
	}
	sort.Slice(results, func(i, j int) bool {
		if math.Abs(results[i].score-results[j].score) < 0.001 {
			return effectiveTime(results[i].entry).After(effectiveTime(results[j].entry))
		}
		return results[i].score > results[j].score
	})
	limit := opts.Limit
	if limit <= 0 || limit > len(results) {
		limit = len(results)
	}
	out := make([]Entry, 0, limit)
	for _, item := range results[:limit] {
		out = append(out, item.entry)
	}
	return out, nil
}

func (s *Store) SearchByTag(tag string, limit int) ([]Entry, error) {
	return s.SearchWithOptions(SearchOptions{
		Limit: limit,
		Tags:  []string{tag},
	})
}

func (s *Store) SearchByArea(area string, limit int) ([]Entry, error) {
	return s.SearchWithOptions(SearchOptions{
		Limit: limit,
		Areas: []string{area},
	})
}

func (s *Store) MarkAccessed(ids ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(ids) == 0 {
		return nil
	}
	lookup := map[string]struct{}{}
	for _, id := range ids {
		if strings.TrimSpace(id) != "" {
			lookup[id] = struct{}{}
		}
	}
	entries, err := s.loadEntriesLocked()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	changed := false
	for i := range entries {
		if _, ok := lookup[entries[i].ID]; ok {
			entries[i].AccessCount++
			entries[i].LastAccessedAt = now
			entries[i].UpdatedAt = now
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.saveEntriesLocked(entries, now)
}

func (s *Store) loadEntriesLocked() ([]Entry, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return decodeEntries(raw)
}

func decodeEntries(raw []byte) ([]Entry, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, nil
	}
	switch trimmed[0] {
	case '{':
		var state persistedState
		if err := json.Unmarshal([]byte(trimmed), &state); err != nil {
			return nil, err
		}
		return normalizeLoadedEntries(state.Entries), nil
	case '[':
		var entries []Entry
		if err := json.Unmarshal([]byte(trimmed), &entries); err != nil {
			return nil, err
		}
		return normalizeLoadedEntries(entries), nil
	default:
		scanner := bufio.NewScanner(strings.NewReader(trimmed))
		entries := []Entry{}
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var entry Entry
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				continue
			}
			entries = append(entries, entry)
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		return normalizeLoadedEntries(entries), nil
	}
}

func normalizeLoadedEntries(entries []Entry) []Entry {
	now := time.Now().UTC()
	out := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, normalizeEntry(entry, now, true))
	}
	return out
}

func (s *Store) saveEntriesLocked(entries []Entry, now time.Time) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool {
		return effectiveTime(entries[i]).Before(effectiveTime(entries[j]))
	})
	state := persistedState{
		Version:   2,
		UpdatedAt: now,
		Entries:   entries,
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(raw, '\n'), 0o644)
}

func (s *Store) prepareEntry(entry Entry, now time.Time) (Entry, error) {
	entry = normalizeEntry(entry, now, true)
	if entry.State == "" {
		entry.State = EntryStateActive
	}
	entry.Layer = deriveLayer(entry)
	entry.ConflictKey = deriveConflictKey(entry)
	entry.RelatedIDs = dedupeStrings(entry.RelatedIDs)
	searchText := entrySearchText(entry)
	entry.LocalEmbedding = localSemanticEmbedding(searchText)
	embedding, modelName, err := s.embedText(searchText)
	if err == nil && len(embedding) > 0 {
		entry.Embedding = embedding
		entry.EmbeddingModel = modelName
	}
	return normalizeEntry(entry, now, true), nil
}

func (s *Store) reconcileEntries(entries []Entry, now time.Time) ([]Entry, error) {
	entries = dedupeStoreEntries(entries, now)
	entries = arbitrateEntries(entries, now)
	entries, err := s.compactEntries(entries, now)
	if err != nil {
		return nil, err
	}
	return dedupeStoreEntries(entries, now), nil
}

func dedupeStoreEntries(entries []Entry, now time.Time) []Entry {
	byStableIdentity := map[string]int{}
	out := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		entry = normalizeEntry(entry, now, true)
		identity := duplicateIdentity(entry)
		if identity != "" {
			if idx, ok := byStableIdentity[identity]; ok {
				out[idx] = mergeEntries(out[idx], entry, now)
				continue
			}
			byStableIdentity[identity] = len(out)
		}
		out = append(out, entry)
	}
	return out
}

func duplicateIdentity(entry Entry) string {
	if entry.ID != "" {
		return "id:" + strings.ToLower(entry.ID)
	}
	contentKey := normalizeFactValue(entry.Content)
	if entry.Key != "" && contentKey != "" {
		return "key:" + strings.ToLower(entry.Key) + ":" + contentKey
	}
	if contentKey != "" {
		return strings.ToLower(strings.Join([]string{entry.Layer, entry.Area, entry.Kind, entry.Subject, contentKey}, "|"))
	}
	return ""
}

func findMergeCandidate(entries []Entry, incoming Entry) int {
	incomingContent := normalizeFactValue(incoming.Content)
	for i := range entries {
		if incoming.ID != "" && entries[i].ID == incoming.ID {
			return i
		}
		if incoming.Key != "" && entries[i].Key != "" && strings.EqualFold(incoming.Key, entries[i].Key) {
			if incomingContent != "" && incomingContent == normalizeFactValue(entries[i].Content) {
				return i
			}
			continue
		}
		if incomingContent != "" && strings.EqualFold(entries[i].Layer, incoming.Layer) &&
			strings.EqualFold(entries[i].Area, incoming.Area) &&
			strings.EqualFold(entries[i].Kind, incoming.Kind) &&
			strings.EqualFold(entries[i].Subject, incoming.Subject) &&
			incomingContent == normalizeFactValue(entries[i].Content) {
			return i
		}
	}
	return -1
}

func arbitrateEntries(entries []Entry, now time.Time) []Entry {
	groups := map[string][]int{}
	arbitrationNotes := map[string]int{}
	for i := range entries {
		entry := entries[i]
		if entry.Kind == "memory_arbitration" && entry.Key != "" {
			arbitrationNotes[strings.ToLower(entry.Key)] = i
			continue
		}
		if !shouldArbitrate(entry) {
			continue
		}
		if inactiveEntry(entry) && entry.State != EntryStateDisputed {
			continue
		}
		if strings.TrimSpace(entry.ConflictKey) == "" {
			continue
		}
		groups[strings.ToLower(entry.ConflictKey)] = append(groups[strings.ToLower(entry.ConflictKey)], i)
	}

	for group, indexes := range groups {
		if len(indexes) < 2 {
			continue
		}
		sort.Slice(indexes, func(i, j int) bool {
			left := entries[indexes[i]]
			right := entries[indexes[j]]
			lScore := arbitrationScore(left)
			rScore := arbitrationScore(right)
			if math.Abs(lScore-rScore) < 0.001 {
				return effectiveTime(left).After(effectiveTime(right))
			}
			return lScore > rScore
		})

		winner := indexes[0]
		unresolved := false
		if len(indexes) > 1 {
			diff := arbitrationScore(entries[indexes[0]]) - arbitrationScore(entries[indexes[1]])
			firstValue := normalizeFactValue(entries[indexes[0]].Content)
			secondValue := normalizeFactValue(entries[indexes[1]].Content)
			unresolved = firstValue != secondValue && diff < 1.25
			if sharesExplicitKey(entries, indexes) {
				unresolved = false
			}
		}

		for pos, idx := range indexes {
			entry := entries[idx]
			if pos == 0 {
				entry.Archived = false
				entry.SupersededBy = ""
				if unresolved {
					entry.State = EntryStateDisputed
					entry.ConflictStatus = "unresolved"
				} else {
					entry.State = EntryStateActive
					entry.ConflictStatus = "resolved"
				}
			} else {
				if unresolved {
					entry.Archived = false
					entry.SupersededBy = ""
					entry.State = EntryStateDisputed
					entry.ConflictStatus = "unresolved"
				} else {
					entry.Archived = true
					entry.SupersededBy = entries[winner].ID
					entry.State = EntryStateSuperseded
					entry.ConflictStatus = "resolved"
				}
			}
			entry.UpdatedAt = now
			entries[idx] = entry
		}

		note := buildArbitrationNote(group, entries, indexes, winner, unresolved, now)
		noteKey := strings.ToLower(note.Key)
		if idx, ok := arbitrationNotes[noteKey]; ok {
			note.ID = entries[idx].ID
			if note.CreatedAt.IsZero() {
				note.CreatedAt = entries[idx].CreatedAt
			}
			entries[idx] = mergeEntries(entries[idx], note, now)
		} else {
			arbitrationNotes[noteKey] = len(entries)
			entries = append(entries, note)
		}
	}
	return entries
}

func buildArbitrationNote(group string, entries []Entry, indexes []int, winner int, unresolved bool, now time.Time) Entry {
	samples := make([]string, 0, len(indexes))
	related := make([]string, 0, len(indexes))
	for _, idx := range indexes {
		item := entries[idx]
		related = append(related, item.ID)
		label := compact(preferString(item.Summary, item.Content), 120)
		if label != "" {
			samples = append(samples, label)
		}
	}
	outcome := "resolved"
	content := fmt.Sprintf("Memory arbitration %s for %s. Canonical memory: %s.", outcome, group, compact(preferString(entries[winner].Summary, entries[winner].Content), 140))
	if unresolved {
		outcome = "unresolved"
		content = fmt.Sprintf("Memory arbitration %s for %s. Conflicting memories remain active until stronger evidence arrives. Candidates: %s.", outcome, group, strings.Join(samples, " | "))
	}
	return normalizeEntry(Entry{
		Key:            stableKey("memory", "arbitration", group),
		Layer:          "workflow",
		Area:           "workflow",
		Kind:           "memory_arbitration",
		Subject:        group,
		Summary:        compact(content, 160),
		Content:        content,
		Source:         "memory:arbitration",
		Tags:           []string{"memory", "arbitration", "workflow"},
		Importance:     4,
		Confidence:     0.7,
		State:          EntryStateSummary,
		ConflictStatus: outcome,
		RelatedIDs:     related,
		SourceCount:    len(indexes),
	}, now, true)
}

func (s *Store) compactEntries(entries []Entry, now time.Time) ([]Entry, error) {
	if s.opts.CompactionThreshold <= 0 {
		return entries, nil
	}
	summaryIndexes := map[string]int{}
	for i, entry := range entries {
		if strings.EqualFold(entry.Kind, "summary") && entry.Key != "" {
			summaryIndexes[strings.ToLower(entry.Key)] = i
		}
	}

	groups := map[string][]int{}
	for i, entry := range entries {
		if !shouldCompact(entry) {
			continue
		}
		if inactiveEntry(entry) {
			continue
		}
		groups[summaryGroupKey(entry)] = append(groups[summaryGroupKey(entry)], i)
	}

	for group, indexes := range groups {
		if len(indexes) <= s.opts.CompactionThreshold {
			continue
		}
		sort.Slice(indexes, func(i, j int) bool {
			return effectiveTime(entries[indexes[i]]).After(effectiveTime(entries[indexes[j]]))
		})
		retain := minInt(len(indexes), s.opts.CompactionRetain)
		if retain >= len(indexes) {
			continue
		}
		compactIndexes := indexes[retain:]
		if len(compactIndexes) < 2 {
			continue
		}
		summaryKey := stableKey("memory", "summary", group)
		sources := make([]Entry, 0, len(compactIndexes)+1)
		if idx, ok := summaryIndexes[strings.ToLower(summaryKey)]; ok {
			sources = append(sources, entries[idx])
		}
		for _, idx := range compactIndexes {
			sources = append(sources, entries[idx])
		}
		summaryEntry, err := s.buildSummaryEntry(summaryKey, group, sources, now)
		if err != nil {
			return nil, err
		}
		if idx, ok := summaryIndexes[strings.ToLower(summaryKey)]; ok {
			summaryEntry.ID = entries[idx].ID
			if summaryEntry.CreatedAt.IsZero() {
				summaryEntry.CreatedAt = entries[idx].CreatedAt
			}
			entries[idx] = mergeEntries(entries[idx], summaryEntry, now)
		} else {
			summaryIndexes[strings.ToLower(summaryKey)] = len(entries)
			entries = append(entries, summaryEntry)
		}
		summaryID := entries[summaryIndexes[strings.ToLower(summaryKey)]].ID
		for _, idx := range compactIndexes {
			entries[idx].Archived = true
			entries[idx].State = EntryStateCompacted
			entries[idx].UpdatedAt = now
			entries[idx].RelatedIDs = dedupeStrings(append(entries[idx].RelatedIDs, summaryID))
		}
	}
	return entries, nil
}

func (s *Store) buildSummaryEntry(summaryKey string, group string, sources []Entry, now time.Time) (Entry, error) {
	layer, area, subject := parseSummaryGroupKey(group)
	summaryText, contentText := s.summarizeEntries(layer, area, subject, sources)
	entry := Entry{
		Key:         summaryKey,
		Layer:       layer,
		Area:        area,
		Kind:        "summary",
		Subject:     subject,
		Summary:     summaryText,
		Content:     contentText,
		Source:      "memory:compaction",
		Tags:        []string{"memory_summary", "layer:" + layer, "area:" + area},
		Importance:  6,
		Confidence:  0.72,
		State:       EntryStateSummary,
		RelatedIDs:  collectIDs(sources),
		SourceCount: countSources(sources),
	}
	return s.prepareEntry(entry, now)
}

func normalizeEntry(entry Entry, now time.Time, preserveID bool) Entry {
	if strings.TrimSpace(entry.ID) == "" && !preserveID {
		entry.ID = newEntryID(entry, now)
	}
	if strings.TrimSpace(entry.ID) == "" {
		entry.ID = newEntryID(entry, now)
	}
	entry.Key = strings.TrimSpace(entry.Key)
	entry.Layer = strings.TrimSpace(entry.Layer)
	entry.Area = strings.TrimSpace(entry.Area)
	entry.Kind = strings.TrimSpace(entry.Kind)
	entry.Subject = strings.TrimSpace(entry.Subject)
	entry.Content = strings.TrimSpace(entry.Content)
	entry.Summary = strings.TrimSpace(entry.Summary)
	entry.Source = strings.TrimSpace(entry.Source)
	entry.State = strings.TrimSpace(entry.State)
	entry.ConflictKey = strings.TrimSpace(entry.ConflictKey)
	entry.ConflictStatus = strings.TrimSpace(entry.ConflictStatus)
	entry.SupersededBy = strings.TrimSpace(entry.SupersededBy)
	entry.Tags = dedupeTags(entry.Tags)
	entry.RelatedIDs = dedupeStrings(entry.RelatedIDs)
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	if entry.UpdatedAt.IsZero() {
		entry.UpdatedAt = entry.CreatedAt
	}
	if entry.Layer == "" {
		entry.Layer = deriveLayer(entry)
	}
	if entry.Summary == "" {
		entry.Summary = compact(entry.Content, 200)
	}
	if entry.State == "" {
		entry.State = EntryStateActive
	}
	if entry.Importance < 0 {
		entry.Importance = 0
	}
	if entry.Importance > 10 {
		entry.Importance = 10
	}
	if entry.Confidence < 0 {
		entry.Confidence = 0
	}
	if entry.Confidence > 1 {
		entry.Confidence = 1
	}
	switch entry.State {
	case EntryStateActive, EntryStateSummary, EntryStateDisputed:
		entry.Archived = false
	case EntryStateArchived, EntryStateCompacted, EntryStateSuperseded:
		entry.Archived = true
	default:
		if entry.Archived {
			entry.State = EntryStateArchived
		}
	}
	if entry.ConflictKey == "" {
		entry.ConflictKey = deriveConflictKey(entry)
	}
	return entry
}

func newEntryID(entry Entry, now time.Time) string {
	fingerprint := HashKey(strings.Join([]string{
		entry.Key,
		entry.Layer,
		entry.Area,
		entry.Kind,
		entry.Subject,
		entry.Summary,
		entry.Content,
		entry.Source,
	}, "|"))
	if len(fingerprint) > 10 {
		fingerprint = fingerprint[:10]
	}
	return fmt.Sprintf("mem-%d-%s", now.UnixNano(), fingerprint)
}

func mergeEntries(existing Entry, incoming Entry, now time.Time) Entry {
	out := existing
	out.Key = preferString(incoming.Key, existing.Key)
	out.Layer = preferString(incoming.Layer, existing.Layer)
	out.Area = preferString(incoming.Area, existing.Area)
	out.Kind = preferString(incoming.Kind, existing.Kind)
	out.Subject = preferString(incoming.Subject, existing.Subject)
	out.Summary = preferString(incoming.Summary, existing.Summary)
	out.Content = preferString(incoming.Content, existing.Content)
	out.Source = preferString(incoming.Source, existing.Source)
	out.State = preferString(incoming.State, existing.State)
	out.ConflictKey = preferString(incoming.ConflictKey, existing.ConflictKey)
	out.ConflictStatus = preferString(incoming.ConflictStatus, existing.ConflictStatus)
	out.SupersededBy = preferString(incoming.SupersededBy, existing.SupersededBy)
	out.Tags = dedupeTags(append(existing.Tags, incoming.Tags...))
	out.RelatedIDs = dedupeStrings(append(existing.RelatedIDs, incoming.RelatedIDs...))
	out.SourceCount = max(existing.SourceCount, incoming.SourceCount)
	if incoming.Importance > 0 || existing.Importance == 0 {
		out.Importance = max(existing.Importance, incoming.Importance)
	}
	if incoming.Confidence > 0 || existing.Confidence == 0 {
		out.Confidence = math.Max(existing.Confidence, incoming.Confidence)
	}
	out.AccessCount = max(existing.AccessCount, incoming.AccessCount)
	out.Archived = incoming.Archived
	out.UpdatedAt = now
	if existing.CreatedAt.IsZero() {
		out.CreatedAt = incoming.CreatedAt
	}
	if out.CreatedAt.IsZero() {
		out.CreatedAt = now
	}
	out.LastAccessedAt = laterTime(existing.LastAccessedAt, incoming.LastAccessedAt)
	if len(incoming.LocalEmbedding) > 0 {
		out.LocalEmbedding = incoming.LocalEmbedding
	}
	if len(incoming.Embedding) > 0 {
		out.Embedding = incoming.Embedding
		out.EmbeddingModel = incoming.EmbeddingModel
	}
	return normalizeEntry(out, now, true)
}

func matchesFilters(entry Entry, opts SearchOptions) bool {
	if inactiveEntry(entry) && !opts.IncludeArchived {
		if entry.State != EntryStateSummary && entry.State != EntryStateDisputed {
			return false
		}
	}
	if entry.State == EntryStateSuperseded && !opts.IncludeSuperseded {
		return false
	}
	if entry.Kind == "summary" && !opts.IncludeSummaries && entry.State == EntryStateSummary {
		return false
	}
	if len(opts.Areas) > 0 {
		matched := false
		for _, area := range opts.Areas {
			if entry.MatchArea(area) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(opts.Layers) > 0 {
		matched := false
		for _, layer := range opts.Layers {
			if entry.MatchLayer(layer) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(opts.Kinds) > 0 {
		matched := false
		for _, kind := range opts.Kinds {
			if entry.MatchKind(kind) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(opts.Subjects) > 0 {
		matched := false
		for _, subject := range opts.Subjects {
			if strings.EqualFold(strings.TrimSpace(entry.Subject), strings.TrimSpace(subject)) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	for _, tag := range opts.Tags {
		if !entry.HasTag(tag) {
			return false
		}
	}
	return true
}

func scoreEntry(entry Entry, terms []string, localQuery []float64, remoteQuery []float64, remoteModel string) (float64, float64) {
	meta := float64(entry.Importance) * 1.5
	meta += entry.Confidence * 2
	if entry.State == EntryStateSummary {
		meta += 0.8
	}
	if entry.State == EntryStateDisputed {
		meta -= 0.75
	}
	if !entry.LastAccessedAt.IsZero() && time.Since(entry.LastAccessedAt) < 72*time.Hour {
		meta += 1.5
	}
	if age := time.Since(effectiveTime(entry)); age < 7*24*time.Hour {
		meta += 2
	} else if age < 30*24*time.Hour {
		meta += 0.75
	}
	meta += math.Min(float64(entry.AccessCount)*0.25, 2)

	if len(terms) == 0 {
		return meta, 0
	}

	queryScore := lexicalScore(entry, terms)
	if len(localQuery) > 0 {
		localScore := cosineSimilarity(localQuery, entry.LocalEmbedding)
		if localScore > 0 {
			queryScore += localScore * 7
		}
	}
	if len(remoteQuery) > 0 && len(entry.Embedding) > 0 && entry.EmbeddingModel == remoteModel {
		remoteScore := cosineSimilarity(remoteQuery, entry.Embedding)
		if remoteScore > 0 {
			queryScore += remoteScore * 11
		}
	}
	return meta + queryScore, queryScore
}

func lexicalScore(entry Entry, terms []string) float64 {
	score := 0.0
	content := strings.ToLower(entry.Content)
	summary := strings.ToLower(entry.Summary)
	subject := strings.ToLower(entry.Subject)
	source := strings.ToLower(entry.Source)
	key := strings.ToLower(entry.Key)
	area := strings.ToLower(entry.Area)
	layer := strings.ToLower(entry.Layer)
	kind := strings.ToLower(entry.Kind)
	tags := strings.ToLower(strings.Join(entry.Tags, " "))
	for _, term := range terms {
		switch {
		case summary == term || subject == term || key == term:
			score += 8
		case strings.Contains(tags, term) || area == term || kind == term || layer == term:
			score += 7
		case strings.Contains(summary, term):
			score += 6
		case strings.Contains(subject, term):
			score += 5
		case strings.Contains(content, term):
			score += 4
		case strings.Contains(source, term) || strings.Contains(key, term):
			score += 3
		}
	}
	return score
}

func tokenize(value string) []string {
	fields := strings.Fields(strings.ToLower(value))
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, ".,!?;:\"'`()[]{}")
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}

func compact(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit-3]) + "..."
}

func stableKey(parts ...string) string {
	var cleaned []string
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		part = strings.ReplaceAll(part, "\n", " ")
		part = strings.Join(strings.Fields(part), "-")
		if part != "" {
			cleaned = append(cleaned, part)
		}
	}
	if len(cleaned) == 0 {
		return ""
	}
	return strings.Join(cleaned, ":")
}

func HashKey(value string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(value))))
	return fmt.Sprintf("%x", h.Sum64())
}

func preferString(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return strings.TrimSpace(primary)
	}
	return strings.TrimSpace(fallback)
}

func dedupeTags(tags []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

func dedupeStrings(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func effectiveTime(entry Entry) time.Time {
	if !entry.UpdatedAt.IsZero() {
		return entry.UpdatedAt
	}
	return entry.CreatedAt
}

func laterTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func deriveLayer(entry Entry) string {
	if strings.TrimSpace(entry.Layer) != "" {
		return strings.TrimSpace(entry.Layer)
	}
	area := strings.ToLower(strings.TrimSpace(entry.Area))
	switch {
	case strings.HasPrefix(area, "owner_") || area == "owner":
		return "owner"
	case area == "contacts" || strings.HasPrefix(area, "people"):
		return "people"
	case area == "projects" || strings.HasPrefix(area, "project"):
		return "projects"
	case area == "workflow" || strings.HasPrefix(area, "workflow"):
		return "workflow"
	default:
		return "general"
	}
}

func deriveConflictKey(entry Entry) string {
	if strings.TrimSpace(entry.Key) != "" {
		return strings.ToLower(strings.TrimSpace(entry.Key))
	}
	if strings.TrimSpace(entry.Subject) == "" || strings.TrimSpace(entry.Kind) == "" {
		return ""
	}
	if !shouldArbitrate(entry) {
		return ""
	}
	return stableKey(entry.Layer, entry.Area, entry.Kind, entry.Subject)
}

func shouldArbitrate(entry Entry) bool {
	switch strings.ToLower(strings.TrimSpace(entry.Kind)) {
	case "identity", "preference", "rule", "contact_profile", "person_profile", "project_fact", "workflow_preference":
		return true
	default:
		return false
	}
}

func shouldCompact(entry Entry) bool {
	if entry.Kind == "summary" || entry.Kind == "memory_arbitration" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(entry.Kind)) {
	case "contact_profile", "person_profile", "contact_preference", "contact_alias", "contact_summary":
		return false
	}
	if entry.Importance >= 9 {
		return false
	}
	switch deriveLayer(entry) {
	case "people", "projects", "workflow":
		return true
	default:
		return false
	}
}

func summaryGroupKey(entry Entry) string {
	return strings.ToLower(strings.Join([]string{
		deriveLayer(entry),
		strings.TrimSpace(entry.Area),
		preferString(entry.Subject, "general"),
	}, "|"))
}

func parseSummaryGroupKey(group string) (string, string, string) {
	parts := strings.SplitN(group, "|", 3)
	for len(parts) < 3 {
		parts = append(parts, "")
	}
	layer := parts[0]
	area := parts[1]
	subject := parts[2]
	if layer == "" {
		layer = "general"
	}
	if area == "" {
		area = layer
	}
	if subject == "" {
		subject = "general"
	}
	return layer, area, subject
}

func arbitrationScore(entry Entry) float64 {
	score := float64(entry.Importance) + entry.Confidence*10
	score += sourceAuthority(entry.Source)
	score += math.Min(float64(entry.AccessCount)*0.15, 1)
	if age := time.Since(effectiveTime(entry)); age < 48*time.Hour {
		score += 1.5
	} else if age < 14*24*time.Hour {
		score += 0.5
	}
	return score
}

func sharesExplicitKey(entries []Entry, indexes []int) bool {
	key := ""
	for _, idx := range indexes {
		current := strings.TrimSpace(entries[idx].Key)
		if current == "" {
			return false
		}
		if key == "" {
			key = strings.ToLower(current)
			continue
		}
		if key != strings.ToLower(current) {
			return false
		}
	}
	return key != ""
}

func sourceAuthority(source string) float64 {
	source = strings.ToLower(strings.TrimSpace(source))
	switch {
	case strings.Contains(source, "bootstrap") || strings.Contains(source, "owner"):
		return 4
	case strings.Contains(source, "manual"):
		return 3
	case strings.Contains(source, "social"):
		return 1.5
	case strings.Contains(source, "auto"):
		return 1
	default:
		return 0.5
	}
}

func inactiveEntry(entry Entry) bool {
	return entry.Archived ||
		entry.State == EntryStateArchived ||
		entry.State == EntryStateCompacted ||
		entry.State == EntryStateSuperseded
}

func entrySearchText(entry Entry) string {
	parts := []string{
		entry.Layer,
		entry.Area,
		entry.Kind,
		entry.Subject,
		entry.Summary,
		entry.Content,
		entry.Source,
		strings.Join(entry.Tags, " "),
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func normalizeFactValue(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.NewReplacer(".", " ", ",", " ", "!", " ", "?", " ", ";", " ", ":", " ", "'", "", "\"", "").Replace(value)
	value = strings.Join(strings.Fields(value), " ")
	return value
}

func collectIDs(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.ID) != "" {
			out = append(out, entry.ID)
		}
	}
	return dedupeStrings(out)
}

func countSources(entries []Entry) int {
	total := 0
	for _, entry := range entries {
		if entry.SourceCount > 0 {
			total += entry.SourceCount
			continue
		}
		total++
	}
	if total == 0 {
		total = len(entries)
	}
	return total
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
