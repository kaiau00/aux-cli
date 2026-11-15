package floorplan

import (
	"path"
	"regexp"
	"strings"
)

// File categories. Only CategoryCode contributes to the map's line counts: a
// lockfile's few thousand lines are not part of what anyone wrote, and
// counting them would overstate the project by a third.
const (
	CategoryCode      = "code"
	CategoryConfig    = "config"
	CategoryDocs      = "docs"
	CategoryData      = "data"
	CategoryAsset     = "asset"
	CategoryLock      = "lock"
	CategoryLog       = "log"
	CategoryGenerated = "generated"
	CategoryOther     = "other"
)

// jsonConfigMaxBytes is the size past which a .json file is taken to be data
// rather than hand-written configuration, however it is named.
const jsonConfigMaxBytes = 100_000

// langByExt maps an extension to the language reported for code files. Every
// CSS dialect and every HTML template language collapses to one name, because
// the map groups by what a file is for rather than by its preprocessor.
var langByExt = map[string]string{
	".ts": "typescript", ".tsx": "typescript", ".mts": "typescript", ".cts": "typescript",
	".js": "javascript", ".jsx": "javascript", ".mjs": "javascript", ".cjs": "javascript",
	".vue": "vue", ".svelte": "svelte", ".astro": "astro",
	".py": "python", ".pyi": "python",
	".go": "go", ".rs": "rust", ".java": "java", ".kt": "kotlin", ".kts": "kotlin",
	".scala": "scala", ".cs": "csharp", ".fs": "fsharp", ".rb": "ruby", ".php": "php",
	".swift": "swift", ".dart": "dart",
	".c": "c", ".h": "c", ".cc": "cpp", ".cpp": "cpp", ".cxx": "cpp", ".hpp": "cpp",
	".hh": "cpp", ".m": "objc", ".mm": "objc",
	".ex": "elixir", ".exs": "elixir", ".erl": "erlang", ".clj": "clojure",
	".lua": "lua", ".r": "r", ".jl": "julia", ".zig": "zig", ".elm": "elm",
	".hs": "haskell", ".ml": "ocaml", ".groovy": "groovy",
	".sh": "shell", ".bash": "shell", ".zsh": "shell", ".ps1": "powershell", ".psm1": "powershell",
	".sql": "sql", ".tf": "terraform", ".proto": "protobuf", ".graphql": "graphql", ".gql": "graphql",
	".css": "css", ".scss": "css", ".sass": "css", ".less": "css",
	".html": "html", ".htm": "html", ".ejs": "html", ".hbs": "html",
	".jinja": "html", ".j2": "html", ".njk": "html",
}

var (
	configExts = map[string]bool{
		".json": true, ".yaml": true, ".yml": true, ".toml": true, ".ini": true,
		".cfg": true, ".conf": true, ".xml": true, ".gradle": true,
		".properties": true, ".env": true,
	}
	docExts = map[string]bool{
		".md": true, ".mdx": true, ".rst": true, ".adoc": true, ".txt": true,
	}
	dataExts = map[string]bool{
		".csv": true, ".tsv": true, ".jsonl": true, ".ndjson": true,
		".parquet": true, ".sqlite": true, ".db": true, ".xlsx": true,
		".xls": true, ".avro": true,
	}
	assetExts = map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true,
		".ico": true, ".webp": true, ".avif": true, ".bmp": true, ".woff": true,
		".woff2": true, ".ttf": true, ".otf": true, ".eot": true, ".mp3": true,
		".mp4": true, ".wav": true, ".webm": true, ".mov": true, ".pdf": true,
		".zip": true, ".gz": true, ".tar": true, ".tgz": true, ".7z": true,
		".jar": true, ".wasm": true, ".exe": true, ".dll": true, ".so": true,
		".dylib": true, ".bin": true,
	}

	configNames = regexp.MustCompile(`^(?i)(dockerfile.*|makefile|procfile|justfile|\.env\..*|\.?[\w-]*rc|gemfile|rakefile|cargo\.toml|go\.mod|pyproject\.toml|setup\.cfg|requirements.*\.txt)$`)
	jsonConfig  = regexp.MustCompile(`^(?i)(package|tsconfig.*|jsconfig|composer|manifest|app|components|turbo|nx|lerna|angular|firebase|vercel|now|renovate|tslint|.?eslintrc|.?babelrc|.?prettierrc|.*\.config|.*rc|settings|launch|tasks|deno|biome|project|workspace|devcontainer|cdk|serverless|appsettings.*|openapi|swagger)\.json$`)
	lockNames   = regexp.MustCompile(`^(?i)(package-lock\.json|yarn\.lock|pnpm-lock\.yaml|bun\.lockb|poetry\.lock|pipfile\.lock|cargo\.lock|go\.sum|composer\.lock|gemfile\.lock|.*\.lock)$`)
	generated   = regexp.MustCompile(`(?i)(\.min\.(js|css)$|\.map$|\.tsbuildinfo$|\.pyc$|\.pyo$|\.class$|\.o$|(^|/)next-env\.d\.ts$)`)
)

// classify names what a file is and, for code, which language it is in.
//
// The order the checks run in is the whole of the logic. Generated output is
// decided first so a minified bundle is never counted as source; lockfiles
// come next so go.sum is a lock rather than Go; and a known source extension
// is only consulted after those, so the categories cannot be gamed by a
// familiar-looking suffix.
func classify(relPath string, size int64) (category, lang string) {
	base := strings.ToLower(path.Base(relPath))
	ext := path.Ext(base)

	switch {
	case generated.MatchString(relPath):
		return CategoryGenerated, ""
	case lockNames.MatchString(base):
		return CategoryLock, ""
	case ext == ".log" || strings.HasSuffix(base, ".log"):
		return CategoryLog, ""
	case assetExts[ext]:
		return CategoryAsset, ""
	case dataExts[ext]:
		return CategoryData, ""
	}
	if l, ok := langByExt[ext]; ok {
		return CategoryCode, l
	}
	// A .json file is configuration when its name says so and it is small
	// enough to have been written by hand; otherwise it is data.
	if ext == ".json" && (size > jsonConfigMaxBytes || !jsonConfig.MatchString(base)) {
		return CategoryData, ""
	}
	if configExts[ext] || configNames.MatchString(base) {
		if ext != "" {
			return CategoryConfig, strings.TrimPrefix(ext, ".")
		}
		return CategoryConfig, base
	}
	if docExts[ext] {
		return CategoryDocs, ""
	}
	return CategoryOther, ""
}

// CountsTowardLOC reports whether a category's line counts belong in the map's
// totals. Only code does: everything else is either generated, vendored,
// binary, or prose, and none of that is a measure of the project's size.
func CountsTowardLOC(category string) bool { return category == CategoryCode }
