package main

import (
	"reflect"
	"testing"
)

func TestReorderFlags(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"flags after prompt", []string{"Reply with exactly: OK. No other words.", "--timeout", "110s"}, []string{"--timeout", "110s", "Reply with exactly: OK. No other words."}},
		{"flags before prompt", []string{"-v", "hello"}, []string{"-v", "hello"}},
		{"bool flag then prompt", []string{"--new", "say hi"}, []string{"--new", "say hi"}},
		{"equated flag after prompt", []string{"go", "--timeout=5s"}, []string{"--timeout=5s", "go"}},
		{"no flags", []string{"status"}, []string{"status"}},
		{"mixed", []string{"--session", "abc", "prompt text", "-v"}, []string{"--session", "abc", "-v", "prompt text"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := reorderFlags(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("reorderFlags(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
