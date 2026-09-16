package gnadeworkspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// lockStaleAfter is how old a .lock file must be before a new writer assumes its owner
// crashed/was killed without cleaning up and force-removes it, instead of deadlocking forever.
const lockStaleAfter = 10 * time.Second

// lockAcquireTimeout is how long a writer retries before giving up entirely.
const lockAcquireTimeout = 5 * time.Second

// acquireFileLock takes an OS-level advisory lock via the exclusive creation of a sidecar
// "<file>.lock" file. sync.RWMutex (used elsewhere in this Store) only serializes writers
// within a single OS process -- it does nothing across two separate processes (e.g.
// gnadedoc-graph and gnadeboard-kanban) that each hold their own *Store pointed at the same
// physical workspaces.json. Without this, a burst of near-simultaneous saves from both
// processes can race: both read the same "before" snapshot, both write back their own
// modified copy, and whichever write lands last silently discards the other's changes
// (verified in production: a workspace with 29 registered projects vanished entirely after
// ~30 rapid ingests, each asynchronously re-triggering a cross-process sync).
func acquireFileLock(filePath string) (release func(), err error) {
	lockPath := filePath + ".lock"
	deadline := time.Now().Add(lockAcquireTimeout)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			fmt.Fprintf(f, "%d", os.Getpid())
			f.Close()
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			// Filesystem doesn't support this lock scheme (unlikely) -- proceed unlocked
			// rather than blocking writes entirely.
			return func() {}, nil
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > lockStaleAfter {
			// Previous holder almost certainly crashed without releasing it -- a live
			// holder would have finished its read-modify-write cycle well within this
			// window. Reclaim rather than deadlock forever.
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for workspace store lock at %s", lockPath)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Store persists Manifests as a JSON array in a single file, shared across every app that opens
// the same path (typically ~/.gnade/workspaces.json).
type Store struct {
	filePath string
	mu       sync.RWMutex

	defaultSnapshotAuthor string
	defaultSnapshotSource string
}

// Option configures a Store at construction time.
type Option func(*Store)

// WithSnapshotDefaults sets the author/source stamped on auto-generated version snapshots
// (e.g. the initial snapshot created the first time a workspace is saved), so each app can keep
// its own identity without forking the store logic.
func WithSnapshotDefaults(author, source string) Option {
	return func(s *Store) {
		if author != "" {
			s.defaultSnapshotAuthor = author
		}
		if source != "" {
			s.defaultSnapshotSource = source
		}
	}
}

func isRunningTests() bool {
	base := filepath.Base(os.Args[0])
	return strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".test.exe") || strings.Contains(base, "__debug_bin") || strings.Contains(os.Args[0], "go-build")
}

// NewStore opens (creating if needed) the workspaces JSON file at customPath, or the default
// per-user location (~/.gnade/workspaces.json, or a temp file while running under `go test`).
func NewStore(customPath string, opts ...Option) (*Store, error) {
	filePath := customPath
	if filePath == "" {
		if isRunningTests() {
			filePath = filepath.Join(os.TempDir(), "gnade-test-workspaces.json")
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("failed to get user home dir: %w", err)
			}
			dir := filepath.Join(home, ".gnade")
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, fmt.Errorf("failed to create .gnade dir: %w", err)
			}
			filePath = filepath.Join(dir, "workspaces.json")
		}
	}

	s := &Store{
		filePath:              filePath,
		defaultSnapshotAuthor: "gnade-agent",
		defaultSnapshotSource: "manual",
	}
	for _, opt := range opts {
		opt(s)
	}
	if err := s.ensureFile(); err != nil {
		return nil, err
	}
	return s, nil
}

// FilePath returns the resolved path of the workspaces JSON file.
func (s *Store) FilePath() string {
	return s.filePath
}

func (s *Store) ensureFile() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := os.Stat(s.filePath); os.IsNotExist(err) {
		// If this is a dev sandbox (e.g. workspaces-dev.json), seed from the canonical
		// workspaces.json if it exists, so dev profiles start from real data.
		if strings.HasSuffix(s.filePath, "workspaces-dev.json") {
			prodPath := filepath.Join(filepath.Dir(s.filePath), "workspaces.json")
			if prodData, err := os.ReadFile(prodPath); err == nil && len(strings.TrimSpace(string(prodData))) > 0 {
				return os.WriteFile(s.filePath, prodData, 0644)
			}
		}

		empty := []Manifest{}
		data, _ := json.MarshalIndent(empty, "", "  ")
		return os.WriteFile(s.filePath, data, 0644)
	}
	return nil
}

func (s *Store) readAll() ([]Manifest, error) {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []Manifest{}, nil
		}
		return nil, fmt.Errorf("failed to read workspaces file: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return []Manifest{}, nil
	}
	var list []Manifest
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("failed to parse workspaces json: %w", err)
	}
	return list, nil
}

func (s *Store) writeAll(list []Manifest) error {
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal workspaces: %w", err)
	}
	tmpFile := s.filePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write tmp file: %w", err)
	}
	return os.Rename(tmpFile, s.filePath)
}

