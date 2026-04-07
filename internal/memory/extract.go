package memory

import (
	"fmt"
	"regexp"
	"strings"

	"qorvexus/internal/types"
)

var (
	reCallMe          = regexp.MustCompile(`(?i)\bcall me\s+([A-Za-z0-9 _.\-]{2,40})`)
	reMyNameIs        = regexp.MustCompile(`(?i)\bmy name is\s+([A-Za-z0-9 _.\-]{2,40})`)
	reIAm             = regexp.MustCompile(`(?i)\b(?:i am|i'm)\s+(?:an?\s+)?([A-Za-z][A-Za-z0-9 ,/\-]{2,80})`)
	reWorkAs          = regexp.MustCompile(`(?i)\b(?:i work as|my role is|i work in)\s+([A-Za-z][A-Za-z0-9 ,/\-]{2,80})`)
	reTimezone        = regexp.MustCompile(`(?i)\b(?:my timezone is|timezone[: ]|i am in|i'm in|based in)\s+([A-Za-z0-9_./+\- :]{2,80})`)
	reHelpWith        = regexp.MustCompile(`(?i)\b(?:help me with|i want this bot to help with|this bot should help with)\s+(.{4,160})`)
	rePreferenceLead  = regexp.MustCompile(`(?i)\b(?:i prefer|please|always|never|do not|don't|avoid|use)\b`)
	reQuotedStatement = regexp.MustCompile(`["“](.+?)["”]`)
	reProjectName     = regexp.MustCompile(`(?i)\b(?:project|repo|repository|feature|milestone|initiative)\s+([A-Za-z0-9._/\-]{2,80})`)
	reWorkingOn       = regexp.MustCompile(`(?i)\b(?:working on|building|shipping|fixing|implementing)\s+([A-Za-z0-9 _./#:\-]{3,90})`)
)

func ExtractStructuredMemories(sessionID string, userText string, assistantText string, ctx types.ConversationContext) []Entry {
	userText = strings.TrimSpace(userText)
	assistantText = strings.TrimSpace(assistantText)
	if userText == "" {
		return nil
	}
	var out []Entry
	if ctx.IsOwner || ctx.Trust == types.TrustOwner || strings.EqualFold(sessionID, "owner-onboarding") {
		out = append(out, extractOwnerMemories(userText)...)
		if strings.EqualFold(sessionID, "owner-onboarding") {
			out = append(out, Entry{
				Key:        stableKey("owner", "onboarding", HashKey(userText)),
				Layer:      "owner",
				Area:       "owner_profile",
				Kind:       "onboarding_note",
				Subject:    "owner",
				Summary:    compact(userText, 140),
				Content:    "Owner onboarding note: " + compact(userText, 320),
				Source:     "auto:onboarding",
				Tags:       []string{"owner_profile", "memory_area:owner_profile"},
				Importance: 6,
				Confidence: 0.7,
			})
		}
	}
	out = append(out, extractPeopleMemories(userText, assistantText, ctx)...)
	out = append(out, extractProjectMemories(sessionID, userText, assistantText, ctx)...)
	out = append(out, extractWorkflowMemories(sessionID, userText, assistantText, ctx)...)
	return dedupeEntries(out)
}

