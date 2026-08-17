ClassicStack-web (Finder UI) is consumed from here as a git submodule:

```
git submodule add https://github.com/ObsoleteMadness/ClassicStack-web.git third_party/classicstack-web
git -C third_party/classicstack-web checkout feature/shared-finder-host
```

Until that pin exists, `make spa` uses a sibling checkout at `../ClassicStack-web` (same parent as this repo) or clones `WEB_REF` (default `feature/shared-finder-host`).