func ensureSlices(ws *Manifest) {
	if ws.Projects == nil {
		ws.Projects = make([]Project, 0)
	}
	if ws.Versions == nil {
		ws.Versions = make([]VersionSnapshot, 0)
	}
}

// ListActive returns every non-deleted workspace.
func (s *Store) ListActive() ([]Manifest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	all, err := s.readAll()
	if err != nil {
		return nil, err
	}
	active := make([]Manifest, 0)
	for _, ws := range all {
		if ws.DeletedAt == nil {
			ensureSlices(&ws)
			active = append(active, ws)
		}
	}
	return active, nil
}

// ListTrash returns every soft-deleted workspace.
func (s *Store) ListTrash() ([]Manifest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	all, err := s.readAll()
	if err != nil {
		return nil, err
	}
	trash := make([]Manifest, 0)
	for _, ws := range all {
		if ws.DeletedAt != nil {
			ensureSlices(&ws)
			trash = append(trash, ws)
		}
	}
	return trash, nil
}

// Get resolves a workspace by ID or exact name.
func (s *Store) Get(id string) (*Manifest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	all, err := s.readAll()
	if err != nil {
		return nil, err
	}
	for _, ws := range all {
		if ws.ID == id || ws.Name == id {
			ensureSlices(&ws)
			return &ws, nil
		}
	}
	return nil, fmt.Errorf("workspace not found: %s", id)
}

// Save creates or updates a workspace: assigns an ID/code/version if missing, preserves its
// DeletedAt/Versions history on update, and seeds an initial version snapshot for brand-new
// workspaces using this Store's configured snapshot defaults.
func (s *Store) Save(ws Manifest) (*Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := acquireFileLock(s.filePath)
	if err != nil {
		return nil, err
	}
	defer release()

	all, err := s.readAll()
	if err != nil {
		return nil, err
	}

	// Capture which fields the caller actually left empty BEFORE the auto-generation below
	// fills in defaults (Code/Version) -- otherwise, by the time we reach the existing-record
	// merge, we can no longer tell "caller sent an empty Version" apart from "caller never
	// mentioned Version at all", and a partial update (e.g. only renaming a workspace) would
	// silently reset Version/Code/Description/Path/Projects/AgentInstructions to zero values.
	incomingNameEmpty := ws.Name == ""
	incomingCodeEmpty := ws.Code == ""
	incomingVersionEmpty := ws.Version == ""
	incomingDescriptionEmpty := ws.Description == ""
	incomingPathEmpty := ws.Path == ""
	incomingProjectsEmpty := len(ws.Projects) == 0
	// AgentInstructions is deliberately NOT protected here: an explicit read-modify-write clear
	// (get the full manifest, blank just this field, save it back) is a supported, tested flow
	// (see TestWorkspaceAgentInstructionsPersistAndClear) and must still be able to reach "".

	if ws.ID == "" {
		ws.ID = strings.ToLower(strings.ReplaceAll(ws.Name, " ", "-"))
		if ws.ID == "" {
			ws.ID = fmt.Sprintf("ws-%d", time.Now().UnixMilli())
		}
	}
	if ws.Code == "" {
		words := strings.Fields(ws.Name)
		var genCode string
		for _, w := range words {
			if len(w) > 0 {
				genCode += string(w[0])
			}
		}
		if len(genCode) == 0 && len(ws.Name) >= 2 {
			genCode = ws.Name[:2]
		}
		ws.Code = strings.ToUpper(genCode)
		if len(ws.Code) > 4 {
			ws.Code = ws.Code[:4]
		}
	}
	if ws.Version == "" {
		ws.Version = "v0.1.0"
	}
	ensureSlices(&ws)

	found := false
	for i, existing := range all {
		if existing.ID == ws.ID || existing.Name == ws.Name {
			if ws.DeletedAt == nil && existing.DeletedAt != nil {
				existing.DeletedAt = nil
			}
			ws.DeletedAt = existing.DeletedAt
			// Preserve existing versions if the incoming manifest doesn't carry any.
			if len(ws.Versions) == 0 && len(existing.Versions) > 0 {
				ws.Versions = existing.Versions
			}
			// A caller doing a partial update (e.g. POST/PUT only to set one field, like
			// agent_instructions or a rename) must not blow away everything else it didn't
			// mention -- same precedent as Versions above, extended to every other field.
			// This means this Save path cannot explicitly blank out a previously-set field
			// back to empty; only replacing it with a new non-empty value is supported.
			if incomingNameEmpty && existing.Name != "" {
				ws.Name = existing.Name
			}
			if incomingCodeEmpty && existing.Code != "" {
				ws.Code = existing.Code
			}
			if incomingVersionEmpty && existing.Version != "" {
				ws.Version = existing.Version
			}
			if incomingDescriptionEmpty && existing.Description != "" {
				ws.Description = existing.Description
			}
			if incomingPathEmpty && existing.Path != "" {
				ws.Path = existing.Path
			}
			if incomingProjectsEmpty && len(existing.Projects) > 0 {
				ws.Projects = existing.Projects
			}
			all[i] = ws
			found = true
			break
		}
	}
	if !found {
		if len(ws.Versions) == 0 {
			ws.Versions = []VersionSnapshot{s.newInitialSnapshot(ws)}
		}
		all = append(all, ws)
	}

	if err := s.writeAll(all); err != nil {
		return nil, err
	}
	return &ws, nil
}

