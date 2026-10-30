package session

import "swarg/internal/chat"

// ---------------------------------------------------------------------------
// Store: the Writer as the agent sees it
// ---------------------------------------------------------------------------

// Store adapts a Writer to the consumer interface `agent.Store`: the same calls, but the
// seq results are dropped (the agent numbers its own history and the Writer keeps a Seq that
// is above everything written). It adds no state and no locking of its own.
type Store struct{ w *Writer }

// NewStore wraps w.
func NewStore(w *Writer) *Store { return &Store{w: w} }

// Message appends m (see Writer.Message).
func (s *Store) Message(m chat.Message) error {
	_, err := s.w.Message(m)
	return err
}

// Patch records one stub / argument-shrink batch.
func (s *Store) Patch(ps []chat.Patch) error { return s.w.Patch(ps) }

// Clear records that the history was emptied.
func (s *Store) Clear() error { return s.w.Clear() }

// Compact records that fromSeq..toSeq were replaced by summary.
func (s *Store) Compact(fromSeq, toSeq int, summary chat.Message) error {
	_, err := s.w.Compact(fromSeq, toSeq, summary)
	return err
}

// Repair appends the synthetic cancelled results made when a session was resumed.
func (s *Store) Repair(ms []chat.Message) error {
	_, err := s.w.Repair(ms)
	return err
}

// Flush forces the file to stable storage.
func (s *Store) Flush() error { return s.w.Flush() }

// Thinking records a thinking mode switch.
func (s *Store) Thinking(on bool) error { return s.w.Thinking(on) }

// Model records a Model choice.
func (s *Store) Model(name string) error { return s.w.Model(name) }

// Provider records a Source switch.
func (s *Store) Provider(name string) error { return s.w.Provider(name) }
