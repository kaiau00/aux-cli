package relatedproject_test

import (
	"reflect"
	"testing"

	"github.com/kaiau00/aux-cli/internal/relatedproject"
)

func TestParseGoModDepsBlockForm(t *testing.T) {
	goMod := `module github.com/acme/app

go 1.24

require (
	github.com/acme/lib v1.2.3
	github.com/third/party v0.1.0 // indirect
)
`
	mod, deps := relatedproject.ParseGoModDeps(goMod)
	if mod != "github.com/acme/app" {
		t.Fatalf("modulePath = %q", mod)
	}
	want := []string{"github.com/acme/lib", "github.com/third/party"}
	if !reflect.DeepEqual(deps, want) {
		t.Fatalf("deps = %v, want %v", deps, want)
	}
}

func TestParseGoModDepsSingleLineForm(t *testing.T) {
	goMod := "module github.com/acme/app\n\ngo 1.24\n\nrequire github.com/acme/lib v1.2.3\nrequire github.com/third/party v0.1.0 // indirect\n"
	mod, deps := relatedproject.ParseGoModDeps(goMod)
	if mod != "github.com/acme/app" {
		t.Fatalf("modulePath = %q", mod)
	}
	want := []string{"github.com/acme/lib", "github.com/third/party"}
	if !reflect.DeepEqual(deps, want) {
		t.Fatalf("deps = %v, want %v", deps, want)
	}
}

func TestParseGoModDepsEmpty(t *testing.T) {
	mod, deps := relatedproject.ParseGoModDeps("")
	if mod != "" || deps != nil {
		t.Fatalf("expected empty result for empty input, got mod=%q deps=%v", mod, deps)
	}
}
