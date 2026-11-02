package session

import "swarg/internal/chat"

// ---------------------------------------------------------------------------
// Store: the Writer as the agent sees it
// ---------------------------------------------------------------------------

// Store adapts a Writer to the consumer interface `agent.Store`: the same calls, but the
// seq results are dropped (the agent numbers its own history and the Writer keeps a Seq that
// is above everything written). It adds no state and no locking of its own; every other
// agent.Store method is promoted from the embedded Writer.
type Store struct{ *Writer }

// NewStore wraps w.
func NewStore(w *Writer) *Store { return &Store{w} }

// Message appends m (see Writer.Message).
func (s *Store) Message(m chat.Message) error {
	_, err := s.Writer.Message(m)
	return err
}

// Compact records that fromSeq..toSeq were replaced by summary.
func (s *Store) Compact(fromSeq, toSeq int, summary chat.Message) error {
	_, err := s.Writer.Compact(fromSeq, toSeq, summary)
	return err
}

// Repair appends the synthetic cancelled results made when a session was resumed.
func (s *Store) Repair(ms []chat.Message) error {
	_, err := s.Writer.Repair(ms)
	return err
}
