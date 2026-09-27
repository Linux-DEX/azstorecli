// Package msg holds every cross-screen tea.Msg type.
//
// ARCHITECTURE.md §3 puts these in internal/app/messages.go, but that
// cannot compile: internal/app imports every screen to register it, so a
// screen emitting app.SwitchScreenMsg would close an import cycle. Moving
// the vocabulary into a leaf package both sides import preserves the rule
// that matters — screens never import each other — without the cycle.
package msg

import (
	"time"

	"github.com/Linux-DEX/azstorecli/internal/supervisor"
)

// ScreenID names a routable screen.
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

// Ordered is the 1-8 number-key order shown in the help bar.
var Ordered = []ScreenID{
	ScreenDashboard, ScreenBlob, ScreenQueue, ScreenTable,
	ScreenFunctions, ScreenLogs, ScreenSnapshots, ScreenProfiles,
}

// Titles are the short labels the help bar renders.
var Titles = map[ScreenID]string{
	ScreenDashboard: "Dash",
	ScreenBlob:      "Blob",
	ScreenQueue:     "Queue",
	ScreenTable:     "Table",
	ScreenFunctions: "Func",
	ScreenLogs:      "Logs",
	ScreenSnapshots: "Snap",
	ScreenProfiles:  "Prof",
	ScreenSettings:  "Settings",
}

// SwitchScreen asks the root model to focus a different screen.
type SwitchScreen struct{ Target ScreenID }

// Error surfaces a non-fatal error as a toast instead of crashing.
type Error struct{ Err error }

// Status surfaces a transient informational message in the status bar.
type Status struct {
	Text    string
	Warning bool
}

// ServiceState reports a supervised process changing state.
type ServiceState struct {
	Name  string
	State supervisor.State
	PID   int
	Err   error
}

// Log carries one line out of a process log ring to the log screen and
// the Ctrl+L drawer.
type Log struct {
	Process string
	Line    supervisor.LogLine
}

// ProfileChanged tells every explorer screen to drop its cached storage
// client and re-list against the newly activated profile.
type ProfileChanged struct{ Name string }

// FunctionsLoaded carries the route table parsed out of the Functions
// host's stdout.
type FunctionsLoaded struct{ Routes []Route }

// Route is one discovered function, flattened for display.
type Route struct {
	Name     string
	Trigger  string
	Methods  []string
	URL      string
	Disabled bool
}

// SnapshotComplete reports the end of a save or restore.
type SnapshotComplete struct {
	Name     string
	Bytes    int64
	Duration time.Duration
	Restored bool
}

// SnapshotInFlight brackets a snapshot so the logs screen can suppress
// the storage-connection errors the Functions host emits while Azurite
// is briefly down (ARCHITECTURE.md §9).
type SnapshotInFlight struct{ Active bool }

// FocusQueue / FocusBlob let one screen deep-link into another by ID
// only, without importing it.
type FocusQueue struct{ Queue string }

// FocusBlob deep-links the blob explorer at a container and prefix.
type FocusBlob struct {
	Container string
	Prefix    string
}

// FocusLogs opens the logs screen pre-filtered to one source.
type FocusLogs struct{ Source string }

// FocusTable deep-links the table explorer.
type FocusTable struct{ Table string }

// Action is a palette (or keymap) action the root or the focused
// screen should interpret.
type Action struct{ ID string }

// OpenModal asks the root model to capture input in a modal.
type OpenModal struct {
	Kind        ModalKind
	Title       string
	Body        string
	Prompt      string
	Placeholder string
	Value       string
	Destructive bool
	Action      string
}

// ModalKind is the shape of an OpenModal.
type ModalKind int

const (
	ModalConfirm ModalKind = iota
	ModalInput
	ModalNotice
)

// ModalResult is the user's answer, forwarded to the focused screen.
type ModalResult struct {
	Action string
	OK     bool
	Value  string
}

// Edited is the result of an $EDITOR session.
type Edited struct {
	Kind    string
	Content string
	Err     error
}

// ConfirmQuit asks the root to prompt before exiting.
type ConfirmQuit struct{}

// Refresh asks the focused screen to reload its data.
type Refresh struct{}

// UnlockReadOnly temporarily lifts the profile write guard.
type UnlockReadOnly struct{}
