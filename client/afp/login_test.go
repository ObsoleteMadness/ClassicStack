package afp

import (
	"strings"
	"testing"

	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/afp"
)

func TestPickPasswordUAMPrefersCleartext(t *testing.T) {
	srv := proto.ServerInfo{UAMs: []string{"Cleartxt passwrd", "Randnum exchange"}}
	got, err := pickPasswordUAM(srv)
	if err != nil {
		t.Fatalf("pickPasswordUAM: %v", err)
	}
	if got != "Cleartxt passwrd" {
		t.Fatalf("got %q, want Cleartxt passwrd", got)
	}
}

func TestPickCleartextUAMUsesServerSpelling(t *testing.T) {
	srv := proto.ServerInfo{UAMs: []string{"Randnum exchange", "Cleartxt passwrd"}}
	got, err := pickCleartextUAM(srv)
	if err != nil {
		t.Fatalf("pickCleartextUAM: %v", err)
	}
	if got != "Cleartxt passwrd" {
		t.Fatalf("got %q, want server's exact spelling", got)
	}
}

func TestPickCleartextUAMRejectsWhenNotAdvertised(t *testing.T) {
	srv := proto.ServerInfo{UAMs: []string{"Randnum exchange"}}
	_, err := pickCleartextUAM(srv)
	if err == nil || !strings.Contains(err.Error(), "cleartext") {
		t.Fatalf("expected cleartext-not-offered error, got %v", err)
	}
}

func TestPickGuestUAMUsesServerSpelling(t *testing.T) {
	srv := proto.ServerInfo{UAMs: []string{"No User Authent", "Cleartxt passwrd"}}
	got, err := pickGuestUAM(srv)
	if err != nil {
		t.Fatalf("pickGuestUAM: %v", err)
	}
	if got != "No User Authent" {
		t.Fatalf("got %q", got)
	}
}

func TestPickGuestUAMRejectsWhenNotAdvertised(t *testing.T) {
	srv := proto.ServerInfo{UAMs: []string{"Cleartxt passwrd", "Randnum exchange"}}
	_, err := pickGuestUAM(srv)
	if err == nil || !strings.Contains(err.Error(), "guest") {
		t.Fatalf("expected guest-not-offered error, got %v", err)
	}
}
