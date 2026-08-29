---
title: "Filename Encoding"
weight: 11
---

# Filename encoding

How filenames are transcoded between the host filesystem (arbitrary-length UTF-8) and each
protocol's own wire charset and length limits — MacRoman for AFP, OEM code pages or UTF-16LE for
SMB1, DOS 8.3 for NCP and EtherDFS — including how reserved characters, case-insensitivity, and
8.3/31-character name limits are handled. See
[`spec/16-storage-seam.md`](../spec/16-storage-seam.md) §2/§3 for the design document this page
summarizes, and [forks.md](forks.md) for the related (but separate) concern of resource
forks/Finder metadata.

---

## 1. The filename codec seam

Every share has exactly one `FilenameCodec`, configured with `filename_codec` in `server.toml`
(see [config.md](config.md)) — but a codec doesn't imply a single wire charset. Instead, each
protocol tells the codec which charset a given request actually used, **per request**, so one
share can serve a classic Mac client and a modern one in the same session:

| Wire encoding | Where it comes from |
|---|---|
| MacRoman | AFP short-name / long-name path types |
| UTF-8 | AFP UTF-8 path type |
| ANSI (single-byte OEM code page) | SMB1 without the Unicode session flag; NCP DOS/OS2 name spaces |
| UTF-16LE | SMB1 with `SMB_FLAGS2_UNICODE` set |

A request in a wire charset the codec doesn't support fails cleanly as an illegal name — it never
produces a mangled or truncated path.

### Registered codecs (`filename_codec` values)

| Value | Store charset | Wire charsets accepted |
|---|---|---|
| `identity` (default) | raw UTF-8 bytes as received, POSIX-style reserved-character set | MacRoman, UTF-8, ANSI, UTF-16 |
| `windows-safe` | same as `identity`, but reserves the Windows-illegal character set instead of POSIX's | MacRoman, UTF-8, ANSI, UTF-16 |
| `macroman-utf8` | UTF-8 text | MacRoman, UTF-8 only |
| `macroman-native` | raw MacRoman bytes, stored as-is | MacRoman only |

SMB shares default to `windows-safe` rather than the general `identity` default, since a share
served over SMB is far more likely to be read by a Windows tool that can't cope with `<>:"/\|`
in a name. `macroman-native` can't be combined with the `xattr` fork backend (see
[forks.md](forks.md)) since that backend serves Unicode names, not raw MacRoman store bytes.

### Reserved-character escaping

A character the store charset can't safely hold — `/` always, plus a protocol-specific set like
`<>:"\|?*` under `windows-safe` — is escaped on write as an uppercase ASCII token of its own code
point, e.g. `0x3A` for a colon, and reversed on read. Control characters below `0x20` are always
reserved regardless of which set is active.

One exception matters in practice: `windows-safe` deliberately does **not** reserve `?` or `*`,
even though both are illegal in a real Windows filename. Those two characters are `FIND_FIRST2`/
`SMB_COM_SEARCH` **wildcard metacharacters** on the wire — escaping them at write time would turn
a client's literal `*` search pattern into an inert escaped token before the wildcard matcher ever
sees it, breaking every directory listing that uses a wildcard.

A second nuance applies when *reversing* an escape token back to text: if the destination is a
DOS/Windows wire charset (SMB or NCP) and the escaped character is one Windows structurally can't
represent (a control character, or `<>:"/\|?*`), the literal `"0xNN"` text is left in place rather
than handed to the client as a raw illegal byte — a Mac file named with a literal carriage return
in it (the classic custom-icon-folder marker) would otherwise crash older Windows file managers.
AFP/Mac-facing wire charsets always get the character back unescaped, since Mac clients are the
reason those bytes exist in the name in the first place.

### The three transcoders

- **MacRoman ↔ UTF-8** — a full static 256-entry table (the ASCII half is identity; the upper 128
  bytes map to the real MacRoman repertoire). AppleTalk's own case-fold rules (used for
  case-insensitive zone/NBP-name comparison) are folded in alongside it, since plain ASCII
  case-folding isn't sufficient for MacRoman's accented characters.
