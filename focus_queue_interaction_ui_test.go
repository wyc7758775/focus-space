package main

import (
	"strings"
	"testing"
)

func TestFocusQueueActionsReceiveClicksWithoutOpeningCard(t *testing.T) {
	cssBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cssBytes), ".focus-v3-queue-actions{position:relative;z-index:1;pointer-events:auto}") {
		t.Fatal("focus queue actions must remain interactive above the card open target")
	}
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(scriptBytes), "event.stopPropagation();") {
		t.Fatal("focus queue feedback must not bubble into the card open path")
	}
}
