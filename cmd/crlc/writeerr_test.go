package main

import (
	"errors"
	"strings"
	"testing"
)

type failingWriter struct{}

func (failingWriter) Write(p []byte) (int, error) { return 0, errors.New("pipe closed") }

// A discarded stdout write used to leave the exit code at 0 even though the
// output never arrived. A failed write must now yield a non-zero exit.
func TestRunFailsWhenStdoutWriteFails(t *testing.T) {
	src := "crl v1\npackage p\nbundle b\n\nrule r\n\ttarget t.x\n" +
		"\tcollector c m file_upload from /f\n\t\tsignal a bool from f.a ttl 30d\n\tneed a == true\n"
	code := run([]string{"compile", "-"}, strings.NewReader(src), failingWriter{}, &strings.Builder{})
	if code == 0 {
		t.Fatal("compile returned exit 0 despite a failed stdout write")
	}
}
