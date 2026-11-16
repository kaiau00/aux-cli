package floorplan

import "testing"

// The categories and their precedence are Floorplan's, so a map Aux builds
// counts the same files as a map that project builds for the same repository.
func TestClassify(t *testing.T) {
	cases := []struct {
		path string
		size int64
		cat  string
		lang string
	}{
		// Source in a language the map knows.
		{"main.go", 100, CategoryCode, "go"},
		{"internal/db/q.sql", 100, CategoryCode, "sql"},
		{"web/app.tsx", 100, CategoryCode, "typescript"},
		{"web/app.scss", 100, CategoryCode, "css"}, // every CSS dialect reports as css
		{"tpl/page.hbs", 100, CategoryCode, "html"},
		{"s.sh", 100, CategoryCode, "shell"},

		// Generated output wins over everything, including its own language.
		{"dist/app.min.js", 100, CategoryGenerated, ""},
		{"app.js.map", 100, CategoryGenerated, ""},
		{"x.tsbuildinfo", 100, CategoryGenerated, ""},

		// Lockfiles are next, so go.sum is a lock and not Go source.
		{"go.sum", 100, CategoryLock, ""},
		{"package-lock.json", 100, CategoryLock, ""},
		{"Cargo.lock", 100, CategoryLock, ""},

		{"server.log", 100, CategoryLog, ""},

		// Assets have no line count worth reporting. SVG is an asset even
		// though it is text.
		{"logo.png", 100, CategoryAsset, ""},
		{"icon.svg", 100, CategoryAsset, ""},
		{"f.woff2", 100, CategoryAsset, ""},

		{"rows.csv", 100, CategoryData, ""},
		{"events.jsonl", 100, CategoryData, ""},

		// A JSON file is config when its name says so, and data otherwise or
		// when it is too big to be hand-written configuration.
		{"package.json", 100, CategoryConfig, "json"},
		{"tsconfig.json", 100, CategoryConfig, "json"},
		{"aux-schema.json", 100, CategoryData, ""},
		{"package.json", 200_000, CategoryData, ""},

		// Config by extension, and by name.
		{"config.yaml", 100, CategoryConfig, "yaml"},
		{"go.mod", 100, CategoryConfig, "mod"},
		{"Dockerfile", 100, CategoryConfig, "dockerfile"},
		{"Makefile", 100, CategoryConfig, "makefile"},
		{".eslintrc", 100, CategoryConfig, "eslintrc"},

		{"README.md", 100, CategoryDocs, ""},
		{"notes.txt", 100, CategoryDocs, ""},
		{"docs/adr/0001-x.md", 100, CategoryDocs, ""},

		{"LICENSE", 100, CategoryOther, ""},
		{"install", 100, CategoryOther, ""},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			cat, lang := classify(c.path, c.size)
			if cat != c.cat {
				t.Errorf("classify(%q) category = %q; want %q", c.path, cat, c.cat)
			}
			if lang != c.lang {
				t.Errorf("classify(%q) lang = %q; want %q", c.path, lang, c.lang)
			}
		})
	}
}

// Only code carries a line count into the map's totals. Counting a lockfile's
// 4,000 lines as part of the project would overstate it by a third.
func TestOnlyCodeCountsTowardLOC(t *testing.T) {
	if CountsTowardLOC(CategoryLock) {
		t.Error("a lockfile counts toward LOC")
	}
	if CountsTowardLOC(CategoryAsset) {
		t.Error("an asset counts toward LOC")
	}
	if !CountsTowardLOC(CategoryCode) {
		t.Error("code does not count toward LOC")
	}
}
