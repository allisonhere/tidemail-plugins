package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"
)

// typesafeEndpoint is TypeSafe's System One evaluation API.
const typesafeEndpoint = "https://api.typesafe.ai/v1/systemone"

// jevTimeout bounds one HTTP call. TideMail kills a plugin after 5 seconds, so
// this stays well below that to leave time to fall back to local rules and
// answer.
const jevTimeout = 3500 * time.Millisecond

// maxJevResponse caps what is read from the API.
const maxJevResponse = 1 << 20

// Question IDs. They are for this code only; TypeSafe does not send them to
// the model.
const (
	qNeedsReply = "needs_reply"
	qUrgency    = "urgency"
	qImportance = "importance"
	qCategory   = "category"
)

// jevClient calls the System One API with net/http only.
type jevClient struct {
	endpoint string
	http     *http.Client
}

func newJevClient() jevClient {
	return jevClient{endpoint: typesafeEndpoint, http: &http.Client{Timeout: jevTimeout}}
}

// jevState is everything sent to TypeSafe about a message: the sender, the
// subject, and a few booleans derived locally. Recipients, CC, account and
// folder names, dates, flags, and message IDs are deliberately left out; the
// body, headers, and attachments are never available to this plugin at all.
type jevState struct {
	From            string `json:"from"`
	Subject         string `json:"subject"`
	HasAttachment   bool   `json:"has_attachment"`
	IsReply         bool   `json:"is_reply_in_thread"`
	AutomatedSender bool   `json:"automated_sender"`
	LocalCategory   string `json:"local_category,omitempty"`
}

type jevRequest struct {
	State     jevState               `json:"state"`
	Model     string                 `json:"model"`
	Questions map[string]jevQuestion `json:"questions"`
}