func extractOwnerMemories(text string) []Entry {
	var out []Entry
	if value := firstMatch(reCallMe, text); value != "" {
		out = append(out, ownerEntry("identity", "preferred_name", value, fmt.Sprintf("Owner prefers to be called %s.", value), 10, 1))
	}
	if value := firstMatch(reMyNameIs, text); value != "" {
		out = append(out, ownerEntry("identity", "name", value, fmt.Sprintf("Owner's name is %s.", value), 10, 1))
	}
	if value := firstMatch(reWorkAs, text); value != "" {
		out = append(out, ownerEntry("identity", "role", value, fmt.Sprintf("Owner's role or field: %s.", cleanSentence(value)), 8, 0.95))
	} else if value := firstMatch(reIAm, text); value != "" && looksLikeRole(value) {
		out = append(out, ownerEntry("identity", "role", value, fmt.Sprintf("Owner describes themselves as %s.", cleanSentence(value)), 7, 0.8))
	}
	if value := firstMatch(reTimezone, text); value != "" {
		out = append(out, ownerEntry("identity", "timezone", value, fmt.Sprintf("Owner timezone or locale: %s.", cleanSentence(value)), 8, 0.85))
	}
	if value := firstMatch(reHelpWith, text); value != "" {
		out = append(out, ownerEntry("goals", "primary_needs", value, fmt.Sprintf("Owner wants the bot to help with %s.", cleanSentence(value)), 8, 0.85))
	}
	for _, pref := range extractPreferenceStatements(text) {
		out = append(out, Entry{
			Key:        stableKey("owner", "preference", HashKey(pref)),
			Layer:      "owner",
			Area:       "owner_preferences",
			Kind:       "preference",
			Subject:    "owner",
			Summary:    compact(pref, 120),
			Content:    pref,
			Source:     "auto:conversation",
			Tags:       []string{"owner_profile", "owner_preference", "memory_area:owner_profile"},
			Importance: 8,
			Confidence: 0.8,
		})
	}
	for _, rule := range extractRuleStatements(text) {
		out = append(out, Entry{
			Key:        stableKey("owner", "rule", HashKey(rule)),
			Layer:      "owner",
			Area:       "owner_rules",
			Kind:       "rule",
			Subject:    "owner",
			Summary:    compact(rule, 120),
			Content:    rule,
			Source:     "auto:conversation",
			Tags:       []string{"owner_profile", "owner_rule", "memory_area:owner_profile"},
			Importance: 10,
			Confidence: 0.9,
		})
	}
	return out
}

func ownerEntry(kind string, slot string, value string, content string, importance int, confidence float64) Entry {
	value = cleanSentence(value)
	return Entry{
		Key:        stableKey("owner", kind, slot),
		Layer:      "owner",
		Area:       "owner_profile",
		Kind:       kind,
		Subject:    "owner",
		Summary:    compact(content, 120),
		Content:    content,
		Source:     "auto:conversation",
		Tags:       []string{"owner_profile", "owner_identity", "memory_area:owner_profile"},
		Importance: importance,
		Confidence: confidence,
	}
}

func extractPeopleMemories(userText string, assistantText string, ctx types.ConversationContext) []Entry {
	if ctx.IsOwner {
		return nil
	}
	if ctx.Trust != types.TrustExternal && ctx.Trust != types.TrustTrusted {
		return nil
	}
	subject := strings.TrimSpace(ctx.SenderName)
	if subject == "" {
		subject = strings.TrimSpace(ctx.SenderID)
	}
	if subject == "" {
		return nil
	}
	content := fmt.Sprintf("Interaction with %s. Latest inbound: %s", subject, compact(userText, 220))
	if strings.TrimSpace(assistantText) != "" {
		content += ". Latest reply: " + compact(assistantText, 180)
	}
	return []Entry{{
		Key:        stableKey("person", HashKey(subject), "interaction", HashKey(compact(userText, 120))),
		Layer:      "people",
		Area:       "contacts",
		Kind:       "interaction_note",
		Subject:    subject,
		Summary:    compact(content, 140),
		Content:    content,
		Source:     "auto:conversation",
		Tags:       []string{"people", "contact", "memory_layer:people"},
		Importance: 5,
		Confidence: 0.7,
	}}
}

