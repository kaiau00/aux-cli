// Package relatedproject builds the related-project graph:
// typed edges between distinct projects derived from dependencies, schemas,
// deployment config, shared remotes, or user declaration. Every edge preserves
// both project identities, so cross-project retrieval never merges similarly
// named symbols from different repositories without identity.
package relatedproject

// RelationType classifies a directed relationship between two projects.
type RelationType string

const (
	ServiceClient   RelationType = "service_client"
	LibraryConsumer RelationType = "library_consumer"
	SchemaGenerator RelationType = "schema_generator"
	AppInfra        RelationType = "app_infra"
	CodeDocs        RelationType = "code_docs"
)

// Source records how an edge was derived.
type Source string

const (
	SourceDeps     Source = "deps"
	SourceSchema   Source = "schema"
	SourceDeploy   Source = "deploy"
	SourceRemote   Source = "remote"
	SourceDeclared Source = "declared"
)

// Relation is one directed edge between two projects.
type Relation struct {
	ID           string
	FromProject  string
	ToProject    string
	RelationType RelationType
	Source       Source
	CreatedAt    int64
}

// DeriveFromModules turns a project's module dependencies into library/consumer
// relations for every dependency that resolves to another known project. Pure
// and deterministic: a dependency on an unknown module, on the project's own
// module, or a self-edge is skipped, so identity is never fabricated.
func DeriveFromModules(fromProjectID, ownModulePath string, deps []string, knownByModule map[string]string) []Relation {
	seen := map[string]bool{}
	var out []Relation
	for _, dep := range deps {
		if dep == "" || dep == ownModulePath {
			continue
		}
		toProject, ok := knownByModule[dep]
		if !ok || toProject == "" || toProject == fromProjectID {
			continue
		}
		if seen[toProject] {
			continue
		}
		seen[toProject] = true
		out = append(out, Relation{
			FromProject:  fromProjectID,
			ToProject:    toProject,
			RelationType: LibraryConsumer,
			Source:       SourceDeps,
		})
	}
	return out
}
