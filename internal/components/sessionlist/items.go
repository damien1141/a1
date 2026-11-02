// Package sessionlist maps session.SessionMeta rows into listpicker items.
package sessionlist

import (
        "time"

        "github.com/damien1141/a1/internal/components/chrome"
        "github.com/damien1141/a1/internal/components/listpicker"
        "github.com/damien1141/a1/internal/session"
)

// NewSessionID is the sentinel ID for the synthetic "New session" row
// prepended to the picker. SessionCommands.Accept routes this ID to
// Clear() instead of resume(id). Using a sentinel (rather than a separate
// callback) keeps the listpicker's OnAccept contract uniform: every row
// produces an ID, and the caller decides what to do with it.
const NewSessionID = "__new__"

// Config returns chrome copy for the sessions picker.
func Config() listpicker.ShowConfig {
        return listpicker.ShowConfig{
                Title:       "Sessions",
                FilterHint:  "filter id or preview…",
                Empty:       "No sessions in this directory",
                EmptyFilter: "No sessions match %q",
                Hint:        chrome.ListHint("resume"),
        }
}

// Items maps session metas into generic list rows. A synthetic "New
// session" row is always prepended so the user can start a fresh session
// without leaving the picker — the row's ID (NewSessionID) is routed to
// Clear() by SessionCommands.Accept.
func Items(list []session.SessionMeta, currentID string, now time.Time) []listpicker.Item {
        if now.IsZero() {
                now = time.Now()
        }
        // Prepend the "New session" row. It always sits at the top so the
        // user can start fresh without scrolling past existing sessions.
        out := make([]listpicker.Item, 0, len(list)+1)
        out = append(out, listpicker.Item{
                ID:       NewSessionID,
                Leading:  "new",
                Primary:  "New session",
                Detail:   "start an empty session",
                Keywords: "new clear fresh",
        })
        for _, m := range list {
                detail := m.Preview
                if detail == "" {
                        detail = "(no preview)"
                }
                item := listpicker.Item{
                        ID:       m.ID,
                        Leading:  FormatRelative(m.Mtime, now),
                        Primary:  shortID(m.ID),
                        Detail:   detail,
                        Keywords: m.ID + " " + m.Preview,
                }
                if m.ID == currentID {
                        item.Badge = "current"
                }
                out = append(out, item)
        }
        return out
}

func shortID(id string) string {
        if len(id) > 8 {
                return id[:8]
        }
        return id
}
