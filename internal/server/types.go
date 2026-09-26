// Shared JSON shapes and store aliases for handlers.
package server

import "agent-runtime/internal/store"

type storeProject struct {
	ID string
}

type storeWorkspace struct {
	ID   string
	Path string
}

// storeRevision aliases the config revision row.
type storeRevision = store.ConfigRevision

func workspaceJSON(w *store.Workspace) map[string]any {
	return map[string]any{
		"id": w.ID, "projectId": w.ProjectID, "path": w.Path,
		"confirmed": w.Confirmed, "lastSeenAt": w.LastSeenAt,
		"gitCommonDir": w.GitCommonDir,
	}
}
