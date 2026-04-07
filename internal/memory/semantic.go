package memory

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"

	"qorvexus/internal/model"
	"qorvexus/internal/types"
)

func (s *Store) embedQuery(query string) ([]float64, string, error) {
	query = strings.TrimSpace(query)
	if query == "" || !s.opts.SemanticSearch {
		return nil, "", nil
	}
	return s.embedText(query)
}

func (s *Store) embedText(text string) ([]float64, string, error) {
	text = strings.TrimSpace(text)
	if text == "" || !s.opts.SemanticSearch {
		return nil, "", nil
	}
	if s.opts.Models == nil || strings.TrimSpace(s.opts.EmbeddingModel) == "" {
		return nil, "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.opts.EmbeddingTimeout)
	defer cancel()
	resp, ok, err := s.opts.Models.Embed(ctx, s.opts.EmbeddingModel, []string{text})
	if !ok || err != nil || resp == nil || len(resp.Vectors) == 0 {
		return nil, "", err
	}
	return normalizeVector(resp.Vectors[0]), resp.Model, nil
}

func (s *Store) summarizeEntries(layer string, area string, subject string, entries []Entry) (string, string) {
	if summary, content, ok := s.summarizeWithModel(layer, area, subject, entries); ok {
		return compact(summary, 180), strings.TrimSpace(content)
	}
	return fallbackSummary(layer, area, subject, entries)
}

func (s *Store) summarizeWithModel(layer string, area string, subject string, entries []Entry) (string, string, bool) {
	if s.opts.Models == nil || strings.TrimSpace(s.opts.SummaryModel) == "" {
		return "", "", false
	}
	client, _, ok := s.opts.Models.Get(s.opts.SummaryModel)
	if !ok || client == nil {
		return "", "", false
	}
	var b strings.Builder
	b.WriteString("Condense these long-term memory fragments into one durable note.\n")
	b.WriteString("Keep stable facts, remove repetition, mention unresolved uncertainty briefly, and prefer concrete details over filler.\n")
	b.WriteString("Return plain text only, at most 5 short bullet lines.\n")
	b.WriteString(fmt.Sprintf("Layer: %s\nArea: %s\nSubject: %s\nFragments:\n", layer, area, subject))
	for _, entry := range pickSummaryEntries(entries, s.opts.MaxSummarySources) {
		b.WriteString("- ")
		b.WriteString(compact(preferString(entry.Summary, entry.Content), 220))
		b.WriteString("\n")
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.opts.SummaryTimeout)
	defer cancel()
	resp, err := client.Complete(ctx, typesToCompletionRequest(s.opts.SummaryModel, b.String()))
	if err != nil || resp == nil {
		return "", "", false
	}
	content := strings.TrimSpace(resp.Message.Content)
	if content == "" {
		return "", "", false
	}
	return compact(content, 180), content, true
}

func typesToCompletionRequest(modelName string, prompt string) model.CompletionRequest {
	return model.CompletionRequest{
		Model: modelName,
		Messages: []types.Message{
			{
				Role:    types.RoleUser,
				Content: prompt,
			},
		},
		MaxTokens:   280,
		Temperature: 0.1,
	}
}

func fallbackSummary(layer string, area string, subject string, entries []Entry) (string, string) {
	picked := pickSummaryEntries(entries, 6)
	if len(picked) == 0 {
		content := fmt.Sprintf("Summary for %s/%s/%s.", layer, area, subject)
		return compact(content, 180), content
	}
	lines := []string{}
	for _, entry := range picked {
		line := compact(preferString(entry.Summary, entry.Content), 160)
		if line != "" {
			lines = append(lines, line)
		}
	}
	header := fmt.Sprintf("Summary for %s / %s / %s.", layer, area, subject)
	content := header
	if len(lines) > 0 {
		content += "\n- " + strings.Join(lines, "\n- ")
	}
	return compact(header+" "+strings.Join(lines, " "), 180), content
}

func pickSummaryEntries(entries []Entry, limit int) []Entry {
	items := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.Content) == "" && strings.TrimSpace(entry.Summary) == "" {
			continue
		}
		items = append(items, entry)
	}
	sort.Slice(items, func(i, j int) bool {
		left := summaryWeight(items[i])
		right := summaryWeight(items[j])
		if math.Abs(left-right) < 0.001 {
			return effectiveTime(items[i]).After(effectiveTime(items[j]))
		}
		return left > right
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func summaryWeight(entry Entry) float64 {
	score := float64(entry.Importance) + entry.Confidence*3
	if entry.Kind == "summary" {
		score += 1.5
	}
	return score
}

func localSemanticEmbedding(text string) []float64 {
	text = normalizeFactValue(text)
	if text == "" {
		return nil
	}
	const dims = 192
	vector := make([]float64, dims)
	tokens := strings.Fields(text)
	for _, token := range tokens {
		addHashedFeature(vector, token, 1.6)
		for _, alias := range semanticAliases(token) {
			addHashedFeature(vector, alias, 1.0)
		}
	}
	for i := 0; i < len(tokens)-1; i++ {
		addHashedFeature(vector, tokens[i]+"_"+tokens[i+1], 0.85)
	}
	runes := []rune(text)
	for n := 3; n <= 4; n++ {
		for i := 0; i+n <= len(runes); i++ {
			addHashedFeature(vector, string(runes[i:i+n]), 0.3)
		}
	}
	return normalizeVector(vector)
}

func semanticAliases(token string) []string {
	switch token {
	case "owner", "user":
		return []string{"person", "identity"}
	case "reply", "response":
		return []string{"answer", "message"}
	case "repo", "repository":
		return []string{"project", "codebase"}
	case "contact", "person":
		return []string{"people", "counterparty"}
	case "timezone", "locale":
		return []string{"location", "region"}
	case "workflow", "process":
		return []string{"routine", "steps"}
	default:
		if len(token) > 5 {
			return []string{token[:len(token)-1]}
		}
	}
	return nil
}

func addHashedFeature(vector []float64, feature string, weight float64) {
	if len(vector) == 0 || feature == "" {
		return
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(feature))
	idx := int(h.Sum64() % uint64(len(vector)))
	vector[idx] += weight
}

func normalizeVector(vector []float64) []float64 {
	if len(vector) == 0 {
		return nil
	}
	magnitude := 0.0
	for _, value := range vector {
		magnitude += value * value
	}
	if magnitude == 0 {
		return nil
	}
	magnitude = math.Sqrt(magnitude)
	out := make([]float64, len(vector))
	for i, value := range vector {
		out[i] = value / magnitude
	}
	return out
}

func cosineSimilarity(a []float64, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	sum := 0.0
	for i := range a {
		sum += a[i] * b[i]
	}
	if sum < 0 {
		return 0
	}
	return sum
}
