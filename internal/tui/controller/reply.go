package controller

// AskReply is the user's response for a gated tool confirmation.
type AskReply struct {
	Approved        bool
	Feedback        string
	AllowSession    bool // Allow All for This Session
	AllowPersistent bool // Allow All for Every Session
	// AllowlistPattern, when non-empty, means the command is approved and this
	// regex should be persisted to the bash allowlist in config.yaml.
	AllowlistPattern string
}

// ExtConfirmReply is the user's response for an extension Confirm dialog.
type ExtConfirmReply struct {
	OK bool
}

// ContinueReply is the user's response when the tool-round budget is exhausted.
type ContinueReply struct {
	Continue bool
}
