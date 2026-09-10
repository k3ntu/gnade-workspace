// Package gnadeworkspace is the shared persistence layer for the "workspace" concept used by
// both gnadedoc-graph and gnadeboard-kanban to group microservice/repository projects. Both apps
// read and write the same ~/.gnade/workspaces.json file; this package is the single source of
// truth for that file's shape and CRUD logic, so the two apps stop drifting independently.
//
// Each app keeps its own local Go type (domain.WorkspaceManifest in gnadedoc-graph,
// models.WorkspaceInfo in gnadeboard-kanban) for its own call sites and Wails-bound methods, and
// converts to/from the types in this package only at the boundary of its own thin store wrapper.
package gnadeworkspace

import (
	"encoding/json"
	"time"
)

// Manifest describes a workspace: a named group of projects/microservices, versioned over time.
// Its field set is the union of what gnadedoc-graph and gnadeboard-kanban each historically
// tracked on their own (e.g. CreatedAt came only from graph, UpdatedAgo only from kanban), so
// persisting through this type no longer silently drops whichever fields the other app doesn't
// know about.
type Manifest struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Code        string            `json:"code"`
	Version     string            `json:"version"`
	Description string            `json:"description,omitempty"`
	Path        string            `json:"path,omitempty"`
	UpdatedAgo  string            `json:"updatedAgo,omitempty"`
	Projects    []Project         `json:"projects"`
	Versions    []VersionSnapshot `json:"versions"`
	CreatedAt   time.Time         `json:"created_at,omitempty"`
	DeletedAt   *time.Time        `json:"deleted_at,omitempty"`
}

// Project describes a project/microservice within a workspace.
type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"` // "microservice" | "pipeline step"
	Path        string `json:"path,omitempty"`
	Description string `json:"description,omitempty"`
	DocsCount   int    `json:"docs_count,omitempty"`
	NodesCount  int    `json:"nodes_count,omitempty"`
	TasksCount  int    `json:"tasks_count,omitempty"`
	AgentStatus string `json:"agentStatus,omitempty"`
	HasDrafts   bool   `json:"hasDrafts,omitempty"`
	Initials    string `json:"initials,omitempty"`
	Desc        string `json:"desc,omitempty"`
}

// UnmarshalJSON tolerates both snake_case and camelCase for docs/nodes/tasks counts
// (docsCount/nodesCount/tasksCount), matching entries written by older tooling.
func (p *Project) UnmarshalJSON(data []byte) error {
	type Alias Project
	aux := struct {
		*Alias
		CamelDocsCount  *int `json:"docsCount"`
		CamelNodesCount *int `json:"nodesCount"`
		CamelTasksCount *int `json:"tasksCount"`
	}{
		Alias: (*Alias)(p),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.CamelDocsCount != nil && p.DocsCount == 0 {
		p.DocsCount = *aux.CamelDocsCount
	}
	if aux.CamelNodesCount != nil && p.NodesCount == 0 {
		p.NodesCount = *aux.CamelNodesCount
	}
	if aux.CamelTasksCount != nil && p.TasksCount == 0 {
		p.TasksCount = *aux.CamelTasksCount
	}
	return nil
}

// VersionSnapshot is an immutable version snapshot (Git-replica style) of a workspace's
// architecture/contracts at a point in time.
type VersionSnapshot struct {
	ID             string    `json:"id"`
	CommitHash     string    `json:"commit_hash"`
	Version        string    `json:"version"`
	Title          string    `json:"title"`
	Message        string    `json:"message,omitempty"`
	Author         string    `json:"author"`
	Source         string    `json:"source"` // "hybrid" | "kanban" | "docgraph" | "manual"
	TasksCount     int       `json:"tasks_count"`
	ImpactLevel    string    `json:"impact_level"` // "LOW" | "MEDIUM" | "HIGH" | "BREAKING"
	ArchNodesCount int       `json:"arch_nodes_count,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	IsCurrent      bool      `json:"is_current,omitempty"`
}
