package main

import (
	"strings"
	"testing"
)

func TestVersionNamesTheBuild(t *testing.T) {
	oldTag, oldRev := buildTag, revision
	defer func() { buildTag, revision = oldTag, oldRev }()
	buildTag, revision = "v1.2.3", "0123456789abcdef"
	if got := buildVersion(); !strings.HasPrefix(got, "filebox v1.2.3 (go") || !strings.HasSuffix(got, ", 0123456)") {
		t.Fatalf("a build told its commit: %q", got)
	}
	buildTag, revision = "v1.2.3", ""
	if got := buildVersion(); !strings.HasPrefix(got, "filebox v1.2.3 (go") {
		t.Fatalf("a build that is not told: %q", got)
	}
}
