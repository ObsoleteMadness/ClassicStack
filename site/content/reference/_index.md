---
title: "API Reference"
weight: 12
---

# API Reference

Generated from Go doc comments (via [gomarkdoc](https://github.com/princjef/gomarkdoc)) for the
packages meant for external reuse: the protocol codecs (`core/protocol/...`), the file/network
services (`core/service/...`), the client SDK (`client/...`), and the shared support packages
(`core/csnet`, `core/binaryprimitives`, `core/config`, `core/fs`, `core/encoding`, `core/link`,
`core/buf`). See [§5](../docs/manual/#5-extending-classicstack--the-client-sdk) and
[§6](../docs/manual/#6-extending-classicstack--the-server-sdk) of the manual for the guided
walkthroughs these pages are the reference companion to.

`adapter/*` and `cmd/*` are excluded — they are ClassicStack's own composition/CLI layer, not
reuse surface for an external importer.

This section is regenerated on every build; the pages under it are not stored in the repository.
A thin or missing doc comment on a package shows up here as a thin page — if you're extending
ClassicStack and a page is missing detail you need, that's a good sign the source package's doc
comment could use it too.
