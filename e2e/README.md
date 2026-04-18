# End-to-End Tests

This directory contains experimental scaffolding for running automated end-to-end testing using `minivmac` and Python-based `machfs` tooling.

The strategy includes:
1. Creating a disk image (`test.dsk`) holding the AppleScript to run.
2. Running `minivmac` with a boot disk and the new disk image.
3. The AppleScript (which would need to be executed inside the VM, perhaps by placing it in the Startup Items of a boot disk in a real run) mounts the OmniTalk AFP server, runs tasks, and logs results to `results.txt` on the test disk.
4. `extract_results.py` retrieves the log file from the disk image for the Go test to parse.

## Prerequisites
- `minivmac` in PATH
- `python3` in PATH
- `machfs` Python module installed (`pip install machfs`)
- A system boot disk for `minivmac` (e.g. `vMac.ROM` and a system disk image) configured correctly to run the script.
