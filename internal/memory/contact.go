package memory

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"qorvexus/internal/types"
)

type ContactIdentity struct {
	CanonicalSubject string
	RouteKey         string
	DisplayName      string
	ClaimedName      string
	Organization     string
	Aliases          []string
}

type ContactCard struct {
	Subject         string    `json:"subject"`
	DisplayName     string    `json:"display_name,omitempty"`
	Organization    string    `json:"organization,omitempty"`
	Role            string    `json:"role,omitempty"`
	Location        string    `json:"location,omitempty"`
	Trust           string    `json:"trust,omitempty"`
	Aliases         []string  `json:"aliases,omitempty"`
	Channels        []string  `json:"channels,omitempty"`
	Preferences     []string  `json:"preferences,omitempty"`
	LastInteraction string    `json:"last_interaction,omitempty"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
}

func ResolveContactIdentity(ctx types.ConversationContext, userText string) ContactIdentity {
	routeKey := ContactMemoryRouteKey(ctx)
	displayName := strings.TrimSpace(ctx.SenderName)
	if displayName == "" {
		displayName = strings.TrimSpace(ctx.SenderID)
	}
	claimedName := firstMatch(reMyNameIs, userText)
	if claimedName == "" {
		claimedName = firstMatch(reCallMe, userText)
	}
	claimedName = cleanSentence(claimedName)
	organization := cleanSentence(firstMatch(reWorkAt, userText))

	baseName := claimedName
	if baseName == "" {
		baseName = displayName
	}
	canonical := routeKey
	if normalized := normalizeContactName(baseName); normalized != "" {
		canonical = "person:" + normalized
	}

	aliases := dedupeStrings([]string{
		routeKey,
		displayName,
		claimedName,
	})

	return ContactIdentity{
		CanonicalSubject: canonical,
		RouteKey:         routeKey,
		DisplayName:      displayName,
		ClaimedName:      claimedName,
		Organization:     organization,
		Aliases:          aliases,
	}
}

func ContactMemoryRouteKey(ctx types.ConversationContext) string {
	channel := strings.ToLower(strings.TrimSpace(ctx.Channel))
	senderID := strings.TrimSpace(ctx.SenderID)
	senderName := normalizeContactLabel(ctx.SenderName)
	switch {
	case channel == "" && senderID == "" && senderName == "":
		return ""
	case senderID != "":
		if channel == "" {
			return senderID
		}
		return channel + ":" + senderID
	default:
		if channel == "" {
			return senderName
		}
		return channel + ":" + senderName
	}
}

func ContactMemorySubject(ctx types.ConversationContext) string {
	return ResolveContactIdentity(ctx, "").CanonicalSubject
}

func ContactMemoryDisplayName(ctx types.ConversationContext) string {
	return ResolveContactIdentity(ctx, "").DisplayName
}

func ContactMemoryTags(ctx types.ConversationContext) []string {
	return ContactIdentityTags(ResolveContactIdentity(ctx, ""), ctx)
}

func ContactIdentityTags(identity ContactIdentity, ctx types.ConversationContext) []string {
	tags := []string{
		"people",
		"contact",
		"memory_layer:people",
	}
	if identity.CanonicalSubject != "" {
		tags = append(tags, "contact_subject:"+identity.CanonicalSubject)
	}
	if identity.RouteKey != "" {
		tags = append(tags, "contact_route:"+identity.RouteKey)
	}
	if channel := strings.TrimSpace(strings.ToLower(ctx.Channel)); channel != "" {
		tags = append(tags, "channel:"+channel)
	}
	if senderID := strings.TrimSpace(ctx.SenderID); senderID != "" {
		tags = append(tags, "contact_id:"+senderID)
	}
	if senderName := normalizeContactLabel(ctx.SenderName); senderName != "" {
		tags = append(tags, "contact_name:"+senderName)
	}
	if identity.ClaimedName != "" {
		tags = append(tags, "contact_claimed_name:"+normalizeContactLabel(identity.ClaimedName))
	}
	if ctx.Trust != "" {
		tags = append(tags, "trust:"+strings.ToLower(string(ctx.Trust)))
	}
	return dedupeTags(tags)
}

func BuildContactCard(entries []Entry) ContactCard {
	card := ContactCard{}
	var lastInteractionAt time.Time
	for _, entry := range entries {
		if strings.TrimSpace(card.Subject) == "" && strings.TrimSpace(entry.Subject) != "" {
			card.Subject = strings.TrimSpace(entry.Subject)
		}
		if effectiveTime(entry).After(card.UpdatedAt) {
			card.UpdatedAt = effectiveTime(entry)
		}
		for _, tag := range entry.Tags {
			lower := strings.ToLower(strings.TrimSpace(tag))
			switch {
			case strings.HasPrefix(lower, "contact_route:"):
				card.Aliases = append(card.Aliases, strings.TrimSpace(tag[len("contact_route:"):]))
			case strings.HasPrefix(lower, "channel:"):
				card.Channels = append(card.Channels, strings.TrimSpace(tag[len("channel:"):]))
			case strings.HasPrefix(lower, "contact_name:") && card.DisplayName == "":
				card.DisplayName = strings.TrimSpace(tag[len("contact_name:"):])
			}
		}
		switch strings.ToLower(strings.TrimSpace(entry.Kind)) {
		case "contact_profile":
			switch {
			case strings.HasSuffix(strings.ToLower(entry.Key), ":display_name"):
				card.DisplayName = parseContactValue(entry.Content, "Contact display name is ")
			case strings.HasSuffix(strings.ToLower(entry.Key), ":organization"):
				card.Organization = parseContactValue(entry.Content, "Contact organization or company: ")
			case strings.HasSuffix(strings.ToLower(entry.Key), ":role"):
				card.Role = parseContactValue(entry.Content, "Contact role or field: ")
			case strings.HasSuffix(strings.ToLower(entry.Key), ":location"):
				card.Location = parseContactValue(entry.Content, "Contact timezone or location hint: ")
			case strings.HasSuffix(strings.ToLower(entry.Key), ":trust"):
				card.Trust = parseContactValue(entry.Content, "Contact trust level is ")
			}
		case "contact_alias":
			card.Aliases = append(card.Aliases, parseContactValue(entry.Content, "Known contact alias or route: "))
		case "contact_preference":
			card.Preferences = append(card.Preferences, strings.TrimSpace(entry.Content))
		case "interaction_note":
			if card.LastInteraction == "" || effectiveTime(entry).After(lastInteractionAt) {
				card.LastInteraction = compact(strings.TrimSpace(entry.Content), 180)
				lastInteractionAt = effectiveTime(entry)
			}
		}
	}
	card.Aliases = dedupeStrings(card.Aliases)
	card.Channels = dedupeStrings(card.Channels)
	card.Preferences = dedupeStrings(card.Preferences)
	if card.DisplayName != "" && strings.Contains(card.DisplayName, "-") {
		card.DisplayName = humanizeContactLabel(card.DisplayName)
	}
	return card
}

func FormatContactCard(card ContactCard) string {
	if strings.TrimSpace(card.Subject) == "" &&
		strings.TrimSpace(card.DisplayName) == "" &&
		strings.TrimSpace(card.Organization) == "" &&
		strings.TrimSpace(card.Role) == "" &&
		strings.TrimSpace(card.Location) == "" &&
		strings.TrimSpace(card.Trust) == "" &&
		len(card.Aliases) == 0 &&
		len(card.Preferences) == 0 &&
		strings.TrimSpace(card.LastInteraction) == "" {
		return ""
	}
	var lines []string
	if card.DisplayName != "" {
		lines = append(lines, "display_name: "+card.DisplayName)
	}
	if card.Organization != "" {
		lines = append(lines, "organization: "+card.Organization)
	}
	if card.Role != "" {
		lines = append(lines, "role: "+card.Role)
	}
	if card.Location != "" {
		lines = append(lines, "location: "+card.Location)
	}
	if card.Trust != "" {
		lines = append(lines, "trust: "+card.Trust)
	}
	if len(card.Aliases) > 0 {
		lines = append(lines, "aliases: "+strings.Join(card.Aliases, ", "))
	}
	if len(card.Channels) > 0 {
		lines = append(lines, "channels: "+strings.Join(card.Channels, ", "))
	}
	if len(card.Preferences) > 0 {
		lines = append(lines, "preferences: "+strings.Join(card.Preferences, " | "))
	}
	if card.LastInteraction != "" {
		lines = append(lines, "last_interaction: "+card.LastInteraction)
	}
	return strings.Join(lines, "\n")
}

func normalizeContactName(value string) string {
	value = cleanSentence(value)
	if value == "" {
		return ""
	}
	if looksLikeRole(value) {
		return ""
	}
	return normalizeContactLabel(value)
}

func normalizeContactLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer(".", " ", ",", " ", "/", " ", "_", " ", "@", " ", "(", " ", ")", " ").Replace(value)
	value = strings.Join(strings.Fields(value), "-")
	return strings.Trim(value, "-")
}

func humanizeContactLabel(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '-' || r == '_' })
	for i := range parts {
		if len(parts[i]) == 0 {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, " ")
}

func parseContactValue(content string, prefix string) string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, prefix) {
		return strings.Trim(strings.TrimSpace(content[len(prefix):]), ". ")
	}
	return strings.Trim(content, ". ")
}

func SortContactEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Importance == entries[j].Importance {
			return effectiveTime(entries[i]).After(effectiveTime(entries[j]))
		}
		return entries[i].Importance > entries[j].Importance
	})
}

func ContactCardKey(subject string) string {
	if strings.TrimSpace(subject) == "" {
		return ""
	}
	return stableKey("person", subject, "summary_card")
}

func ContactCardEntry(entries []Entry) Entry {
	card := BuildContactCard(entries)
	content := FormatContactCard(card)
	return Entry{
		Key:        ContactCardKey(card.Subject),
		Layer:      "people",
		Area:       "contacts",
		Kind:       "contact_summary",
		Subject:    card.Subject,
		Summary:    compact(fmt.Sprintf("Contact summary for %s.", displayNameOrSubject(card.DisplayName, card.Subject)), 140),
		Content:    content,
		Source:     "memory:contact_summary",
		Tags:       []string{"people", "contact", "contact_summary"},
		Importance: 7,
		Confidence: 0.75,
	}
}

func (s *Store) RefreshContactCard(subject string) error {
	if s == nil || strings.TrimSpace(subject) == "" {
		return nil
	}
	items, err := s.SearchWithOptions(SearchOptions{
		Layers:           []string{"people"},
		Areas:            []string{"contacts"},
		Subjects:         []string{subject},
		Limit:            64,
		IncludeSummaries: true,
	})
	if err != nil {
		return err
	}
	filtered := make([]Entry, 0, len(items))
	for _, item := range items {
		if item.Kind == "contact_summary" {
			continue
		}
		filtered = append(filtered, item)
	}
	if len(filtered) == 0 {
		return nil
	}
	entry := ContactCardEntry(filtered)
	if strings.TrimSpace(entry.Content) == "" {
		return nil
	}
	return s.Upsert(entry)
}
