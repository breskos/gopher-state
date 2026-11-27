package workflow

import (
	"encoding/json"
	"io"
	"maps"
	"sync"
)

// Record describes a successful transition and its effective eligibility rule.
// Condition combines requirements and when. Facts includes present operands
// referenced by the rule, keeping unrelated event data out of the log.
type Record struct {
	Meta        map[string]json.RawMessage `json:"meta,omitempty"`
	RunID       string                     `json:"runId"`
	Sequence    uint64                     `json:"sequence"`
	Decision    Decision                   `json:"decision"`
	Selection   Selection                  `json:"selection"`
	Weight      uint64                     `json:"weight,omitempty"`
	Probability float64                    `json:"probability"`
	Condition   *Condition                 `json:"condition,omitempty"`
	Facts       Facts                      `json:"facts,omitempty"`
	Encounter   string                     `json:"encounter,omitempty"`
	Shared      *Decision                  `json:"shared,omitempty"`
}

// Recorder receives a transition record before a run advances. Recorders
// shared by several runs must support concurrent calls.
type Recorder interface {
	WriteRecord(Record) error
}

// GroupRecorder records a joint action in one call. It must return an error
// if the group cannot be recorded. Shared implementations must be concurrent-safe.
type GroupRecorder interface {
	WriteGroup([]Record) error
}

// RecorderFunc adapts a function to Recorder.
type RecorderFunc func(Record) error

// WriteRecord calls f.
func (f RecorderFunc) WriteRecord(record Record) error { return f(record) }

// MemoryRecorder stores transition records in write order. Its zero value is
// ready to use. Do not copy it after use.
type MemoryRecorder struct {
	mu      sync.Mutex
	records []Record
}

// WriteRecord stores a private copy.
func (m *MemoryRecorder) WriteRecord(record Record) error {
	record = cloneRecord(record)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, record)
	return nil
}

// WriteGroup appends all records as one uninterrupted block.
func (m *MemoryRecorder) WriteGroup(records []Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, record := range records {
		m.records = append(m.records, cloneRecord(record))
	}
	return nil
}

// Records returns a snapshot that the caller may modify.
func (m *MemoryRecorder) Records() []Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	records := make([]Record, len(m.records))
	for i, record := range m.records {
		records[i] = cloneRecord(record)
	}
	return records
}

func cloneRecord(record Record) Record {
	record.Facts = maps.Clone(record.Facts)
	record.Meta = cloneMetadata(record.Meta)
	if record.Shared != nil {
		shared := *record.Shared
		record.Shared = &shared
	}
	if record.Condition != nil {
		condition := cloneCondition(*record.Condition)
		record.Condition = &condition
	}
	return record
}

func cloneCondition(c Condition) Condition {
	if c.All != nil {
		all := make([]Condition, len(c.All))
		for i, child := range c.All {
			all[i] = cloneCondition(child)
		}
		c.All = all
	}
	if c.Any != nil {
		anyOf := make([]Condition, len(c.Any))
		for i, child := range c.Any {
			anyOf[i] = cloneCondition(child)
		}
		c.Any = anyOf
	}
	if c.Not != nil {
		not := cloneCondition(*c.Not)
		c.Not = &not
	}
	return c
}

// JSONLRecorder writes one JSON record per line and serializes concurrent writes.
type JSONLRecorder struct {
	mu      sync.Mutex
	encoder *json.Encoder
}

// NewJSONLRecorder writes to the supplied writer. The caller manages flushing,
// syncing and closing the writer.
func NewJSONLRecorder(w io.Writer) *JSONLRecorder {
	return &JSONLRecorder{encoder: json.NewEncoder(w)}
}

// WriteRecord encodes one record followed by a newline.
func (j *JSONLRecorder) WriteRecord(record Record) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.encoder.Encode(record)
}

// WriteGroup encodes one JSON line with a group array of joint records.
func (j *JSONLRecorder) WriteGroup(records []Record) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.encoder.Encode(struct {
		Group []Record `json:"group"`
	}{Group: records})
}

func (p *predicate) describe(facts, used Facts) Condition {
	switch p.op {
	case opAll, opAny:
		children := make([]Condition, len(p.children))
		for i := range p.children {
			children[i] = p.children[i].describe(facts, used)
		}
		if p.op == opAll {
			return Condition{All: children}
		}
		return Condition{Any: children}
	case opNot:
		child := p.children[0].describe(facts, used)
		return Condition{Not: &child}
	default:
		for _, field := range []string{p.field, p.other} {
			if field != "" {
				if value := facts[field]; value.valid() {
					used[field] = value
				}
			}
		}
		names := [...]string{"exists", "eq", "ne", "lt", "lte", "gt", "gte"}
		return Condition{Field: p.field, Op: names[p.op-opExists], Value: p.value, OtherField: p.other}
	}
}
