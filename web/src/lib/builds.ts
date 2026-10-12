import type { Build } from './api'

// getBuilds is scoped server-side to the CL's latest patchset, but not to the
// latest build per tool within it — retriggering (a retry, or the bulk
// "Trigger builds" action) inserts a fresh row per tool rather than replacing
// the old one, so the response can carry more than one row for the same
// tool_name. Keep only the highest id (== most recently inserted) per tool.
export function latestBuildPerTool(builds: Build[]): Build[] {
  const byTool = new Map<string, Build>()
  for (const b of builds) {
    const existing = byTool.get(b.tool_name)
    if (!existing || b.id > existing.id) byTool.set(b.tool_name, b)
  }
  return [...byTool.values()]
}
