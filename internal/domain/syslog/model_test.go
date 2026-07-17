package syslog

import (
	"testing"
	"time"
)

func TestParseTime(t *testing.T) {
	if got, err := ParseTime(""); err != nil || got != 0 {
		t.Fatalf("empty = %d, %v", got, err)
	}
	if got, err := ParseTime("1700000000"); err != nil || got != 1700000000 {
		t.Fatalf("epoch = %d, %v", got, err)
	}
	date, err := ParseTime("2026-07-10")
	if err != nil {
		t.Fatalf("date error = %v", err)
	}
	if want := time.Date(2026, 7, 10, 0, 0, 0, 0, time.Local).Unix(); date != want {
		t.Fatalf("date = %d, want %d", date, want)
	}
	dt, err := ParseTime("2026-07-10 13:05:00")
	if err != nil {
		t.Fatalf("datetime error = %v", err)
	}
	if want := time.Date(2026, 7, 10, 13, 5, 0, 0, time.Local).Unix(); dt != want {
		t.Fatalf("datetime = %d, want %d", dt, want)
	}
	if _, err := ParseTime("not-a-time"); err == nil {
		t.Fatal("expected error for invalid time")
	}
}
