package log

import (
	"os"
	"strconv"
	"sync"
)

type fileSink struct {
	mu   sync.Mutex
	f    *os.File
	min  *LevelVar
	path string
}

// NewFileSink builds a sink that appends records to path. The file is created
// if missing (0644). min is the threshold (a *LevelVar so it retunes live); a
// nil min emits every level.
func NewFileSink(path string, min *LevelVar) (Sink, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &fileSink{f: f, min: min, path: path}, nil
}

// Min reports the sink's current threshold (Debug when unset).
func (s *fileSink) Min() Level {
	if s.min == nil {
		return Debug
	}
	return s.min.Level()
}

// Write renders one record to the log file.
func (s *fileSink) Write(rec Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return
	}

	b := make([]byte, 0, 128)
	b = append(b, rec.Scope...)
	b = append(b, ' ', '[')
	b = append(b, levelString(rec.Level)...)
	b = append(b, ']', ' ')
	b = append(b, rec.Msg...)
	for _, f := range rec.Fields {
		b = append(b, ' ')
		b = append(b, f.Key...)
		b = append(b, '=')
		switch f.Kind {
		case KindStr:
			b = strconv.AppendQuote(b, f.s)
		case KindInt:
			b = strconv.AppendInt(b, f.i, 10)
		case KindBool:
			b = strconv.AppendBool(b, f.b)
		}
	}
	b = append(b, '\n')
	_, _ = s.f.Write(b)
}

// Close closes the log file.
func (s *fileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	return err
}
