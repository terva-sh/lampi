package terva

import "terva.sh/lampi/internal/adapter"

// projectGit reads origin's URL and HEAD from the repository that contains
// cwd. A missing directory, a missing .git, or an unreadable config is
// an empty result: the allowlist can still match the cwd itself.
func projectGit(cwd string) (remote, commit string) {
	return adapter.ProjectGit(cwd)
}