func extractProjectMemories(sessionID string, userText string, assistantText string, ctx types.ConversationContext) []Entry {
	if !(ctx.IsOwner || ctx.Trust == types.TrustOwner) {
		return nil
	}
	subject := firstMatch(reProjectName, userText)
	if subject == "" {
		subject = firstMatch(reWorkingOn, userText)
	}
	if subject == "" {
		return nil
	}
	content := fmt.Sprintf("Project context for %s: %s", cleanSentence(subject), compact(userText, 220))
	if strings.TrimSpace(assistantText) != "" {
		content += ". Latest outcome: " + compact(assistantText, 180)
	}
	return []Entry{{
		Key:        stableKey("project", HashKey(subject), HashKey(sessionID), HashKey(compact(userText, 120))),
		Layer:      "projects",
		Area:       "projects",
		Kind:       "project_note",
		Subject:    cleanSentence(subject),
		Summary:    compact(content, 140),
		Content:    content,
		Source:     "auto:conversation",
		Tags:       []string{"projects", "project", "memory_layer:projects"},
		Importance: 7,
		Confidence: 0.72,
	}}
}

func extractWorkflowMemories(sessionID string, userText string, assistantText string, ctx types.ConversationContext) []Entry {
	if strings.EqualFold(sessionID, "owner-onboarding") {
		return nil
	}
	if !(ctx.IsOwner || ctx.Trust == types.TrustOwner) {
		return nil
	}
	summary := fmt.Sprintf("Session %s focused on %s", sessionID, compact(userText, 120))
	content := summary
	if strings.TrimSpace(assistantText) != "" {
		content += ". Assistant outcome: " + compact(assistantText, 180)
	}
	return []Entry{{
		Key:        stableKey("workflow", "conversation", HashKey(sessionID), HashKey(compact(userText, 120))),
		Layer:      "workflow",
		Area:       "workflow",
		Kind:       "conversation_outcome",
		Subject:    sessionID,
		Summary:    compact(summary, 140),
		Content:    content,
		Source:     "auto:conversation",
		Tags:       []string{"workflow", "memory_layer:workflow", "session:" + sessionID},
		Importance: 4,
		Confidence: 0.65,
	}}
}

func extractPreferenceStatements(text string) []string {
	lines := splitStatements(text)
	var out []string
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "prefer") || strings.Contains(lower, "please") || strings.Contains(lower, "i like") {
			out = append(out, normalizeStatement(line))
		}
	}
	return out
}

func extractRuleStatements(text string) []string {
	lines := splitStatements(text)
	var out []string
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "always") || strings.Contains(lower, "never") || strings.Contains(lower, "do not") || strings.Contains(lower, "don't") || strings.Contains(lower, "avoid") {
			out = append(out, normalizeStatement(line))
		}
	}
	return out
}

func splitStatements(text string) []string {
	text = strings.ReplaceAll(text, "\n", ". ")
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return r == '.' || r == '!' || r == '?' || r == ';'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if len(part) < 8 {
			continue
		}
		out = append(out, part)
	}
	return out
}

func normalizeStatement(value string) string {
	value = cleanSentence(value)
	if quoted := firstQuoted(value); quoted != "" && rePreferenceLead.MatchString(value) {
		return quoted
	}
	return value
}

func cleanSentence(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, ".,!?;:")
	value = strings.Join(strings.Fields(value), " ")
	return value
}

func firstMatch(re *regexp.Regexp, value string) string {
	match := re.FindStringSubmatch(value)
	if len(match) < 2 {
		return ""
	}
	return cleanSentence(match[1])
}

func firstQuoted(value string) string {
	match := reQuotedStatement.FindStringSubmatch(value)
	if len(match) < 2 {
		return ""
	}
	return cleanSentence(match[1])
}

func looksLikeRole(value string) bool {
	value = strings.ToLower(value)
	keywords := []string{"engineer", "developer", "designer", "founder", "manager", "student", "researcher", "writer", "operator"}
	for _, keyword := range keywords {
		if strings.Contains(value, keyword) {
			return true
		}
	}
	return false
}

func dedupeEntries(entries []Entry) []Entry {
	seen := map[string]struct{}{}
	out := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.Content) == "" {
			continue
		}
		key := entry.Key
		if key == "" {
			key = HashKey(entry.Content)
		}
		if _, ok := seen[strings.ToLower(key)]; ok {
			continue
		}
		seen[strings.ToLower(key)] = struct{}{}
		out = append(out, entry)
	}
	return out
}
