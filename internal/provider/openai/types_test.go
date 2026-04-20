package openai

import (
	"encoding/json"
	"testing"
)

func TestMessageContentMarshalTextAsString(t *testing.T) {
	payload, err := json.Marshal(TextContent("hello"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(payload) != `"hello"` {
		t.Fatalf("unexpected payload: %s", payload)
	}
}

func TestMessageContentUnmarshalArray(t *testing.T) {
	var content MessageContent
	if err := json.Unmarshal([]byte(`[{"type":"text","text":"hello"}]`), &content); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(content.Parts) != 1 || content.Parts[0].Text != "hello" {
		t.Fatalf("unexpected parts: %+v", content.Parts)
	}
}

func TestToolChoiceMarshalNamedFunction(t *testing.T) {
	payload, err := json.Marshal(ToolChoice{Mode: ToolChoiceModeFunction, FunctionName: "lookup_weather"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(payload) != `{"type":"function","function":{"name":"lookup_weather"}}` {
		t.Fatalf("unexpected payload: %s", payload)
	}
}
