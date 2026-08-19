package translate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/1outres/sitka/internal/anthropic"
	"github.com/1outres/sitka/internal/openai"
)

// ErrToolCallIndex reports a streamed tool call fragment that does not say
// which call it belongs to.
var ErrToolCallIndex = errors.New("translate: stream tool call has no index")

// Stream reads OpenAI chunks from src and writes the equivalent Anthropic
// event sequence to dst.
func Stream(src *openai.StreamReader, dst *anthropic.SSEWriter, anthropicModel string) error {
	state := newStreamState(dst, anthropicModel)
	for {
		chunk, err := src.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return state.fail(err)
		}
		if err := state.consume(chunk); err != nil {
			return err
		}
	}
	return state.finish()
}

// streamState holds everything the event sequence needs to stay consistent:
// which blocks are open, how OpenAI tool call indices map to Anthropic block
// indices, and what the closing events must report.
type streamState struct {
	dst   *anthropic.SSEWriter
	model string

	started    bool
	nextIndex  int
	openBlocks []int
	textIndex  int
	textOpen   bool
	toolBlocks map[int]int
	stopReason *string
	usage      anthropic.Usage
}

func newStreamState(dst *anthropic.SSEWriter, model string) *streamState {
	return &streamState{dst: dst, model: model, toolBlocks: make(map[int]int)}
}

func (s *streamState) consume(chunk *openai.Chunk) error {
	if chunk.Usage != nil {
		s.usage = anthropic.Usage{
			InputTokens:  chunk.Usage.PromptTokens,
			OutputTokens: chunk.Usage.CompletionTokens,
		}
	}
	if err := s.start(chunk.ID); err != nil {
		return err
	}
	if len(chunk.Choices) == 0 {
		return nil
	}

	choice := chunk.Choices[0]
	if text := choice.Delta.Content; text != nil && *text != "" {
		if err := s.writeText(*text); err != nil {
			return err
		}
	}
	for _, call := range choice.Delta.ToolCalls {
		if err := s.writeToolCall(call); err != nil {
			return err
		}
	}
	if choice.FinishReason != nil {
		if reason := stopReason(*choice.FinishReason); reason != nil {
			s.stopReason = reason
		}
	}
	return nil
}

func (s *streamState) start(id string) error {
	if s.started {
		return nil
	}
	s.started = true
	return s.send(anthropic.EventMessageStart, anthropic.MessageStartEvent{
		Type: anthropic.EventMessageStart,
		Message: anthropic.Response{
			ID:      id,
			Type:    messageType,
			Role:    anthropic.RoleAssistant,
			Model:   s.model,
			Content: []anthropic.ContentBlock{},
			Usage:   s.usage,
		},
	})
}

func (s *streamState) finish() error {
	if err := s.start(""); err != nil {
		return err
	}
	if err := s.closeAll(); err != nil {
		return err
	}
	if err := s.send(anthropic.EventMessageDelta, anthropic.MessageDeltaEvent{
		Type:  anthropic.EventMessageDelta,
		Delta: anthropic.MessageDeltaBody{StopReason: s.finalStopReason()},
		Usage: s.usage,
	}); err != nil {
		return err
	}
	return s.send(anthropic.EventMessageStop, anthropic.MessageStopEvent{Type: anthropic.EventMessageStop})
}

func (s *streamState) writeText(text string) error {
	if !s.textOpen {
		index, err := s.openBlock(anthropic.ContentBlock{Type: anthropic.BlockText})
		if err != nil {
			return err
		}
		s.textIndex = index
		s.textOpen = true
	}
	return s.send(anthropic.EventContentBlockDelta, anthropic.ContentBlockDeltaEvent{
		Type:  anthropic.EventContentBlockDelta,
		Index: s.textIndex,
		Delta: anthropic.Delta{Type: anthropic.DeltaText, Text: text},
	})
}

func (s *streamState) writeToolCall(call openai.ToolCall) error {
	if call.Index == nil {
		return ErrToolCallIndex
	}

	index, known := s.toolBlocks[*call.Index]
	if !known {
		if err := s.closeText(); err != nil {
			return err
		}
		opened, err := s.openBlock(toolUseBlock(call.ID, call.Function.Name, json.RawMessage(emptyToolInput)))
		if err != nil {
			return err
		}
		s.toolBlocks[*call.Index] = opened
		index = opened
	}

	if call.Function.Arguments == "" {
		return nil
	}
	return s.send(anthropic.EventContentBlockDelta, anthropic.ContentBlockDeltaEvent{
		Type:  anthropic.EventContentBlockDelta,
		Index: index,
		Delta: anthropic.Delta{Type: anthropic.DeltaInputJSON, PartialJSON: call.Function.Arguments},
	})
}

func (s *streamState) openBlock(block anthropic.ContentBlock) (int, error) {
	index := s.nextIndex
	s.nextIndex++
	s.openBlocks = append(s.openBlocks, index)
	return index, s.send(anthropic.EventContentBlockStart, anthropic.ContentBlockStartEvent{
		Type:         anthropic.EventContentBlockStart,
		Index:        index,
		ContentBlock: block,
	})
}

func (s *streamState) closeText() error {
	if !s.textOpen {
		return nil
	}
	s.textOpen = false
	s.openBlocks = slices.DeleteFunc(s.openBlocks, func(open int) bool { return open == s.textIndex })
	return s.sendBlockStop(s.textIndex)
}

func (s *streamState) closeAll() error {
	open := s.openBlocks
	s.openBlocks = nil
	s.textOpen = false
	for _, index := range open {
		if err := s.sendBlockStop(index); err != nil {
			return err
		}
	}
	return nil
}

func (s *streamState) sendBlockStop(index int) error {
	return s.send(anthropic.EventContentBlockStop, anthropic.ContentBlockStopEvent{
		Type:  anthropic.EventContentBlockStop,
		Index: index,
	})
}

// finalStopReason names a reason even when the upstream never sent one,
// because a client waits for the stop reason to end the turn.
func (s *streamState) finalStopReason() *string {
	if s.stopReason != nil {
		return s.stopReason
	}
	reason := anthropic.StopEndTurn
	return &reason
}

func (s *streamState) fail(cause error) error {
	sendErr := s.dst.Send(anthropic.EventError, anthropic.ErrorEvent{
		Type:  anthropic.EventError,
		Error: anthropic.ErrorDetail{Type: anthropic.ErrAPI, Message: cause.Error()},
	})
	err := fmt.Errorf("translate: read upstream stream: %w", cause)
	if sendErr != nil {
		return errors.Join(err, fmt.Errorf("translate: send error event: %w", sendErr))
	}
	return err
}

func (s *streamState) send(event string, payload any) error {
	if err := s.dst.Send(event, payload); err != nil {
		return fmt.Errorf("translate: send %s event: %w", event, err)
	}
	return nil
}
