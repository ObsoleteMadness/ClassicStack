# docs.classicstack site

The published documentation site, built with [Hugo](https://gohugo.io/) (a Go static
site generator — a single binary, no Python/Node toolchain needed) and the
[hugo-book](https://github.com/alex-shpak/hugo-book) theme.

**There is no content here to write.** `hugo.toml`'s `[[module.mounts]]` mount the real
`../docs`, `../spec`, and `../ARCHITECTURE.md` straight into the site's content tree, so
the page you're reading on the web is generated from the same Markdown you'd read on
GitHub — editing a file under `docs/` is the whole edit, there's nothing to duplicate or
keep in sync. `content/_index.md` in this directory is the one page that's genuinely
site-only: the homepage.

## Preview locally

Requires [Hugo](https://gohugo.io/installation/) (extended edition) and Go (Hugo Modules
resolves the theme dependency via `go.mod`/`go.sum` in this directory, same mechanism as
any other Go module — no separate package manager).

~~~bash
cd site
hugo server
# → http://localhost:1313/
~~~

## Build

~~~bash
cd site
hugo --minify -d ../public
~~~

## Deploy

`.github/workflows/docs.yml` builds and publishes this site to GitHub Pages on every
push to `main` that touches `docs/`, `spec/`, `ARCHITECTURE.md`, or `site/`.

## Nav ordering

Pages under `docs/` are ordered by the `weight` in their Hugo front matter (a
`---\ntitle: "..."\nweight: N\n---` block at the top of the file) rather than
alphabetically — see any file under `../docs` for the pattern. `spec/` relies on its
existing `00-`, `01-`, … filename prefixes instead, so no front matter was added there.