- **UTF-16LE ↔ UTF-8** — strips one optional leading byte-order mark, resolves surrogate pairs,
  and rejects odd-length input outright rather than silently dropping a trailing byte.
- **OEM code page ↔ UTF-8** — only CP437 is implemented, matching what the DOS-era clients this
  project targets (Windows for Workgroups 3.11, DOS LAN Manager) actually negotiate. A client can
  send filenames in whatever OEM page it negotiated rather than the host's own locale, which is
  exactly why the wire charset is chosen per-request instead of fixed server-wide.

---

## 2. AFP

The path-type byte on the wire selects the charset: type `1` (short name) and type `2` (long
name) both carry MacRoman bytes; type `3` carries UTF-8. An unrecognized path type falls back to
MacRoman, matching classic (pre-OS-9) AFP behavior.

Every path name — MacRoman or UTF-8 — is sent as a Pascal string: a single length byte followed by
up to 255 bytes of name. This is a deliberate simplification of the full AFP3 Unicode-name wire
format (which specifies a 4-byte text-encoding hint plus a 2-byte length ahead of the UTF-8 bytes,
not a 1-byte Pascal-string length); there's no per-path "Unicode hint" field or text-encoding
negotiation here. In practice this only matters for names near or over 255 bytes — anything a
normal classic-Mac or modern client sends fits comfortably either way.

The server advertises AFP versions up to 2.2 by default; UTF-8 path-type handling is present
regardless, but treat it as a compatibility simplification of AFP3 Unicode support rather than a
full implementation of the AFP3 wire format.

---

## 3. SMB1

The Unicode flag (`SMB_FLAGS2_UNICODE`) is read **per request**, not fixed by the negotiated
dialect — the same session can freely mix ANSI and UTF-16LE requests. Path separators are split in
whichever charset the request used (a single `0x5C` byte for ANSI, the two-byte little-endian unit
for UTF-16), so a UTF-16 name is never mis-split on a low byte that happens to equal a backslash.

Paths are resolved case-insensitively regardless of the host filesystem's own case sensitivity —
SMB names are caseless by convention (confirmed against a real capture: OS/2's Workplace Shell
creates `foo.lnk`, then queries it back as `foo.LNK`).

**OS/2 long names:** before NT-style long-name SMB existed, OS/2 clients set a genuine long name
over an 8.3 host name using a `.LONGNAME` extended attribute (via `TRANS2_SET_PATH/FILE_INFORMATION`).
Path resolution understands this: when a case-insensitive match against the real host name misses,
each path component is retried against any `.LONGNAME` EA bound to a sibling entry — confirmed
against a real OS/2 capture that sets `.LONGNAME` on one exchange and opens by that long name on a
later one.

**8.3 wildcard matching** (`SMB_COM_SEARCH`/`FIND_FIRST2`): both the candidate name and the search
pattern are split into base/extension segments at the first `.` and matched independently,
case-insensitively. A `?` in the pattern matches one character *or nothing* once the name segment
has run out — the documented DOS quirk needed for an all-wildcard pattern like `????????.???` to
match an extensionless directory name; without it, extensionless directories silently vanish from
every directory browse.

