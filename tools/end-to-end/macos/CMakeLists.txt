# ClassicStack AFP end-to-end test app — System 7.1, built with Retro68.
#
# To build (in WSL, with Retro68 already built at ~/Retro68-build):
#   mkdir build && cd build
#   cmake .. -DCMAKE_TOOLCHAIN_FILE=$HOME/Retro68-build/toolchain/m68k-apple-macos/cmake/retro68.toolchain.cmake
#   make
#
# Produces AFPE2E.APPL (+ .bin) in the build directory. See README.md for how
# to get it onto a disk image and run it under Snow.

cmake_minimum_required(VERSION 3.10)
project(AFPE2E C CXX)

# CONSOLE links RetroConsole (a text-window stdio redirector, ~800KB of
# mostly C++ UI code) so every printf/fprintf(stderr, ...) in the app shows
# up as a visible, live console under Snow/Mini vMac — needed while chasing
# bugs like "results.txt never gets written". Off by default: turn it on
# with -DAFPE2E_CONSOLE=ON when you need to watch the app run.
option(AFPE2E_CONSOLE "Link RetroConsole for a visible diagnostic console (much larger binary)" OFF)

# Off by default: the app just quits back to the Finder when the script
# finishes, so the same booted emulator/machine can be reused for repeated
# test runs without a full power cycle (faster debugging loop, and it avoids
# accidentally testing against a stale disk/emulator snapshot from a
# previous run). Turn on for an unattended harness run where nothing else
# needs the machine afterwards.
option(AFPE2E_SHUTDOWN_ON_EXIT "Power the emulated machine off via the Shutdown Manager when the script finishes" OFF)

# Off = registers zero script commands and skips compiling the AppleTalk/AFP
# sources entirely (atalk/asp/afp_client/volmount/fileops/macipx — the code that
# makes classic AppleTalk/.MPP/.XPP Toolbox trap calls and File Manager calls
# against a mounted AFP volume) — matches the original stage-1 skeleton. Used to
# isolate whether a crash comes from that Toolbox-calling code or from the
# script/results plumbing underneath it. On by default (normal builds want the
# AppleTalk/AFP commands); turn off only for isolation testing.
option(AFPE2E_AFP_COMMANDS "Compile in the AppleTalk/AFP script commands" ON)

set(AFPE2E_APP_OPTIONS "")
if(AFPE2E_CONSOLE)
    list(APPEND AFPE2E_APP_OPTIONS CONSOLE)
endif()

set(AFPE2E_SOURCES
    src/main.c
    src/results.c
    src/script/script.c
    src/script/commands.c
    src/platform/mac_files.c
    src/platform/statuswin.c
    )
# The full AppleTalk/AFP client:
#   atalk.c      — driver init, ZIP zone discovery, NBP server lookup, LAP type
#   asp.c        — ASP session layer over the .XPP driver
#   afp_client.c — FPGetSrvrInfo parse + guest-login volume ("share") enumeration
#   volmount.c   — PBVolumeMount an AFP share as a real Mac volume (vRefNum)
#   fileops.c    — File Manager file/fork/directory ops against the mounted volume
#   macipx.c     — MacIPX version/address placeholders
if(AFPE2E_AFP_COMMANDS)
    list(APPEND AFPE2E_SOURCES
        src/afp/atalk.c
        src/afp/asp.c
        src/afp/afp_client.c
        src/afp/volmount.c
        src/afp/fileops.c
        src/net/macipx.c
        )
endif()

add_application(AFPE2E ${AFPE2E_APP_OPTIONS}
    ${AFPE2E_SOURCES}
    TYPE "APPL"
    CREATOR "Ae2e"
    )

target_include_directories(AFPE2E PRIVATE src)

if(AFPE2E_SHUTDOWN_ON_EXIT)
    target_compile_definitions(AFPE2E PRIVATE AFPE2E_SHUTDOWN_ON_EXIT=1)
else()
    target_compile_definitions(AFPE2E PRIVATE AFPE2E_SHUTDOWN_ON_EXIT=0)
endif()

if(AFPE2E_AFP_COMMANDS)
    target_compile_definitions(AFPE2E PRIVATE AFPE2E_AFP_COMMANDS=1)
else()
    target_compile_definitions(AFPE2E PRIVATE AFPE2E_AFP_COMMANDS=0)
endif()

