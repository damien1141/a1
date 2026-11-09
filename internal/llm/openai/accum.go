package openai

import (
	"strings"

	"github.com/damien1141/a1/internal/llm"
)

type streamAccumulator struct {
	role      llm.Role
	content   strings.Builder
	reasoning strings.Builder
	toolCalls map[int]*toolCallAcc
	maxIndex  int
}

type toolCallAcc struct {
	id   string
	typ  string
	name string
	args strings.Builder
}

func newStreamAccumulator() *streamAccumulator {
	return &streamAccumulator{
		toolCalls: make(map[int]*toolCallAcc),
		maxIndex:  -1,
	}
}

func (s *streamAccumulator) applyDelta(delta llm.StreamDelta) {
	if delta.Role != "" {
		s.role = llm.Role(delta.Role)
	}
	if delta.Content != "" {
		s.content.WriteString(delta.Content)
	}
	if delta.ReasoningContent != "" {
		s.reasoning.WriteString(delta.ReasoningContent)
	}
	for _, tc := range delta.ToolCalls {
		s.applyToolCallDelta(tc)
	}
}

func (s *streamAccumulator) applyMessage(msg *llm.Message) {
	if msg == nil {
		return
	}
	if strings.TrimSpace(msg.Content) != "" {
		s.content.Reset()
		s.content.WriteString(msg.Content)
	}
	if strings.TrimSpace(msg.ReasoningContent) != "" {
		s.reasoning.Reset()
		s.reasoning.WriteString(msg.ReasoningContent)
	}
	for i, tc := range msg.ToolCalls {
		s.applyToolCallMessage(tc, i)
	}
}

// applyToolCallMessage merges a complete tool call from a non-delta message
// echo. Unlike delta fragments, an echo carries the whole call, so its
// arguments REPLACE whatever fragments already accumulated — appending would
// concatenate the two copies into invalid JSON. Slots are assigned
// positionally because message echoes carry no stream index; a full echo
// lists every call in order, which is the only echo shape gateways actually
// send.
func (s *streamAccumulator) applyToolCallMessage(tc llm.ToolCall, slot int) {
	if slot > s.maxIndex {
		s.maxIndex = slot
	}
	acc := s.toolCalls[slot]
	if acc == nil {
		acc = &toolCallAcc{typ: "function"}
		s.toolCalls[slot] = acc
	}
	if tc.ID != "" {
		acc.id = tc.ID
	}
	if tc.Type != "" {
		acc.typ = tc.Type
	}
	if tc.Function.Name != "" {
		acc.name = tc.Function.Name
	}
	if args := tc.Function.Arguments; args != "" {
		acc.args.Reset()
		acc.args.WriteString(args)
	}
}

func (s *streamAccumulator) applyToolCallDelta(tc llm.ToolCall) {
	if tc.Index > s.maxIndex {
		s.maxIndex = tc.Index
	}
	acc := s.toolCalls[tc.Index]
	if acc == nil {
		acc = &toolCallAcc{typ: "function"}
		s.toolCalls[tc.Index] = acc
	}
	if acc.id == "" && tc.ID != "" {
		acc.id = tc.ID
	}
	if tc.Type != "" {
		acc.typ = tc.Type
	}
	if tc.Function.Name != "" {
		acc.name = tc.Function.Name
	}
	if tc.Function.Arguments != "" {
		acc.args.WriteString(tc.Function.Arguments)
	}
}

func (s *streamAccumulator) message() llm.Message {
	role := s.role
	if role == "" {
		role = llm.RoleAssistant
	}
	return llm.Message{
		Role:             role,
		Content:          s.content.String(),
		ReasoningContent: s.reasoning.String(),
		ToolCalls:        s.orderedToolCalls(),
	}
}

func (s *streamAccumulator) orderedToolCalls() []llm.ToolCall {
	if s.maxIndex < 0 {
		return nil
	}
	out := make([]llm.ToolCall, 0, s.maxIndex+1)
	for i := 0; i <= s.maxIndex; i++ {
		acc, ok := s.toolCalls[i]
		if !ok {
			continue
		}
		out = append(out, llm.ToolCall{
			Index: i,
			ID:    acc.id,
			Type:  acc.typ,
			Function: llm.Function{
				Name:      acc.name,
				Arguments: acc.args.String(),
			},
		})
	}
	return out
}
