package logsafe

import (
	"errors"
	"testing"
)

func TestString_KeepsAValueOnOneLine(t *testing.T) {
	got := String("first\nsecond\r\nthird")
	if got != `first\nsecond\r\nthird` {
		t.Fatalf("String() = %q", got)
	}
	if plain := String("no breaks here"); plain != "no breaks here" {
		t.Errorf("a value without line breaks changed: %q", plain)
	}
}

func TestErr_RendersNilAndBreaks(t *testing.T) {
	if got := Err(nil); got != "<nil>" {
		t.Errorf("Err(nil) = %q", got)
	}
	got := Err(errors.New("mlaas said:\n{\"forged\": \"line\"}"))
	if got != `mlaas said:\n{"forged": "line"}` {
		t.Errorf("Err() = %q", got)
	}
}
