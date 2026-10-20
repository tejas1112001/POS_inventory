package mailer

import (
	"context"
	"sync"
)

// FakeSender records messages instead of sending them (tests). Fail, if
// set, decides the outcome per message.
type FakeSender struct {
	mu   sync.Mutex
	Sent []Message
	Fail func(m Message) error
}

// Send implements Sender.
func (f *FakeSender) Send(_ context.Context, m Message) (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Fail != nil {
		if err := f.Fail(m); err != nil {
			return "", err
		}
	}
	f.Sent = append(f.Sent, m)
	return m.MessageID, nil
}

// Messages returns a copy of what was sent.
func (f *FakeSender) Messages() []Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Message(nil), f.Sent...)
}
