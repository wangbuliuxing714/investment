package prompts

import _ "embed"

const OptimizationInvestPath = "优化/INVEST.MD"

// OptimizationDefault only seeds a new workspace document. Runtime requests
// read the saved document, never append or fall back to this template.
//
//go:embed defaults/optimization.md
var OptimizationDefault string

//go:embed defaults/workspace.md
var WorkspaceDefault string
