# Third-party components

The build resolves Go modules from `go.mod` / `go.sum` and frontend packages from `frontend/package.json` / `frontend/pnpm-lock.yaml`. Those manifests and each upstream package contain the exact versions and license terms used by a build.

The console uses Preact, TanStack Query, vanilla-extract, `markdown-it`, and Emojibase shortcode data. The Markdown renderer is bundled into the embedded frontend; it does not fetch a script or stylesheet at runtime. Cached Discord images are observed or retrieved from allowlisted Discord CDN URLs when the user enables resource capture; they are not bundled with this source tree.

`third_party/htmx-LICENSE` is retained for the historical htmx asset, which is no longer used by the current Preact console. It can be removed when the project no longer needs to document that historical distribution.

Archive Markdown, JSONL, and ZIP generation uses Go's standard library and the existing SQLite driver. Markdown exports preserve bodies as fenced text and do not invoke the frontend renderer. Optional attachment bundles copy cached images without fetching additional third-party assets.
