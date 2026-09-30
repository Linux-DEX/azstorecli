package keymap

import "sort"

// Scope names where an action applies. The empty scope is global.
const (
	ScopeGlobal    = ""
	ScopeDashboard = "dashboard"
	ScopeBlob      = "blob"
	ScopeQueue     = "queue"
	ScopeTable     = "table"
	ScopeFunctions = "functions"
	ScopeLogs      = "logs"
	ScopeSnapshots = "snapshots"
	ScopeProfiles  = "profiles"
)

// Action is one addressable thing the user can do.
//
// ARCHITECTURE.md §7 gave Action a `Run func(*app.Deps) tea.Cmd` field.
// That cannot compile — keymap would import app, app imports screens,
// and screens import keymap. Instead an action is pure data, and the
// palette emits its ID as a message that the focused screen or the root
// model interprets. One registry still feeds the palette, the help
// overlay, and keymap.yaml validation, which was the point.
type Action struct {
	ID    string   // "snapshot.save"
	Title string   // "snapshot save"
	Desc  string   // "Save current workspace"
	Scope string   // "" = global
	Keys  []string // default bindings
}

// registry is the single source of truth. Adding an entry here makes an
// action discoverable in the palette, the help overlay, and
// `azstore keys list` at once.
var registry = []Action{
	// ---- global -----------------------------------------------------
	{ID: "app.quit", Title: "quit", Desc: "Back one level, or quit from the root", Keys: []string{"q"}},
	{ID: "app.force_quit", Title: "force quit", Desc: "Quit now, stopping child processes", Keys: []string{"ctrl+c"}},
	{ID: "app.palette", Title: "command palette", Desc: "Fuzzy-search every action", Keys: []string{":", "ctrl+k"}},
	{ID: "app.help", Title: "help", Desc: "Show keybindings in a dialog", Keys: []string{"?"}},
	{ID: "app.filter", Title: "filter", Desc: "Filter or search the current list", Keys: []string{"/"}},
	{ID: "app.escape", Title: "cancel", Desc: "Close a modal, clear a filter, pop focus", Keys: []string{"esc"}},
	{ID: "app.refresh", Title: "refresh", Desc: "Reload the current view", Keys: []string{"ctrl+r", "f5"}},
	{ID: "app.next_pane", Title: "next pane", Desc: "Cycle pane focus within the screen", Keys: []string{"tab"}},
	{ID: "app.prev_pane", Title: "previous pane", Desc: "Cycle pane focus backwards", Keys: []string{"shift+tab"}},
	{ID: "app.log_drawer", Title: "toggle log drawer", Desc: "Show or hide the bottom log split", Keys: []string{"ctrl+l"}},
	{ID: "app.profile_switch", Title: "switch profile", Desc: "Jump to connection profiles", Keys: []string{"ctrl+p"}},
	{ID: "app.snapshot_save", Title: "snapshot save", Desc: "Quick-save a workspace snapshot", Keys: []string{"ctrl+s"}},
	{ID: "app.unlock", Title: "unlock read-only", Desc: "Temporarily allow writes on a read-only profile", Keys: []string{"ctrl+w"}},
	{ID: "app.yank", Title: "yank", Desc: "Copy the selected item's identifier", Keys: []string{"y"}},
	{ID: "nav.top", Title: "go to top", Desc: "Jump to the first row", Keys: []string{"g", "home"}},
	{ID: "nav.bottom", Title: "go to bottom", Desc: "Jump to the last row", Keys: []string{"G", "end"}},
	{ID: "nav.up", Title: "up", Desc: "Move the cursor up", Keys: []string{"k", "up"}},
	{ID: "nav.down", Title: "down", Desc: "Move the cursor down", Keys: []string{"j", "down"}},
	{ID: "nav.left", Title: "left", Desc: "Move focus left, or up one level", Keys: []string{"h", "left"}},
	{ID: "nav.right", Title: "right", Desc: "Move focus right, or descend", Keys: []string{"l", "right"}},
	{ID: "nav.half_down", Title: "half page down", Desc: "Scroll down half a page", Keys: []string{"ctrl+d"}},
	{ID: "nav.half_up", Title: "half page up", Desc: "Scroll up half a page", Keys: []string{"ctrl+u"}},
	{ID: "nav.select", Title: "select", Desc: "Activate the highlighted row", Keys: []string{"enter"}},

	{ID: "screen.dashboard", Title: "go to dashboard", Desc: "Service status and recent activity", Keys: []string{"1"}},
	{ID: "screen.blob", Title: "go to blob explorer", Desc: "Containers, blobs, and previews", Keys: []string{"2"}},
	{ID: "screen.queue", Title: "go to queue explorer", Desc: "Queues and messages", Keys: []string{"3"}},
	{ID: "screen.table", Title: "go to table explorer", Desc: "Tables and entities", Keys: []string{"4"}},
	{ID: "screen.functions", Title: "go to functions", Desc: "Route table and invoke pane", Keys: []string{"5"}},
	{ID: "screen.logs", Title: "go to logs", Desc: "Merged process output", Keys: []string{"6"}},
	{ID: "screen.snapshots", Title: "go to snapshots", Desc: "Saved workspace archives", Keys: []string{"7"}},
	{ID: "screen.profiles", Title: "go to profiles", Desc: "Connection profiles", Keys: []string{"8"}},

	// ---- dashboard --------------------------------------------------
	{ID: "service.start", Title: "start service", Desc: "Start the selected service", Scope: ScopeDashboard, Keys: []string{"s"}},
	{ID: "service.stop", Title: "stop service", Desc: "Stop the selected service", Scope: ScopeDashboard, Keys: []string{"x"}},
	{ID: "service.restart", Title: "restart service", Desc: "Restart the selected service", Scope: ScopeDashboard, Keys: []string{"r"}},
	{ID: "service.start_all", Title: "start all", Desc: "Start everything in dependency order", Scope: ScopeDashboard, Keys: []string{"S"}},
	{ID: "service.stop_all", Title: "stop all", Desc: "Stop everything in reverse order", Scope: ScopeDashboard, Keys: []string{"X"}},
	{ID: "service.restart_all", Title: "restart all", Desc: "Restart the whole stack", Scope: ScopeDashboard, Keys: []string{"R"}},
	{ID: "service.logs", Title: "service logs", Desc: "Open logs filtered to this service", Scope: ScopeDashboard, Keys: []string{"l"}},
	{ID: "service.open_workspace", Title: "open workspace", Desc: "Reveal the workspace directory", Scope: ScopeDashboard, Keys: []string{"o"}},
	{ID: "service.edit_config", Title: "edit config", Desc: "Edit the project config in $EDITOR", Scope: ScopeDashboard, Keys: []string{"e"}},
	{ID: "service.copy_connstr", Title: "copy connection string", Desc: "Yank the connection string", Scope: ScopeDashboard, Keys: []string{"c"}},
	{ID: "service.doctor", Title: "doctor", Desc: "Check node, func, ports, and versions", Scope: ScopeDashboard, Keys: []string{"d"}},

	// ---- blob -------------------------------------------------------
	{ID: "blob.upload", Title: "upload", Desc: "Upload a local file", Scope: ScopeBlob, Keys: []string{"u"}},
	{ID: "blob.upload_dir", Title: "upload directory", Desc: "Upload a directory, preserving prefixes", Scope: ScopeBlob, Keys: []string{"U"}},
	{ID: "blob.download", Title: "download", Desc: "Download the selection", Scope: ScopeBlob, Keys: []string{"d"}},
	{ID: "blob.delete", Title: "delete", Desc: "Delete the selection", Scope: ScopeBlob, Keys: []string{"D", "delete"}},
	{ID: "blob.new", Title: "new container", Desc: "Create a container or virtual directory", Scope: ScopeBlob, Keys: []string{"n"}},
	{ID: "blob.properties", Title: "properties", Desc: "View and edit properties and metadata", Scope: ScopeBlob, Keys: []string{"p"}},
	{ID: "blob.sas", Title: "generate SAS", Desc: "Build a SAS URL with permissions and expiry", Scope: ScopeBlob, Keys: []string{"s"}},
	{ID: "blob.yank_content", Title: "yank content", Desc: "Copy blob content to the clipboard", Scope: ScopeBlob, Keys: []string{"Y"}},
	{ID: "blob.edit", Title: "edit blob", Desc: "Edit in $EDITOR and upload on save", Scope: ScopeBlob, Keys: []string{"e"}},
	{ID: "blob.copy", Title: "copy blob", Desc: "Copy to another container", Scope: ScopeBlob, Keys: []string{"c"}},
	{ID: "blob.move", Title: "move blob", Desc: "Move or rename", Scope: ScopeBlob, Keys: []string{"m"}},
	{ID: "blob.tier", Title: "change tier", Desc: "Toggle Hot / Cool / Archive", Scope: ScopeBlob, Keys: []string{"t"}},
	{ID: "blob.hexview", Title: "toggle hex view", Desc: "Show binary content as a hex dump", Scope: ScopeBlob, Keys: []string{"v"}},
	{ID: "blob.versions", Title: "blob versions", Desc: "List snapshots and versions", Scope: ScopeBlob, Keys: []string{"V"}},
	{ID: "blob.break_lease", Title: "break lease", Desc: "Force-release a blob lease", Scope: ScopeBlob, Keys: []string{"L"}},
	{ID: "blob.deep_search", Title: "deep search", Desc: "Search names across every container", Scope: ScopeBlob, Keys: []string{"ctrl+f"}},
	{ID: "blob.sort", Title: "sort", Desc: "Sort by name, size, or modified", Scope: ScopeBlob, Keys: []string{"o"}},
	{ID: "blob.multi_select", Title: "toggle selection", Desc: "Add or remove the row from the selection", Scope: ScopeBlob, Keys: []string{" "}},
	{ID: "blob.select_all", Title: "select all", Desc: "Select every row in view", Scope: ScopeBlob, Keys: []string{"ctrl+a"}},
	{ID: "blob.up_level", Title: "up one prefix", Desc: "Leave the current virtual directory", Scope: ScopeBlob, Keys: []string{"backspace"}},
	{ID: "blob.load_full", Title: "load full preview", Desc: "Fetch the whole blob into the preview", Scope: ScopeBlob, Keys: []string{"F"}},

	// ---- queue ------------------------------------------------------
	{ID: "queue.add", Title: "add message", Desc: "Compose a message in $EDITOR", Scope: ScopeQueue, Keys: []string{"a"}},
	{ID: "queue.add_file", Title: "add message from file", Desc: "Enqueue a file's contents", Scope: ScopeQueue, Keys: []string{"A"}},
	{ID: "queue.delete", Title: "delete message", Desc: "Delete the selected message", Scope: ScopeQueue, Keys: []string{"D"}},
	{ID: "queue.clear", Title: "clear queue", Desc: "Delete every message", Scope: ScopeQueue, Keys: []string{"C"}},
	{ID: "queue.peek", Title: "peek", Desc: "Refresh without dequeuing", Scope: ScopeQueue, Keys: []string{"P"}},
	{ID: "queue.watch", Title: "watch queue", Desc: "Auto-poll every second", Scope: ScopeQueue, Keys: []string{"w"}},
	{ID: "queue.requeue", Title: "requeue", Desc: "Move a message to another queue", Scope: ScopeQueue, Keys: []string{"R"}},
	{ID: "queue.visibility", Title: "update visibility", Desc: "Change the visibility timeout", Scope: ScopeQueue, Keys: []string{"u"}},
	{ID: "queue.base64", Title: "toggle base64", Desc: "Switch between raw and decoded text", Scope: ScopeQueue, Keys: []string{"b"}},
	{ID: "queue.new", Title: "new queue", Desc: "Create a queue", Scope: ScopeQueue, Keys: []string{"n"}},
	{ID: "queue.properties", Title: "queue properties", Desc: "Metadata and approximate count", Scope: ScopeQueue, Keys: []string{"p"}},
	{ID: "queue.trigger", Title: "trigger function", Desc: "Run the bound function against this message", Scope: ScopeQueue, Keys: []string{"t"}},

	// ---- table ------------------------------------------------------
	{ID: "table.filter", Title: "edit filter", Desc: "Edit the OData filter", Scope: ScopeTable, Keys: []string{"f"}},
	{ID: "table.saved_queries", Title: "saved queries", Desc: "Pick from queries.yaml", Scope: ScopeTable, Keys: []string{"F"}},
	{ID: "table.new", Title: "new entity", Desc: "Add an entity", Scope: ScopeTable, Keys: []string{"n"}},
	{ID: "table.edit", Title: "edit entity", Desc: "Edit the entity as JSON in $EDITOR", Scope: ScopeTable, Keys: []string{"E"}},
	{ID: "table.delete", Title: "delete entity", Desc: "Delete the selected entity", Scope: ScopeTable, Keys: []string{"D"}},
	{ID: "table.columns", Title: "choose columns", Desc: "Show, hide, and reorder columns", Scope: ScopeTable, Keys: []string{"c"}},
	{ID: "table.sort", Title: "sort column", Desc: "Sort by the current column", Scope: ScopeTable, Keys: []string{"o"}},
	{ID: "table.export", Title: "export", Desc: "Write the result set to CSV or JSON", Scope: ScopeTable, Keys: []string{"x"}},
	{ID: "table.import", Title: "import", Desc: "Load entities from CSV or JSON", Scope: ScopeTable, Keys: []string{"i"}},
	{ID: "table.next_page", Title: "next page", Desc: "Follow the continuation token", Scope: ScopeTable, Keys: []string{">"}},
	{ID: "table.prev_page", Title: "previous page", Desc: "Go back one page", Scope: ScopeTable, Keys: []string{"<"}},
	{ID: "table.new_table", Title: "new table", Desc: "Create a table", Scope: ScopeTable, Keys: []string{"N"}},

	// ---- functions --------------------------------------------------
	{ID: "func.invoke", Title: "invoke function", Desc: "Run the selected function", Scope: ScopeFunctions, Keys: []string{"enter"}},
	{ID: "func.edit_body", Title: "edit request body", Desc: "Edit the body in $EDITOR", Scope: ScopeFunctions, Keys: []string{"e"}},
	{ID: "func.headers", Title: "edit headers", Desc: "Edit headers and query parameters", Scope: ScopeFunctions, Keys: []string{"H"}},
	{ID: "func.start", Title: "start host", Desc: "Start the Functions host", Scope: ScopeFunctions, Keys: []string{"s"}},
	{ID: "func.stop", Title: "stop host", Desc: "Stop the Functions host", Scope: ScopeFunctions, Keys: []string{"x"}},
	{ID: "func.restart", Title: "restart host", Desc: "Restart the Functions host", Scope: ScopeFunctions, Keys: []string{"r"}},
	{ID: "func.watch", Title: "toggle watch mode", Desc: "Restart the host on source change", Scope: ScopeFunctions, Keys: []string{"w"}},
	{ID: "func.logs", Title: "function logs", Desc: "Logs filtered to this function", Scope: ScopeFunctions, Keys: []string{"l"}},
	{ID: "func.settings", Title: "edit settings", Desc: "Edit local.settings.json", Scope: ScopeFunctions, Keys: []string{"E"}},
	{ID: "func.open_source", Title: "open source", Desc: "Open the source file in $EDITOR", Scope: ScopeFunctions, Keys: []string{"o"}},
	{ID: "func.curl", Title: "copy as curl", Desc: "Yank the invocation as a curl command", Scope: ScopeFunctions, Keys: []string{"c"}},
	{ID: "func.save_request", Title: "save request", Desc: "Save the current request for replay", Scope: ScopeFunctions, Keys: []string{"S"}},
	{ID: "func.replay", Title: "replay request", Desc: "Load a saved request", Scope: ScopeFunctions, Keys: []string{"R"}},
	{ID: "func.trigger_target", Title: "go to trigger target", Desc: "Jump to the bound queue, blob, or table", Scope: ScopeFunctions, Keys: []string{"t"}},
	{ID: "func.toggle_enabled", Title: "enable/disable", Desc: "Patch the function's disabled flag", Scope: ScopeFunctions, Keys: []string{"d"}},

	// ---- logs -------------------------------------------------------
	{ID: "logs.pause", Title: "pause logs", Desc: "Pause or resume auto-scroll", Scope: ScopeLogs, Keys: []string{" "}},
	{ID: "logs.source_all", Title: "all sources", Desc: "Show every process", Scope: ScopeLogs, Keys: []string{"1"}},
	{ID: "logs.source_azurite", Title: "azurite only", Desc: "Show only Azurite output", Scope: ScopeLogs, Keys: []string{"2"}},
	{ID: "logs.source_functions", Title: "functions only", Desc: "Show only Functions host output", Scope: ScopeLogs, Keys: []string{"3"}},
	{ID: "logs.filter", Title: "filter logs", Desc: "Filter by level, function, or regex", Scope: ScopeLogs, Keys: []string{"f"}},
	{ID: "logs.wrap", Title: "toggle wrap", Desc: "Wrap long lines", Scope: ScopeLogs, Keys: []string{"w"}},
	{ID: "logs.clear", Title: "clear buffer", Desc: "Empty the log buffer", Scope: ScopeLogs, Keys: []string{"x"}},
	{ID: "logs.save", Title: "save logs", Desc: "Write the visible buffer to a file", Scope: ScopeLogs, Keys: []string{"S"}},
	{ID: "logs.timestamps", Title: "toggle timestamps", Desc: "Show or hide timestamps", Scope: ScopeLogs, Keys: []string{"t"}},
	{ID: "logs.json", Title: "toggle JSON pretty-print", Desc: "Expand structured log lines", Scope: ScopeLogs, Keys: []string{"J"}},
	{ID: "logs.follow", Title: "follow tail", Desc: "Jump to the tail and re-follow", Scope: ScopeLogs, Keys: []string{"G"}},

	// ---- snapshots --------------------------------------------------
	{ID: "snapshot.save", Title: "snapshot save", Desc: "Save the current workspace", Scope: ScopeSnapshots, Keys: []string{"n"}},
	{ID: "snapshot.restore", Title: "snapshot restore", Desc: "Restore a snapshot, autosaving first", Scope: ScopeSnapshots, Keys: []string{"enter"}},
	{ID: "snapshot.delete", Title: "snapshot delete", Desc: "Delete a snapshot", Scope: ScopeSnapshots, Keys: []string{"D"}},
	{ID: "snapshot.rename", Title: "snapshot rename", Desc: "Rename or edit notes", Scope: ScopeSnapshots, Keys: []string{"r"}},
	{ID: "snapshot.export", Title: "snapshot export", Desc: "Export the archive to a path", Scope: ScopeSnapshots, Keys: []string{"e"}},
	{ID: "snapshot.import", Title: "snapshot import", Desc: "Import an archive from disk", Scope: ScopeSnapshots, Keys: []string{"i"}},
	{ID: "snapshot.diff", Title: "snapshot diff", Desc: "Compare against the live workspace", Scope: ScopeSnapshots, Keys: []string{"d"}},
	{ID: "snapshot.verify", Title: "snapshot verify", Desc: "Recompute and check the checksum", Scope: ScopeSnapshots, Keys: []string{"v"}},

	// ---- profiles ---------------------------------------------------
	{ID: "profile.activate", Title: "activate profile", Desc: "Rebind every explorer to this profile", Scope: ScopeProfiles, Keys: []string{"enter"}},
	{ID: "profile.new", Title: "new profile", Desc: "Add a connection profile", Scope: ScopeProfiles, Keys: []string{"n"}},
	{ID: "profile.edit", Title: "edit profile", Desc: "Edit the selected profile", Scope: ScopeProfiles, Keys: []string{"E"}},
	{ID: "profile.delete", Title: "delete profile", Desc: "Remove the selected profile", Scope: ScopeProfiles, Keys: []string{"D"}},
	{ID: "profile.test", Title: "test connection", Desc: "Check every configured endpoint", Scope: ScopeProfiles, Keys: []string{"t"}},
	{ID: "profile.readonly", Title: "toggle read-only", Desc: "Flip the read-only guard", Scope: ScopeProfiles, Keys: []string{"R"}},
	{ID: "profile.yank_full", Title: "yank connection string", Desc: "Copy the connection string, key included", Scope: ScopeProfiles, Keys: []string{"Y"}},
}

// Actions returns every registered action, sorted by ID.
func Actions() []Action {
	out := make([]Action, len(registry))
	copy(out, registry)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ActionsFor returns the global actions plus those scoped to one screen,
// which is exactly what the palette should offer while that screen has
// focus.
func ActionsFor(scope string) []Action {
	var out []Action
	for _, a := range registry {
		if a.Scope == ScopeGlobal || a.Scope == scope {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		// Scoped actions first: on the blob screen, "blob delete" is a
		// better first hit for "de" than "go to dashboard".
		if (out[i].Scope == ScopeGlobal) != (out[j].Scope == ScopeGlobal) {
			return out[j].Scope == ScopeGlobal
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Lookup finds an action by ID.
func Lookup(id string) (Action, bool) {
	for _, a := range registry {
		if a.ID == id {
			return a, true
		}
	}
	return Action{}, false
}
