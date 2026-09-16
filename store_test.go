package gnadeworkspace

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

// TestStore_PartialSaveDoesNotWipeExistingFields reproduces a real incident: a caller does
// Save(Manifest{ID: existingID, Name: existingName, AgentInstructions: "..."}) meaning only to
// set one field (e.g. via a REST client sending a minimal JSON body) -- Description, Path,
// Projects and Version must survive untouched, not silently reset to their zero values.
func TestStore_PartialSaveDoesNotWipeExistingFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspaces.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	full := Manifest{
		ID:          "gnade-suite-live",
		Name:        "Gnade Suite",
		Code:        "GS",
		Version:     "v1.0.0",
		Description: "Workspace cargado desde D:\\dev\\ai\\gnade-suite",
		Path:        "D:\\dev\\ai\\gnade-suite",
		Projects: []Project{
			{ID: "gnadeboard-kanban", Name: "gnadeboard-kanban", Path: "D:\\Projects\\Gnade\\gnadeboard-kanban"},
			{ID: "gnadedoc-graph", Name: "gnadedoc-graph", Path: "D:\\Projects\\Gnade\\gnadedoc-graph"},
		},
	}
	if _, err := store.Save(full); err != nil {
		t.Fatalf("failed to save full manifest: %v", err)
	}

	partial := Manifest{ID: "gnade-suite-live", Name: "Gnade Suite", AgentInstructions: "prueba"}
	saved, err := store.Save(partial)
	if err != nil {
		t.Fatalf("failed to save partial manifest: %v", err)
	}

	if saved.AgentInstructions != "prueba" {
		t.Errorf("expected AgentInstructions to be set, got %q", saved.AgentInstructions)
	}
	if saved.Description != full.Description {
		t.Errorf("Description was wiped: got %q, want %q", saved.Description, full.Description)
	}
	if saved.Path != full.Path {
		t.Errorf("Path was wiped: got %q, want %q", saved.Path, full.Path)
	}
	if saved.Version != full.Version {
		t.Errorf("Version was wiped: got %q, want %q", saved.Version, full.Version)
	}
	if len(saved.Projects) != len(full.Projects) {
		t.Fatalf("Projects were wiped: got %+v, want %+v", saved.Projects, full.Projects)
	}

	// A follow-up save WITH a real, non-empty value must still be able to actually change a
	// field -- the fix only protects against accidental blanking, not legitimate updates.
	renamed := Manifest{ID: "gnade-suite-live", Name: "Gnade Suite", Description: "Nueva descripcion real"}
	saved, err = store.Save(renamed)
	if err != nil {
		t.Fatalf("failed to save renamed manifest: %v", err)
	}
	if saved.Description != "Nueva descripcion real" {
		t.Errorf("expected Description to be updated to the new value, got %q", saved.Description)
	}
	if len(saved.Projects) != len(full.Projects) {
		t.Errorf("Projects should still be preserved on this save too, got %+v", saved.Projects)
	}
}

// TestStore_DevSandboxCreatedEmptyWhenNoProdToSeed covers the fresh-machine/fresh-profile case:
// dev runs (or the user deletes workspaces-dev.json on purpose to see how the app reacts to an
// empty profile) with NO workspaces.json to seed from either. Must create an empty, valid dev
// store instead of erroring or leaving the file missing -- symmetric with what a brand-new
// release install already does for workspaces.json.
func TestStore_DevSandboxCreatedEmptyWhenNoProdToSeed(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ws_sandbox_fresh_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	devFile := filepath.Join(tmpDir, "workspaces-dev.json")
	prodFile := filepath.Join(tmpDir, "workspaces.json")

	if _, err := os.Stat(prodFile); !os.IsNotExist(err) {
		t.Fatalf("expected prodFile to not exist")
	}

	devStore, err := NewStore(devFile)
	if err != nil {
		t.Fatalf("failed to create dev store with nothing to seed from: %v", err)
	}
	active, err := devStore.ListActive()
	if err != nil {
		t.Fatalf("ListActive on freshly-created dev store failed: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("expected empty dev store, got %+v", active)
	}
	if _, err := os.Stat(devFile); err != nil {
		t.Fatalf("expected devFile to have been created on disk: %v", err)
	}

	// Confirm it's a fully usable store afterward, not just an empty file.
	if _, err := devStore.Save(Manifest{ID: "dev-only", Name: "Dev Only"}); err != nil {
		t.Fatalf("failed to save into freshly-created dev store: %v", err)
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

// TestStore_CrossProcessConcurrentSavesDoNotLoseWorkspaces reproduces a real incident: with
// two separate *Store instances (simulating gnadedoc-graph and gnadeboard-kanban, two
// different OS processes that each open the same workspaces.json independently) saving
// concurrently, a workspace with 29 registered projects vanished entirely after a burst of
// ~30 near-simultaneous cross-process saves. sync.RWMutex only serializes writers within a
// single process; acquireFileLock must make the whole read-modify-write cycle atomic across
// processes too.
func TestStore_CrossProcessConcurrentSavesDoNotLoseWorkspaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspaces.json")

	storeA, err := NewStore(path)
	if err != nil {
		t.Fatalf("failed to open store A: %v", err)
	}
	storeB, err := NewStore(path)
	if err != nil {
		t.Fatalf("failed to open store B: %v", err)
	}

	const n = 30
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store := storeA
			if i%2 == 1 {
				store = storeB
			}
			id := fmt.Sprintf("ws-%02d", i)
			if _, err := store.Save(Manifest{ID: id, Name: id}); err != nil {
				t.Errorf("Save(%s) failed: %v", id, err)
			}
		}(i)
	}
	wg.Wait()

	final, err := storeA.ListActive()
	if err != nil {
		t.Fatalf("ListActive failed: %v", err)
	}
	if len(final) != n {
		seen := make(map[string]bool)
		for _, ws := range final {
			seen[ws.ID] = true
		}
		var missing []string
		for i := 0; i < n; i++ {
			id := fmt.Sprintf("ws-%02d", i)
			if !seen[id] {
				missing = append(missing, id)
			}
		}
		t.Fatalf("expected %d workspaces, got %d -- lost under concurrent cross-process writes: %v", n, len(final), missing)
	}
}
