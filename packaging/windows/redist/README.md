# Bundled redistributables

ClassicStack.iss looks here (at compile time) for the third-party installers
it can silently run when the matching Setup task is selected. Both are
`#ifexist`-guarded in the .iss: if a file below is absent, ISCC still
compiles, that task just doesn't offer a bundled install and instead links
out to the vendor's download page.

Place the official installers here, unmodified, exactly as named:

- `npcap-installer.exe` — the Npcap OEM/silent-capable installer
  (https://npcap.com/#download). Needed for EtherTalk/IPX/NetBEUI, which
  talk to Ethernet via raw pcap capture.
- `winfsp-installer.msi` — the WinFsp installer (https://winfsp.dev/rel/).
  Needed for csmount to mount AFP/SMB/NCP shares as local drives.

Both installers accept a silent flag ClassicStack.iss already passes
(`/S` for Npcap's OEM installer, `/quiet /norestart` via msiexec for WinFsp
MSIs) — check the versions you download still support that flag before
wiring up an unattended release.

This directory's contents are gitignored (see the root .gitignore) —
redistributing third-party installer binaries through this repo is a
per-release packaging step, not something to commit.
