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

// engineBox wraps the stored engine so a nil Engine is representable:
// atomic.Value.Store rejects a nil interface outright, which used to turn
// the documented "Pass nil to clear" path into a panic.
type engineBox struct{ engine Engine }

var activeEngine atomic.Value // stores engineBox

// SetActiveEngine registers the active engine for tool/harness access.
// Pass nil to clear.
func SetActiveEngine(e Engine) {
	activeEngine.Store(engineBox{engine: e})
}

// ActiveEngine returns the currently registered engine, or nil.
func ActiveEngine() Engine {
	v := activeEngine.Load()
	if v == nil {
		return nil
	}
	return v.(engineBox).engine
}
