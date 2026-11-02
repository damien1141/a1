package tooldef

import (
	"context"
	"strings"
)

type toolCallIDKey struct{}

type cwdKey struct{}

type questionKey struct{}

type imageEnabledKey struct{}

// WithToolCallID attaches the active tool_use id to ctx for UI correlation.
func WithToolCallID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, toolCallIDKey{}, id)
}

// ToolCallID returns the tool_use id from ctx, or empty.
func ToolCallID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(toolCallIDKey{}).(string)
	return v
}

// WithCwd attaches the session working directory used to resolve relative tool paths.
// Empty cwd is ignored so callers can pass through an unset session cwd.
func WithCwd(ctx context.Context, cwd string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ctx
	}
	return context.WithValue(ctx, cwdKey{}, cwd)
}

func cwdFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(cwdKey{}).(string)
	return strings.TrimSpace(v)
}

// QuestionFunc asks the user a structured question and returns the answer.
// It is defined here (rather than importing the controller) so tools can depend
// on the seam without pulling the TUI into the tool layer.
type QuestionFunc func(ctx context.Context, q Question) (QuestionReply, error)

// Question is a single multiple-choice prompt with its options.
type Question struct {
	Header      string
	Question    string
	Options     []QuestionOption
	MultiSelect bool
}

// QuestionOption is one answer choice.
type QuestionOption struct {
	Label       string
	Description string
}

// QuestionReply is the user's answer. Choice holds the single pick, or the
// first pick when MultiSelect; Choices holds them all.
type QuestionReply struct {
	Choice    string
	Choices   []string
	Cancelled bool
}

// WithQuestionFunc attaches a question asker to ctx. A nil asker is not stored,
// so a tool without one fails closed with a clear message rather than blocking
// forever on nothing.
func WithQuestionFunc(ctx context.Context, fn QuestionFunc) context.Context {
	if ctx == nil || fn == nil {
		return ctx
	}
	return context.WithValue(ctx, questionKey{}, fn)
}

// QuestionFuncFrom returns the asker from ctx, or nil when none was attached.
func QuestionFuncFrom(ctx context.Context) QuestionFunc {
	if ctx == nil {
		return nil
	}
	fn, _ := ctx.Value(questionKey{}).(QuestionFunc)
	return fn
}

// WithImageEnabled tells tools whether the active model can see images. The
// executor attaches it next to WithCwd so a tool producing an optional image
// (og:image previews, screenshot blocks) can skip the encode work entirely
// for blind models instead of paying for bytes the provider would drop.
func WithImageEnabled(ctx context.Context, enabled bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, imageEnabledKey{}, enabled)
}

// ImageEnabledFrom reports whether the active model can see images. False
// (the default on a bare context) means tools should not spend effort
// attaching image blocks — the text result stands alone.
func ImageEnabledFrom(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(imageEnabledKey{}).(bool)
	return v
}
