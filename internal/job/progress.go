package job

import "time"

// Progress is a structured live update from a running job (UI overlay, not LLM context).
type Progress struct {
	JobID           string
	ParentToolUseID string // parent agent tool_use id when known
	ToolUseID       string
	Name            string
	Status          string // session.ToolStatus.String(); decode with session.ParseToolStatus
	Detail          string
	Time            time.Time
}
