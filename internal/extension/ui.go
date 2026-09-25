package extension

import ext "github.com/damien1141/a1/ext/go"

// BusUI publishes UI effects onto host callbacks.
type BusUI struct {
	NotifyFn    func(message, kind string)
	ConfirmFn   func(ext.ConfirmRequest) ext.ConfirmReply
	SetStatusFn func(key, text string)
}

func (u BusUI) Notify(message, kind string) {
	if u.NotifyFn != nil {
		u.NotifyFn(message, kind)
	}
}

func (u BusUI) Confirm(title, message string) bool {
	return u.ConfirmOpts(ext.ConfirmRequest{Title: title, Message: message}).OK
}

func (u BusUI) ConfirmOpts(req ext.ConfirmRequest) ext.ConfirmReply {
	if u.ConfirmFn != nil {
		return u.ConfirmFn(req)
	}
	return ext.ConfirmReply{}
}

func (u BusUI) SetStatus(key, text string) {
	if u.SetStatusFn != nil {
		u.SetStatusFn(key, text)
	}
}

var _ ext.UI = BusUI{}
