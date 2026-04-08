package social

import (
	"strings"

	"qorvexus/internal/types"
)

type DeliveryMode string

const (
	DeliveryModeSend   DeliveryMode = "send"
	DeliveryModeHold   DeliveryMode = "hold"
	DeliveryModeSilent DeliveryMode = "silent"
)

const NoReplySentinel = "[[NO_REPLY]]"

type AuthorizationDecision struct {
	Mode        DeliveryMode `json:"mode"`
	Boundary    Boundary     `json:"boundary"`
	Reason      string       `json:"reason"`
	HighRisk    bool         `json:"high_risk,omitempty"`
	NeedsReview bool         `json:"needs_review,omitempty"`
}

func DecideOutboundAuthorization(env Envelope, reply string, contact ContactNode, autoSendTrusted bool, autoSendExternal bool) AuthorizationDecision {
	reply = strings.TrimSpace(reply)
	if IsSilentReply(reply) {
		return AuthorizationDecision{
			Mode:     DeliveryModeSilent,
			Boundary: contact.Boundary,
			Reason:   "the assistant chose to stay silent",
		}
	}
	if env.Context.IsOwner || env.Context.Trust == types.TrustOwner || env.Context.Trust == types.TrustSystem {
		return AuthorizationDecision{
			Mode:     DeliveryModeSend,
			Boundary: BoundaryOwnerDirect,
			Reason:   "owner or system context can send directly",
		}
	}
	boundary := contact.Boundary
	if boundary == "" {
		boundary = DefaultBoundary(env.Context.Trust, contact.InteractionCount)
	}
	highRisk := containsSensitiveBusiness(reply) || containsSensitiveBusiness(env.Text) || containsCommitmentLanguage(reply)
	switch env.Context.Trust {
	case types.TrustTrusted:
		if !autoSendTrusted {
			return AuthorizationDecision{
				Mode:        DeliveryModeHold,
				Boundary:    boundary,
				Reason:      "trusted auto-send is disabled for this deployment",
				NeedsReview: true,
			}
		}
		if highRisk && boundary == BoundaryTrustedReview {
			return AuthorizationDecision{
				Mode:        DeliveryModeHold,
				Boundary:    boundary,
				Reason:      "trusted contact reply creates a sensitive commitment before the relationship reaches a wider autonomy boundary",
				HighRisk:    true,
				NeedsReview: true,
			}
		}
		return AuthorizationDecision{
			Mode:     DeliveryModeSend,
			Boundary: boundary,
			Reason:   "trusted contact is inside the assistant's delegated social boundary",
			HighRisk: highRisk,
		}
	default:
		if !autoSendExternal {
			return AuthorizationDecision{
				Mode:        DeliveryModeHold,
				Boundary:    boundary,
				Reason:      "external auto-send is disabled for this deployment",
				NeedsReview: true,
			}
		}
		if highRisk && boundary == BoundaryExternalReview {
			return AuthorizationDecision{
				Mode:        DeliveryModeHold,
				Boundary:    boundary,
				Reason:      "external reply creates a sensitive commitment before enough relationship context has been built",
				HighRisk:    true,
				NeedsReview: true,
			}
		}
		return AuthorizationDecision{
			Mode:     DeliveryModeSend,
			Boundary: boundary,
			Reason:   "external contact is inside the assistant's delegated communication boundary",
			HighRisk: highRisk,
		}
	}
}

func IsSilentReply(reply string) bool {
	reply = strings.TrimSpace(reply)
	return reply == "" || strings.EqualFold(reply, NoReplySentinel)
}

func containsSensitiveBusiness(text string) bool {
	text = strings.ToLower(text)
	return containsAny(text, []string{
		"proposal", "quote", "pricing", "price", "payment", "invoice", "contract", "agreement",
		"legal", "nda", "confidential", "deadline", "meeting", "call", "schedule", "tomorrow",
		"next week", "next month", "deliverable", "publish", "announcement",
	})
}

func containsCommitmentLanguage(text string) bool {
	text = strings.ToLower(text)
	return containsAny(text, []string{
		"i will", "we will", "i'll", "we'll", "let me", "happy to", "sure, i can",
		"i can do", "we can do", "i can send", "i can share", "i can prepare", "i can follow up",
	})
}

func containsAny(haystack string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}