type jevQuestion struct {
	Type         string `json:"type"`
	Instructions any    `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

type jevResponse struct {
	Model   string                     `json:"model"`
	Answers map[string]json.RawMessage `json:"answers"`
}

type jevAnswer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul"`
	Choice        string             `json:"choice"`
	Score         *float64           `json:"score"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    *float64           `json:"confidence"`
}

// jevError is an API failure, described without any request content or key.
type jevError struct {
	Status int
	Reason string
}

func (e *jevError) Error() string {
	if e.Status == 0 {
		return "TypeSafe request failed: " + e.Reason
	}
	return fmt.Sprintf("TypeSafe returned %d: %s", e.Status, e.Reason)
}

// categoryOptions are Jev's fixed category choices; "none" means no category.
var categoryOptions = []string{
	"github", "receipt", "shipping", "security", "calendar", "newsletter",
	"support", "social", "billing", "notification", "personal", "none",
}

var categoryCriteria = map[string]string{
	"github":       "A GitHub notification: pull requests, issues, reviews, workflow runs, repository or account events.",
	"receipt":      "A receipt or order confirmation for something already paid for.",
	"shipping":     "A shipment or delivery update: shipped, in transit, out for delivery, delivered.",
	"security":     "A security notice: sign-in alerts, password changes, verification codes, suspicious activity.",
	"calendar":     "A meeting or event invitation, reschedule, cancellation, or reminder.",
	"newsletter":   "A newsletter, digest, or recurring editorial or marketing mailing.",
	"support":      "A support ticket, case, or help desk conversation.",
	"social":       "A social network notification: mentions, follows, comments, messages from a platform.",
	"billing":      "An invoice, bill, statement, payment due, or failed payment that asks for money.",
	"notification": "Any other automated notification from a service.",
	"personal":     "A message written personally by an individual to the recipient.",
	"none":         "None of these fit.",
}

// buildQuestions returns the enabled questions. All of them go in one request.
func buildQuestions(ask map[string]bool) map[string]jevQuestion {
	qs := map[string]jevQuestion{}
	if ask[qNeedsReply] {
		qs[qNeedsReply] = jevQuestion{
			Type: "noul",
			Instructions: "Using only `from` and `subject`, does this email ask the recipient personally " +
				"for a reply, decision, confirmation, or review? `is_reply_in_thread` means it continues " +
				"an existing conversation. `automated_sender` means the address looks machine-generated.",
			Criteria: map[string]any{
				"true": "A person asks the recipient something or asks them to do something: a direct question, " +
					"a request, a confirmation, or a request for review or approval.",
				"false": []string{
					"Marketing or promotional questions (\"Can you believe these deals?\").",
					"Newsletters, digests, receipts, shipping updates, and automated notifications.",
					"Messages that only inform, with nothing asked of the recipient.",
				},
			},
		}
	}
	if ask[qUrgency] {
		qs[qUrgency] = jevQuestion{
			Type:         "score",
			Instructions: "How time-sensitive is this email for the recipient, judging from `from` and `subject`?",
			Criteria: []string{
				"Routine: no time pressure, or a normal automated update such as a delivery or receipt.",
				"Soon: should be handled in the next day or two.",
				"Urgent: explicitly needs attention today or has a near deadline.",
				"Critical: an emergency, outage, account lockout, or security incident needing immediate action.",
			},
		}
	}
	if ask[qImportance] {
		qs[qImportance] = jevQuestion{
			Type: "noul",
			Instructions: "Is this email important enough that the recipient should prioritize seeing it? " +
				"Importance is about consequence, not time pressure.",
			Criteria: map[string]any{
				"true": []string{
					"Security warnings and account problems.",
					"Payment problems, bills due, or failed payments.",
					"Meeting changes and cancellations.",
					"A direct personal request from a person.",
				},
				"false": []string{
					"Routine receipts and order confirmations.",
					"Ordinary newsletters and promotions.",
					"Routine automated notifications.",
				},
			},
		}
	}
	if ask[qCategory] {
		criteria := map[string]string{}
		for _, c := range categoryOptions {
			criteria[c] = categoryCriteria[c]
		}
		qs[qCategory] = jevQuestion{
			Type:         "choice",
			Instructions: "Which category best describes this email, judging from `from` and `subject`? `local_category`, when present, is a guess from simple rules.",
			Criteria:     criteria,
		}
	}
	return qs
}

// decide sends one System One request and returns the validated answers.
func (c jevClient) decide(ctx context.Context, apiKey, model string, state jevState, questions map[string]jevQuestion) (map[string]jevAnswer, string, error) {
	body, err := json.Marshal(jevRequest{State: state, Model: model, Questions: questions})
	if err != nil {
		return nil, "", &jevError{Reason: "could not encode the request"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, "", &jevError{Reason: "could not build the request"}
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "tidemail-plugin-smart/"+classifierVersion)

	resp, err := c.http.Do(req)
	if err != nil {
		var reason string
		switch {
		case errors.Is(err, context.DeadlineExceeded) || isTimeout(err):
			reason = "timed out"
		default:
			reason = "could not reach api.typesafe.ai"
		}
		return nil, "", &jevError{Reason: reason}
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxJevResponse+1))
	if err != nil || len(raw) > maxJevResponse {
		return nil, "", &jevError{Status: resp.StatusCode, Reason: "unreadable response"}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", &jevError{Status: resp.StatusCode, Reason: statusReason(resp.StatusCode)}
	}
	var parsed jevResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, "", &jevError{Status: resp.StatusCode, Reason: "malformed response"}
	}
	answers := map[string]jevAnswer{}
	for id, q := range questions {
		rawAnswer, ok := parsed.Answers[id]
		if !ok {
			return nil, "", &jevError{Status: resp.StatusCode, Reason: "missing answer for " + id}
		}
		var a jevAnswer
		if err := json.Unmarshal(rawAnswer, &a); err != nil || !validAnswer(q, a) {
			return nil, "", &jevError{Status: resp.StatusCode, Reason: "unexpected answer for " + id}
		}
		answers[id] = a
	}
	return answers, parsed.Model, nil
}

func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}

func statusReason(status int) string {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "the API key was rejected"
	case http.StatusPaymentRequired:
		return "billing problem on the TypeSafe account"
	case http.StatusUnprocessableEntity:
		return "the request was not accepted (check the model setting)"
	case http.StatusTooManyRequests:
		return "rate limited"
	case 529:
		return "TypeSafe is overloaded"
	}
	if status >= 500 {
		return "server error"
	}
	return "unexpected status"
}

func validProb(p float64) bool { return !math.IsNaN(p) && p >= 0 && p <= 1 }

// validAnswer checks an answer has the question's type and sane values.
func validAnswer(q jevQuestion, a jevAnswer) bool {
	if a.Type != q.Type {
		return false
	}
	switch q.Type {
	case "noul":
		return a.Noul != nil && validProb(*a.Noul)
	case "choice":
		if _, known := categoryCriteria[a.Choice]; !known || a.Probabilities == nil {
			return false
		}
		for k, p := range a.Probabilities {
			if _, known := categoryCriteria[k]; !known || !validProb(p) {
				return false
			}
		}
		return true
	case "score":
		if a.Score == nil || a.Probabilities == nil {
			return false
		}
		for k, p := range a.Probabilities {
			if n, err := strconv.Atoi(k); err != nil || n < 0 || n > 3 || !validProb(p) {
				return false
			}
		}
		return true
	}
	return false
}
