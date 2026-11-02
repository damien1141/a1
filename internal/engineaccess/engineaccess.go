package engineaccess

import (
	"sync/atomic"

	"github.com/damien1141/a1/internal/permission"
)

// Engine describes the minimal engine surface permissiontool needs without
// importing internal/agent (which would create an import cycle).
type Engine interface {
	Gate() permission.Gate
}

var activeEngine atomic.Value

// SetActiveEngine registers the active engine for tool/harness access.
// Pass nil to clear.
func SetActiveEngine(e Engine) {
	activeEngine.Store(e)
}

// ActiveEngine returns the currently registered engine, or nil.
func ActiveEngine() Engine {
	v := activeEngine.Load()
	if v == nil {
		return nil
	}
	return v.(Engine)
}
