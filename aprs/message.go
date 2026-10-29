package aprs

import (
	"fmt"
	"strings"
)

// MessageKind tells a message from an ack or a reject.
type MessageKind int

const (
	MessageText MessageKind = iota
	MessageAck
	MessageRej
)

// Message is an APRS message (":ADDRESSEE:text{id"), ack or reject.
type Message struct {
	Kind MessageKind
	To   string // the addressee, without padding
	Text string // empty for acks and rejects
	// ID is the message number; for an ack or reject, the number it answers.
	// Empty for a message that asks for no ack.
	ID string
	// ReplyAcks is set when the sender uses the reply-ack form "{MM}AA", and
	// ReplyAck is AA: the number of an earlier message that this one acks.
	ReplyAcks bool
	ReplyAck  string
}

// MaxMessageText is the longest message text APRS allows.
const MaxMessageText = 67

// IsBulletin reports whether the message is a bulletin or an announcement
// (addressee "BLN…").
func (m Message) IsBulletin() bool { return strings.HasPrefix(m.To, "BLN") }

// message decodes "ADDRESSEE:text" after the ':' data type.
func (d *Data) message(s string) error {
	if len(s) < 10 || s[9] != ':' {
		return fmt.Errorf("%w: message addressee is not nine bytes followed by ':'", ErrMalformed)
	}
	to := strings.TrimRight(s[:9], " ")
	if err := checkName(to); err != nil {
		return fmt.Errorf("%w: addressee %v", ErrMalformed, err)
	}
	m := parseMessageText(to, s[10:])
	d.Kind = KindMessage
	d.Message = &m
	return nil
}

func parseMessageText(to, text string) Message {
	m := Message{To: to}
	// "ackMM" and "rejMM", optionally with a reply-ack "}", never carry a
	// '{'. Text that merely starts with "ack" fails the number check.
	if !strings.Contains(text, "{") {
		for _, k := range []struct {
			prefix string
			kind   MessageKind
		}{{"ack", MessageAck}, {"rej", MessageRej}} {
			if rest, ok := strings.CutPrefix(text, k.prefix); ok {
				if id, ra, hasRA, ok := splitID(rest); ok {
					m.Kind, m.ID, m.ReplyAcks, m.ReplyAck = k.kind, id, hasRA, ra
					return m
				}
			}
		}
	}
	if i := strings.LastIndexByte(text, '{'); i >= 0 {
		if id, ra, hasRA, ok := splitID(text[i+1:]); ok {
			m.Text, m.ID, m.ReplyAcks, m.ReplyAck = text[:i], id, hasRA, ra
			return m
		}
	}
	m.Text = text
	return m
}

// splitID reads "MM" or "MM}AA" (AA may be empty).
func splitID(s string) (id, replyAck string, hasReplyAck, ok bool) {
	s = strings.TrimRight(s, " ")
	id, replyAck, hasReplyAck = strings.Cut(s, "}")
	if !validID(id) || (replyAck != "" && !validID(replyAck)) {
		return "", "", false, false
	}
	return id, replyAck, hasReplyAck, true
}

// validID accepts a message number: one to five letters or digits.
func validID(s string) bool {
	if len(s) < 1 || len(s) > 5 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isAlnum(s[i]) {
			return false
		}
	}
	return true
}

// MessageInfo formats a message's information field. id may be empty for a
// message that asks for no ack.
func MessageInfo(to, text, id string) (string, error) {
	if err := checkName(to); err != nil {
		return "", fmt.Errorf("addressee %v", err)
	}
	if text == "" || len(text) > MaxMessageText {
		return "", fmt.Errorf("message text must be 1 to %d characters", MaxMessageText)
	}
	for i := 0; i < len(text); i++ {
		if c := text[i]; c < ' ' || c > '~' || c == '|' || c == '~' || c == '{' {
			return "", fmt.Errorf("message text may not contain %q", c)
		}
	}
	if id != "" && !validID(id) {
		return "", fmt.Errorf("message number %q must be 1 to 5 letters or digits", id)
	}
	s := fmt.Sprintf(":%-9s:%s", to, text)
	if id != "" {
		s += "{" + id
	}
	return s, nil
}

// AckInfo formats an ack of message id from the station to.
func AckInfo(to, id string) (string, error) { return answerInfo(to, "ack", id) }

// RejInfo formats a reject of message id from the station to.
func RejInfo(to, id string) (string, error) { return answerInfo(to, "rej", id) }

func answerInfo(to, word, id string) (string, error) {
	if err := checkName(to); err != nil {
		return "", fmt.Errorf("addressee %v", err)
	}
	if !validID(id) {
		return "", fmt.Errorf("message number %q must be 1 to 5 letters or digits", id)
	}
	return fmt.Sprintf(":%-9s:%s%s", to, word, id), nil
}
