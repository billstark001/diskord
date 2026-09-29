// Package version holds the release version injected by the build scripts.
package version

// Current is "dev" for ad hoc go builds. Distribution builds derive it from
// frontend/package.json, the project's single release-version source.
var Current = "dev"
