package app

// This file is the ONLY channel screens use to talk to each other or to
// the root model. internal/screens/* must never import one another
// directly — see the cross-screen rule in docs/ARCHITECTURE.md §3.
//
// Add new message types here as later milestones need them, e.g.
// SupervisorStatusMsg (M2), BlobSelectedMsg (M3), FunctionInvokedMsg (M5).

// ScreenID names a routable screen. The dashboard is the only one wired
// up in M1; the rest are named here so the router type-checks against
// the full set from the start.
type ScreenID string

const (
	ScreenDashboard ScreenID = "dashboard"
	ScreenBlob      ScreenID = "blob"
	ScreenQueue     ScreenID = "queue"
	ScreenTable     ScreenID = "table"
	ScreenFunctions ScreenID = "functions"
	ScreenLogs      ScreenID = "logs"
	ScreenSnapshots ScreenID = "snapshots"
	ScreenProfiles  ScreenID = "profiles"
	ScreenSettings  ScreenID = "settings"
)

// SwitchScreenMsg asks the root model to focus a different screen. Any
// screen can emit this (e.g. pressing Enter on a dashboard tile) without
// knowing anything about the screen it's switching to beyond its ID.
type SwitchScreenMsg struct {
	Target ScreenID
}

// ErrorMsg surfaces a non-fatal error to the status bar instead of
// crashing the program. Screens should prefer this over panicking or
// silently swallowing errors.
type ErrorMsg struct {
	Err error
}
