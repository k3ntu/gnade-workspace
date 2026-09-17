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
	AgentInstructions string            `json:"agent_instructions,omitempty"`
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Code              string            `json:"code"`
	Version           string            `json:"version"`
	Description       string            `json:"description,omitempty"`
	Path              string            `json:"path,omitempty"`
	UpdatedAgo        string            `json:"updatedAgo,omitempty"`
	Projects          []Project         `json:"projects"`
	Versions          []VersionSnapshot `json:"versions"`
	CreatedAt         time.Time         `json:"created_at,omitempty"`
	DeletedAt         *time.Time        `json:"deleted_at,omitempty"`
	// TurboReviewMode is a gnadedoc-graph-only setting (kanban never reads or writes it): when
	// true, a draft forked by ConflictArbiter for this workspace is merged into its authoritative
	// original immediately instead of waiting in the human review queue. It is still persisted
	// (not deleted) with a resolution marker, so the auto-approval stays auditable.
	TurboReviewMode bool `json:"turbo_review_mode,omitempty"`
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
	// DocFolders y DocExcludeFolders son la política de documentación que declara el dueño de
	// un repositorio (que gnadedoc-graph usa para decidir qué .md de cada proyecto entran al
	// grafo): rutas relativas que SUMAN carpetas/archivos como documentación y que los SACAN.
	//
	// Son campos que solo usa gnadedoc-graph (como TurboReviewMode en Manifest), pero viven en
	// el tipo compartido por el mismo motivo que el resto: si la otra app re-guarda el
	// workspace sin conocerlos, los borraría en silencio -- que es exactamente el problema que
	// este paquete existe para evitar.
	DocFolders        []string `json:"doc_folders,omitempty"`
	DocExcludeFolders []string `json:"doc_exclude_folders,omitempty"`
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
	// ExportPath es la ruta (relativa al Path del workspace) del archivo JSON con el contenido
	// real exportado (nodos/aristas/documentos) para este snapshot, versionado con un commit
	// git real en CommitHash. Vacío en snapshots emitidos antes de que existiera el export real.
	ExportPath string `json:"export_path,omitempty"`
}