**A protocol-precision trap worth knowing about:** per the CIFS spec, the short-name field in a
`FIND_FIRST2` "both directory info" reply must always be UTF-16LE, even on a plain ANSI (non-
Unicode) session — a real NT 3.51 capture shows that getting this wrong (sending it in the
session's own wire charset instead) makes NT silently discard every entry in the reply, i.e. the
share appears completely empty. A zero-length short-name field means "this entry has no distinct
8.3 alternate name," which is what's sent when the long name already fits 8.3 as-is.

---

## 4. NCP / NetWare

NCP addresses a filename through one of several **name spaces**, each with its own charset:

| Name space | Charset | Notes |
|---|---|---|
| DOS | ANSI/OEM, upper-cased | Always served; the 8.3 name every client can fall back to. |
| Macintosh | MacRoman | 31-character long name. |
| NFS | UTF-8 | Case-sensitive long name. |
| OS/2 | ANSI/OEM | Long name, case-preserving. |
| FTAM | — | Not served. |

General path resolution (splitting a request path into components before dispatching to a
specific name-space operation) always assumes ANSI/OEM bytes — NetWare 3.x itself predates
Unicode, so this matches real bindery-era behavior. Only the two long-name spaces (Macintosh,
NFS) get their own charset once a specific name-space operation is in play.

---

## 5. EtherDFS

EtherDFS is raw DOS INT 21h redirector traffic over Ethernet with no session or charset
negotiation at all — there's no equivalent of AFP's path-type byte or SMB's Unicode flag. Names on
the wire are DOS 8.3 "FCB" format: 8 bytes of base name, 3 bytes of extension, space-padded, no
embedded dot (`REPORT~1.XLS` becomes the 11 bytes `REPORT~1XLS`). No charset transcoding happens
at all on this protocol — bytes pass through as the DOS client sent them, in whatever single-byte
code page that client itself is using; if the client sent a derived short name for a longer host
name, it's reversed back to the real host filename using the same short-name lookup described
below.

---

## 6. Length limits and 8.3/31-character name derivation

One naming engine derives both the 8.3 short name and the 31-character "medium" (classic Mac long)
name from a real host filename, and is shared identically across all four protocols — AFP serves
its wire long name through it, NCP its 8.3 field, SMB its short name, and EtherDFS reverses a wire
8.3 name back to the real host filename through it, so a name derived for one protocol is stable
and consistent no matter which protocol asks for it (or reverses it) next.

Derivation only kicks in when a name doesn't already fit as-is:

- **8.3 short name:** if the host name's base is ≤8 characters, its extension ≤3, and both
  contain only FAT-safe characters once upper-cased (letters, digits, and a small set of
  DOS-legal punctuation — spaces and anything else are stripped), it's used unchanged. Otherwise
  a collision-numbered short name is generated: the base is upper-cased, stripped of anything
  FAT-illegal, truncated to make room for a `~N` suffix, and the extension is capped to 3
  characters — e.g. `My Report.docx` becomes something like `MYREP~1.DOC`.
- **31-character medium name:** a host name of 31 characters or fewer is used unchanged;
  longer names get a `-N` numeric suffix and are truncated to fit.
- Both directions are reversible: given a derived name, the same lookup returns the original host
  filename it was derived from.

| Context | Limit |
|---|---|
| DOS/FAT 8.3 (SMB legacy names, NCP DOS name space, EtherDFS) | 8-character base + 3-character extension, upper-case |
| Classic Mac / AFP long name, NCP Macintosh name space | 31 characters |
| AFP UTF-8 path type, NCP NFS/OS2 name spaces | the host filename itself, no derived-name limit |

---

## 7. Illegal characters — summary

- **Store → wire:** a character the store charset marked reserved when the name was written is
  escaped as an ASCII token; reading it back restores the original character, **unless** the
  destination is a DOS/Windows wire charset and the character is one Windows can't represent at
  all — in which case the escaped text is left as-is rather than handing a client a byte it would
  choke on.
- **Wire → store:** a name that can't be represented in the store's charset (e.g. encoding a name
  back out to a MacRoman-only client when it contains a character with no MacRoman equivalent)
  fails as an illegal name — each protocol reports this in its own native error, never a silently
  mangled path.
- **Length overflow:** handled entirely by the 8.3/31-character derivation in
  [§6](#6-length-limits-and-83-31-character-name-derivation), not by the charset codec — a
  protocol always asks for the already-derived short/medium name before wire-encoding it, so the
  codec itself never has to truncate anything.

---

## 8. See also

- [forks.md](forks.md) — resource forks, Finder info, and DOS attributes; a related but separate
  concern from the name transcoding covered here.
- [config.md](config.md) — the `filename_codec` server.toml key.
- [`spec/16-storage-seam.md`](../spec/16-storage-seam.md) — the underlying design document.
- [`spec/errata.md`](../spec/errata.md) — documented deviations from spec/real-client behavior,
  including the SMB 8.3 wildcard and `FIND_FIRST2` short-name quirks described above.
