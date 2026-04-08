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
	reWorkAt          = regexp.MustCompile(`(?i)\b(?:i work at|i'm with|i am with|my company is|our company is|we are from)\s+([A-Za-z0-9&.,_/+\- ]{2,80})`)
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
	identity := ResolveContactIdentity(ctx, userText)
	subject := identity.CanonicalSubject
	if subject == "" {
		return nil
	}
	displayName := displayNameOrSubject(identity.ClaimedName, identity.DisplayName)
	tags := ContactIdentityTags(identity, ctx)
	var out []Entry
	if identity.RouteKey != "" {
		out = append(out, Entry{
			Key:        stableKey("person", subject, "alias", HashKey(identity.RouteKey)),
			Layer:      "people",
			Area:       "contacts",
			Kind:       "contact_alias",
			Subject:    subject,
			Summary:    fmt.Sprintf("Known route alias for %s: %s.", displayNameOrSubject(displayName, subject), identity.RouteKey),
			Content:    fmt.Sprintf("Known contact alias or route: %s.", identity.RouteKey),
			Source:     "social:identity",
			Tags:       append([]string{}, tags...),
			Importance: 8,
			Confidence: 0.95,
		})
	}
	if identity.ClaimedName != "" && !strings.EqualFold(strings.TrimSpace(identity.ClaimedName), strings.TrimSpace(identity.DisplayName)) {
		out = append(out, Entry{
			Key:        stableKey("person", subject, "alias", HashKey(identity.ClaimedName)),
			Layer:      "people",
			Area:       "contacts",
			Kind:       "contact_alias",
			Subject:    subject,
			Summary:    fmt.Sprintf("%s explicitly introduced themselves as %s.", displayNameOrSubject(identity.DisplayName, subject), identity.ClaimedName),
			Content:    fmt.Sprintf("Known contact alias or route: %s.", identity.ClaimedName),
			Source:     "social:identity",
			Tags:       append([]string{}, tags...),
			Importance: 7,
			Confidence: 0.88,
		})
	}
	if displayName != "" {
		out = append(out, Entry{
			Key:        stableKey("person", subject, "identity", "display_name"),
			Layer:      "people",
			Area:       "contacts",
			Kind:       "contact_profile",
			Subject:    subject,
			Summary:    fmt.Sprintf("Display name for %s is %s.", subject, displayName),
			Content:    fmt.Sprintf("Contact display name is %s.", displayName),
			Source:     "social:identity",
			Tags:       append([]string{}, tags...),
			Importance: 8,
			Confidence: 0.95,
		})
	}
	out = append(out, Entry{
		Key:        stableKey("person", subject, "relationship", "trust"),
		Layer:      "people",
		Area:       "contacts",
		Kind:       "contact_profile",
		Subject:    subject,
		Summary:    fmt.Sprintf("Trust level for %s is %s.", displayNameOrSubject(displayName, subject), ctx.Trust),
		Content:    fmt.Sprintf("Contact trust level is %s.", ctx.Trust),
		Source:     "social:identity",
		Tags:       append([]string{}, tags...),
		Importance: 7,
		Confidence: 0.9,
	})
	if value := firstMatch(reWorkAt, userText); value != "" {
		out = append(out, Entry{
			Key:        stableKey("person", subject, "profile", "organization"),
			Layer:      "people",
			Area:       "contacts",
			Kind:       "contact_profile",
			Subject:    subject,
			Summary:    fmt.Sprintf("%s is associated with %s.", displayNameOrSubject(displayName, subject), cleanSentence(value)),
			Content:    fmt.Sprintf("Contact organization or company: %s.", cleanSentence(value)),
			Source:     "social:conversation",
			Tags:       append([]string{}, tags...),
			Importance: 8,
			Confidence: 0.82,
		})
	}
	if value := firstMatch(reWorkAs, userText); value != "" {
		out = append(out, Entry{
			Key:        stableKey("person", subject, "profile", "role"),
			Layer:      "people",
			Area:       "contacts",
			Kind:       "contact_profile",
			Subject:    subject,
			Summary:    fmt.Sprintf("%s works as %s.", displayNameOrSubject(displayName, subject), cleanSentence(value)),
			Content:    fmt.Sprintf("Contact role or field: %s.", cleanSentence(value)),
			Source:     "social:conversation",
			Tags:       append([]string{}, tags...),
			Importance: 7,
			Confidence: 0.8,
		})
	} else if value := firstMatch(reIAm, userText); value != "" && looksLikeRole(value) {
		out = append(out, Entry{
			Key:        stableKey("person", subject, "profile", "role"),
			Layer:      "people",
			Area:       "contacts",
			Kind:       "contact_profile",
			Subject:    subject,
			Summary:    fmt.Sprintf("%s describes their role as %s.", displayNameOrSubject(displayName, subject), cleanSentence(value)),
			Content:    fmt.Sprintf("Contact role or field: %s.", cleanSentence(value)),
			Source:     "social:conversation",
			Tags:       append([]string{}, tags...),
			Importance: 6,
			Confidence: 0.68,
		})
	}
	if value := firstMatch(reTimezone, userText); value != "" {
		out = append(out, Entry{
			Key:        stableKey("person", subject, "profile", "location"),
			Layer:      "people",
			Area:       "contacts",
			Kind:       "contact_profile",
			Subject:    subject,
			Summary:    fmt.Sprintf("%s is based in or mentions %s.", displayNameOrSubject(displayName, subject), cleanSentence(value)),
			Content:    fmt.Sprintf("Contact timezone or location hint: %s.", cleanSentence(value)),
			Source:     "social:conversation",
			Tags:       append([]string{}, tags...),
			Importance: 6,
			Confidence: 0.74,
		})
	}
	for _, pref := range extractPreferenceStatements(userText) {
		out = append(out, Entry{
			Key:        stableKey("person", subject, "preference", HashKey(pref)),
			Layer:      "people",
			Area:       "contacts",
			Kind:       "contact_preference",
			Subject:    subject,
			Summary:    fmt.Sprintf("%s preference: %s", displayNameOrSubject(displayName, subject), compact(pref, 120)),
			Content:    pref,
			Source:     "social:conversation",
			Tags:       append([]string{}, tags...),
			Importance: 6,
			Confidence: 0.72,
		})
	}
	content := fmt.Sprintf("Interaction with %s. Latest inbound: %s", displayNameOrSubject(displayName, subject), compact(userText, 220))
	if strings.TrimSpace(assistantText) != "" {
		content += ". Latest reply: " + compact(assistantText, 180)
	}
	out = append(out, Entry{
		Key:        stableKey("person", HashKey(subject), "interaction", HashKey(compact(userText, 120))),
		Layer:      "people",
		Area:       "contacts",
		Kind:       "interaction_note",
		Subject:    subject,
		Summary:    compact(content, 140),
		Content:    content,
		Source:     "social:conversation",
		Tags:       append([]string{}, tags...),
		Importance: 5,
		Confidence: 0.7,
	})
	return out
}

func displayNameOrSubject(displayName string, subject string) string {
	if strings.TrimSpace(displayName) != "" {
		return strings.TrimSpace(displayName)
	}
	return strings.TrimSpace(subject)
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