# Build flags: MULTI-SEGMENT (the Retro68 default for a large APPL).
#
# A Mac APPLICATION must use CODE resources; Retro68 gives two layouts for that:
#
#   --mac-single  ONE CODE segment, intra-segment refs. Caps total code at
#                 64 KB; over that, startup/relocation breaks and the first
#                 Toolbox call jumps to garbage ("MOVE.w invalid addressing
#                 mode", bus error at IsMPPOpen). The full AFP set is >64 KB, so
#                 single-segment is out. (Dialog/MPWTool use it for SMALL apps.)
#   (default)     MULTI-SEGMENT: code split across CODE 0/1/2… reached via a jump
#                 table + LoadSeg. No 64 KB cap. This is the recipe the LARGE
#                 samples (Raytracer/HelloWorld/Launcher) use: only
#                 "-Wl,-gc-sections" (+ -ffunction-sections), no --mac-single.
#                 <-- we use this.
#
# (--mac-flat is NOT an option here: it builds a flat code blob for a code
# RESOURCE — an INIT/flt/WDEF, as Samples/SystemExtension does — not an APPL.
# There is no flat APPL Rez template, so add_application can't package it:
# "Could not read binary resource file AFPE2E.code.bin".)
#
# -ffunction-sections + -Wl,-gc-sections drop unreached functions. The EARLIER
# -gc-sections crash in the note history was a C++ vtable reachability issue;
# this is pure C with no vtables, and Raytracer/HelloWorld prove gc-sections is
# fine multi-segment.
#
# printf_compat.h (force-included below) routes sprintf/fprintf/printf to
# newlib's integer-only variants so the ~30 KB float formatter
# (_dtoa_r/_vfprintf_r) never links — this app formats zero floats, so a leaner
# image is free (and keeps individual CODE segments small).
set_target_properties(AFPE2E PROPERTIES COMPILE_OPTIONS
    "-Os;-ffunction-sections;-include;${CMAKE_CURRENT_SOURCE_DIR}/src/platform/printf_compat.h")
if(CMAKE_SYSTEM_NAME MATCHES Retro68)
    set_target_properties(AFPE2E PROPERTIES LINK_FLAGS "-Wl,-gc-sections -Wl,--mac-strip-macsbug")
endif()

# Copy scripts/basic.txt onto the generated AFPE2E.dsk as "script.txt" (TEXT/
# ttxt) so the disk image in the build directory is ready to boot straight
# into a real test run, no manual hfsutils step needed. hmount/hcopy/hattrib
# ship alongside the cross-compiler in the Retro68 toolchain's bin/ dir.
get_filename_component(RETRO68_TOOLCHAIN_BIN "${CMAKE_C_COMPILER}" DIRECTORY)
find_program(HMOUNT_EXE hmount HINTS "${RETRO68_TOOLCHAIN_BIN}")
find_program(HUMOUNT_EXE humount HINTS "${RETRO68_TOOLCHAIN_BIN}")
find_program(HCOPY_EXE hcopy HINTS "${RETRO68_TOOLCHAIN_BIN}")
find_program(HATTRIB_EXE hattrib HINTS "${RETRO68_TOOLCHAIN_BIN}")

if(HMOUNT_EXE AND HUMOUNT_EXE AND HCOPY_EXE AND HATTRIB_EXE)
    add_custom_command(TARGET AFPE2E_APPL POST_BUILD
        COMMAND ${HUMOUNT_EXE} ARGS "AFPE2E" || ${CMAKE_COMMAND} -E true
        COMMAND ${HMOUNT_EXE} "${CMAKE_CURRENT_BINARY_DIR}/AFPE2E.dsk"
        COMMAND ${HCOPY_EXE} -t "${CMAKE_CURRENT_SOURCE_DIR}/scripts/basic.txt" :script.txt
        COMMAND ${HATTRIB_EXE} -t TEXT -c ttxt :script.txt
        COMMAND ${HUMOUNT_EXE}
        COMMENT "Copying scripts/basic.txt onto AFPE2E.dsk as script.txt"
        VERBATIM
        )
else()
    message(WARNING "hfsutils (hmount/hcopy/hattrib) not found next to the Retro68 compiler — "
                     "AFPE2E.dsk will not have scripts/basic.txt copied onto it automatically; "
                     "copy it by hand before running under Snow.")
endif()
