ClassicStack-web (Finder UI) is consumed from here as a git submodule, pinned to
a commit on its `main` branch:

```
third_party/classicstack-web  →  https://github.com/ObsoleteMadness/ClassicStack-web.git
```

Vite and `tsc` alias `classicstack-web/*` straight into that tree's `src/*` (see
`adapter/control/http/ui/vite.config.ts` and `tsconfig.json`) — there is no npm
publish step, so both repos typecheck against the same TypeScript sources.

Clone with `--recurse-submodules`, or run `git submodule update --init` in an
existing checkout. See the "Web UI submodule" section of the top-level README
for the day-to-day commands.

## Source resolution

`make spa` (`scripts/ci/spa.sh`) resolves the Finder UI in this order:

| | Source |
|---|---|
| 1 | `$WEB_DIR`, if set — an explicit checkout, for working against a local tree |
| 2 | this submodule |
| 3 | `git submodule update --init`, when the clone skipped submodules |
| 4 | a sibling `../ClassicStack-web` checkout |
| 5 | a shallow clone of `$WEB_REF` (default `main`) into this directory |

CI takes path 2: every workflow job that builds the SPA checks out with
`submodules: recursive`, and `.github/actions/setup-spa` fails with a clear
message if the submodule is empty.
