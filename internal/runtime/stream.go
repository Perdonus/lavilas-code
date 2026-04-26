package runtime

import (
	"io"
	"strings"
	"time"
)

type Stream interface {
	Recv() (StreamEvent, error)
	Close() error
}

type StreamEventType string

const (
	StreamEventTypeDelta      StreamEventType = "delta"
	StreamEventTypeChoiceDone StreamEventType = "choice_done"
	StreamEventTypeUsage      StreamEventType = "usage"
	StreamEventTypeDone       StreamEventType = "done"
)

type StreamEvent struct {
	Type         StreamEventType
	ResponseID   string
	Model        string
	CreatedAt    time.Time
	ChoiceIndex  int
	Delta        MessageDelta
	ReasoningDelta string
	FinishReason FinishReason
	Usage        *Usage
}

type MessageDelta struct {
	Role      Role
	Content   []ContentPartDelta
	ToolCalls []ToolCallDelta
}

func (d MessageDelta) Empty() bool {
	return d.Role == "" && len(d.Content) == 0 && len(d.ToolCalls) == 0
}

func (d MessageDelta) Text() string {
	var parts []string
	for _, part := range d.Content {
		if part.Type == ContentPartTypeText && strings.TrimSpace(part.Text) != "" {
			parts = append(parts, part.Text)
		}
	}
	return strings.Join(parts, "")
}

type ContentPartDelta struct {
	Type ContentPartType
	Text string
}

type ToolCallDelta struct {
	Index          int
	ID             string
	Type           ToolType
	NameDelta      string
	ArgumentsDelta string
}

var ErrStreamDone = io.EOF