func (s *Store) newInitialSnapshot(ws Manifest) VersionSnapshot {
	commitHash := "a1b2c3d"
	if len(ws.ID) >= 7 {
		commitHash = strings.ReplaceAll(ws.ID, "-", "")
		if len(commitHash) > 7 {
			commitHash = commitHash[:7]
		}
	}
	return VersionSnapshot{
		ID:             fmt.Sprintf("snp-%s", strings.ReplaceAll(uuid.New().String(), "-", "")[:8]),
		CommitHash:     commitHash,
		Version:        ws.Version,
		Title:          fmt.Sprintf("Release %s: %s", ws.Version, ws.Name),
		Message:        "Initial snapshot of architecture and contracts.",
		Author:         s.defaultSnapshotAuthor,
		Source:         s.defaultSnapshotSource,
		TasksCount:     0,
		ImpactLevel:    "LOW",
		ArchNodesCount: len(ws.Projects),
		CreatedAt:      time.Now().UTC(),
		IsCurrent:      true,
	}
}

// AddSnapshot appends a new version snapshot, marks it current, updates the workspace's version
// string, and persists the change.
func (s *Store) AddSnapshot(workspaceID string, snap VersionSnapshot) (*Manifest, *VersionSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := acquireFileLock(s.filePath)
	if err != nil {
		return nil, nil, err
	}
	defer release()

	all, err := s.readAll()
	if err != nil {
		return nil, nil, err
	}

	for i, ws := range all {
		if ws.ID == workspaceID || ws.Name == workspaceID {
			if snap.ID == "" {
				snap.ID = fmt.Sprintf("snp-%s", strings.ReplaceAll(uuid.New().String(), "-", "")[:8])
			}
			if snap.CommitHash == "" {
				snap.CommitHash = strings.ReplaceAll(uuid.New().String(), "-", "")[:7]
			}
			if snap.Author == "" {
				snap.Author = s.defaultSnapshotAuthor
			}
			if snap.Source == "" {
				snap.Source = s.defaultSnapshotSource
			}
			if snap.ImpactLevel == "" {
				snap.ImpactLevel = "LOW"
			}
			if snap.CreatedAt.IsZero() {
				snap.CreatedAt = time.Now().UTC()
			}
			snap.IsCurrent = true

			for j := range ws.Versions {
				ws.Versions[j].IsCurrent = false
			}
			ws.Versions = append(ws.Versions, snap)
			if snap.Version != "" {
				ws.Version = snap.Version
			}
			all[i] = ws
			if err := s.writeAll(all); err != nil {
				return nil, nil, err
			}
			return &all[i], &snap, nil
		}
	}
	return nil, nil, fmt.Errorf("workspace not found: %s", workspaceID)
}

// MoveToTrash soft-deletes a workspace by ID or name.
func (s *Store) MoveToTrash(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := acquireFileLock(s.filePath)
	if err != nil {
		return err
	}
	defer release()

	all, err := s.readAll()
	if err != nil {
		return err
	}
	found := false
	now := time.Now().UTC()
	for i, ws := range all {
		if ws.ID == id || ws.Name == id {
			all[i].DeletedAt = &now
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("workspace not found: %s", id)
	}
	return s.writeAll(all)
}

// Restore clears a workspace's soft-deleted state.
func (s *Store) Restore(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := acquireFileLock(s.filePath)
	if err != nil {
		return err
	}
	defer release()

	all, err := s.readAll()
	if err != nil {
		return err
	}
	found := false
	for i, ws := range all {
		if ws.ID == id || ws.Name == id {
			all[i].DeletedAt = nil
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("workspace not found: %s", id)
	}
	return s.writeAll(all)
}

// Purge permanently removes a single workspace.
func (s *Store) Purge(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := acquireFileLock(s.filePath)
	if err != nil {
		return err
	}
	defer release()

	all, err := s.readAll()
	if err != nil {
		return err
	}
	filtered := make([]Manifest, 0, len(all))
	for _, ws := range all {
		if ws.ID != id && ws.Name != id {
			filtered = append(filtered, ws)
		}
	}
	return s.writeAll(filtered)
}

// PurgeAllTrash permanently removes every soft-deleted workspace.
func (s *Store) PurgeAllTrash() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := acquireFileLock(s.filePath)
	if err != nil {
		return err
	}
	defer release()

	all, err := s.readAll()
	if err != nil {
		return err
	}
	active := make([]Manifest, 0, len(all))
	for _, ws := range all {
		if ws.DeletedAt == nil {
			active = append(active, ws)
		}
	}
	return s.writeAll(active)
}
