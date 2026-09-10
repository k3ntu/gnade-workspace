package gnadeworkspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_CRUDAndTrash(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ws_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	wsFile := filepath.Join(tmpDir, "workspaces.json")
	store, err := NewStore(wsFile)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// 1. Initial list should be empty
	active, err := store.ListActive()
	if err != nil {
		t.Fatalf("ListActive failed: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("expected 0 active workspaces, got %d", len(active))
	}

	// 2. Save new workspace
	ws1 := Manifest{
		ID:   "gnade-suite",
		Name: "Gnade Suite",
		Code: "GS",
	}
	saved, err := store.Save(ws1)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if saved.Code != "GS" {
		t.Errorf("expected code GS, got %s", saved.Code)
	}

	// 3. List active should have 1
	active, err = store.ListActive()
	if err != nil || len(active) != 1 {
		t.Fatalf("expected 1 active, got %d (err: %v)", len(active), err)
	}
	if active[0].Name != "Gnade Suite" {
		t.Errorf("expected Gnade Suite, got %s", active[0].Name)
	}

	// 4. Move to trash
	if err := store.MoveToTrash("gnade-suite"); err != nil {
		t.Fatalf("MoveToTrash failed: %v", err)
	}

	active, _ = store.ListActive()
	if len(active) != 0 {
		t.Errorf("expected 0 active after trash, got %d", len(active))
	}

	trash, err := store.ListTrash()
	if err != nil || len(trash) != 1 {
		t.Fatalf("expected 1 in trash, got %d (err: %v)", len(trash), err)
	}
	if trash[0].ID != "gnade-suite" {
		t.Errorf("expected gnade-suite in trash, got %s", trash[0].ID)
	}

	// 5. Restore from trash
	if err := store.Restore("gnade-suite"); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}
	active, _ = store.ListActive()
	if len(active) != 1 {
		t.Errorf("expected 1 active after restore, got %d", len(active))
	}

	// 6. Purge
	if err := store.Purge("gnade-suite"); err != nil {
		t.Fatalf("Purge failed: %v", err)
	}
	active, _ = store.ListActive()
	trash, _ = store.ListTrash()
	if len(active) != 0 || len(trash) != 0 {
		t.Errorf("expected 0 active and 0 trash after purge, got active=%d, trash=%d", len(active), len(trash))
	}
}

func TestStore_DevSandboxSeeding(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ws_sandbox_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	prodFile := filepath.Join(tmpDir, "workspaces.json")
	devFile := filepath.Join(tmpDir, "workspaces-dev.json")

	// 1. Create and seed production store
	prodStore, err := NewStore(prodFile)
	if err != nil {
		t.Fatalf("failed to create prod store: %v", err)
	}
	_, err = prodStore.Save(Manifest{
		ID:   "prod-suite",
		Name: "Production Suite",
		Code: "PS",
	})
	if err != nil {
		t.Fatalf("failed to save prod workspace: %v", err)
	}

	// Verify devFile does not exist yet
	if _, err := os.Stat(devFile); !os.IsNotExist(err) {
		t.Fatalf("expected devFile to not exist initially")
	}

	// 2. Initialize dev store - should automatically seed from workspaces.json
	devStore, err := NewStore(devFile)
	if err != nil {
		t.Fatalf("failed to create dev store: %v", err)
	}

	devActive, err := devStore.ListActive()
	if err != nil {
		t.Fatalf("dev ListActive failed: %v", err)
	}
	if len(devActive) != 1 || devActive[0].ID != "prod-suite" {
		t.Fatalf("expected dev store to seed from prod, got %+v", devActive)
	}

	// 3. Mutate dev store (add a dev-only workspace and delete prod-suite from dev)
	_, err = devStore.Save(Manifest{
		ID:   "dev-experiment",
		Name: "Dev Experiment",
		Code: "DE",
	})
	if err != nil {
		t.Fatalf("failed to save in dev store: %v", err)
	}
	if err := devStore.Purge("prod-suite"); err != nil {
		t.Fatalf("failed to purge from dev store: %v", err)
	}

	// Verify dev store only has dev-experiment
	devActive, _ = devStore.ListActive()
	if len(devActive) != 1 || devActive[0].ID != "dev-experiment" {
		t.Fatalf("expected dev store to have only dev-experiment, got %+v", devActive)
	}

	// 4. Verify prodStore is completely UNTOUCHED (sandbox isolation)
	prodActive, _ := prodStore.ListActive()
	if len(prodActive) != 1 || prodActive[0].ID != "prod-suite" {
		t.Fatalf("production store was corrupted! Expected 1 prod-suite, got %+v", prodActive)
	}
}

// TestStore_PreservesFieldsFromBothApps guards the original bug this unification fixes: before
// sharing one Store/Manifest, gnadedoc-graph's narrower struct (no UI fields, no CreatedAt in
// kanban's case) could silently drop fields the other app had written when it re-saved a
// workspace. A Manifest carrying "fields from both worlds" must round-trip through Save/Get
// intact.
func TestStore_PreservesFieldsFromBothApps(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ws_union_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(filepath.Join(tmpDir, "workspaces.json"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	createdAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	ws := Manifest{
		ID:   "auth-suite",
		Name: "Auth Suite",
		Code: "AUTH",
		// graph-only field
		CreatedAt: createdAt,
		Projects: []Project{
			{
				ID:   "ms-security-customer",
				Name: "ms-security-customer",
				Type: "microservice",
				// kanban-only UI fields
				AgentStatus: "reviewing",
				HasDrafts:   true,
				Initials:    "MS",
				Desc:        "Security/customer boundary service",
			},
		},
	}

	if _, err := store.Save(ws); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, err := store.Get("auth-suite")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !got.CreatedAt.Equal(createdAt) {
		t.Errorf("expected CreatedAt to survive the round-trip, got %v", got.CreatedAt)
	}
	if len(got.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(got.Projects))
	}
	p := got.Projects[0]
	if p.AgentStatus != "reviewing" || !p.HasDrafts || p.Initials != "MS" || p.Desc != "Security/customer boundary service" {
		t.Errorf("expected UI-only project fields to survive the round-trip, got %+v", p)
	}

	// Re-saving (simulating the OTHER app writing back) must not drop these fields either.
	if _, err := store.Save(*got); err != nil {
		t.Fatalf("re-Save failed: %v", err)
	}
	again, err := store.Get("auth-suite")
	if err != nil {
		t.Fatalf("Get after re-save failed: %v", err)
	}
	if !again.CreatedAt.Equal(createdAt) || again.Projects[0].AgentStatus != "reviewing" {
		t.Errorf("expected fields to survive a re-save, got %+v", again)
	}
}
