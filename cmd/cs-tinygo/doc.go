// Command cs-tinygo is a minimal main whose ONLY purpose is to give the TinyGo
// amd64 build gates (A4) something real to compile: it imports the TinyGo-safe
// subset of core/ so that a forbidden import or a reflection-using package on
// that subset makes `tinygo build` fail — proving the no-reflection /
// no-forbidden-import discipline without ESP32 hardware.
//
// Its import surface GROWS as more of core becomes TinyGo-clean (Phase 1/2).
// Packages that legitimately can't compile under TinyGo yet are simply not
// imported here.
//
// This is NOT a product binary; the real interactive entry point stays
// cmd/classicstack.
package main
