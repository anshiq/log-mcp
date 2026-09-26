package config

import (
	"testing"
)

func TestRedactor(t *testing.T) {
	r := CompileRedactor([]string{`secret-\d+`, `token=[^\s]+`})
	if got := r.Redact("key secret-123 ok"); got != "key *** ok" {
		t.Fatalf("got %q", got)
	}
	if got := r.Redact("nothing here"); got != "nothing here" {
		t.Fatalf("got %q", got)
	}
	var nilR *Redactor
	if nilR.Redact("x") != "x" {
		t.Fatal("nil redactor must pass through")
	}
	if CompileRedactor([]string{"(["}).Redact("([") != "([" {
		t.Fatal("invalid patterns must be skipped")
	}
}

func TestExpandPortTemplates(t *testing.T) {
	env := []string{"PORT=${port:api}", "PLAIN=1", "TWO=${port:api}/${port:db}"}
	got, err := ExpandPortTemplates(env, func(name string) (int, error) {
		if name == "api" {
			return 3000, nil
		}
		return 4000, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != "PORT=3000" || got[1] != "PLAIN=1" || got[2] != "TWO=3000/4000" {
		t.Fatalf("got %q", got)
	}
}
