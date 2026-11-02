package openspec

// GetBuiltinSchemas returns the built-in schema definitions.
func GetBuiltinSchemas() map[string]*SchemaDefinition {
	return map[string]*SchemaDefinition{
		"spec-driven": specDrivenSchema(),
	}
}

// GetSchema returns the schema definition for the given name, or nil if not found.
func GetSchema(name string) *SchemaDefinition {
	schemas := GetBuiltinSchemas()
	if s, ok := schemas[name]; ok {
		return s
	}
	return nil
}

// specDrivenSchema returns the default "spec-driven" schema with
// proposal → specs → design → tasks dependency graph.
func specDrivenSchema() *SchemaDefinition {
	return &SchemaDefinition{
		Name: "spec-driven",
		Artifacts: []SchemaArtifact{
			{
				ID:        "proposal",
				Generates: "proposal.md",
				Requires:  []string{},
			},
			{
				ID:        "specs",
				Generates: "specs/**/*.md",
				Requires:  []string{"proposal"},
			},
			{
				ID:        "design",
				Generates: "design.md",
				Requires:  []string{"proposal"},
			},
			{
				ID:        "tasks",
				Generates: "tasks.md",
				Requires:  []string{"specs", "design"},
			},
		},
	}
}

// GetReadyArtifacts returns the list of artifact IDs that have all
// dependencies satisfied (i.e., all required artifacts are "created").
func GetReadyArtifacts(schema *SchemaDefinition, statuses map[string]ArtifactStatus) []string {
	var ready []string
	for _, sa := range schema.Artifacts {
		// Skip already created artifacts
		if statuses[sa.ID] == ArtifactStatusCreated {
			continue
		}
		allMet := true
		for _, dep := range sa.Requires {
			if statuses[dep] != ArtifactStatusCreated {
				allMet = false
				break
			}
		}
		if allMet {
			ready = append(ready, sa.ID)
		}
	}
	return ready
}

// GetArtifactOrder returns the artifact IDs in topological order
// (respecting dependency constraints).
func GetArtifactOrder(schema *SchemaDefinition) []string {
	// Since our schemas are small and well-formed, a simple approach works.
	visited := make(map[string]bool)
	var order []string

	idToArtifact := make(map[string]SchemaArtifact)
	for _, a := range schema.Artifacts {
		idToArtifact[a.ID] = a
	}

	var visit func(id string)
	visit = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true
		for _, dep := range idToArtifact[id].Requires {
			visit(dep)
		}
		order = append(order, id)
	}

	for _, a := range schema.Artifacts {
		visit(a.ID)
	}
	return order
}
