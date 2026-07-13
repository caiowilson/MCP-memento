package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePHPFileRelationsUsesParserAndFallsBackOnlyOnFailure(t *testing.T) {
	valid := parsePHPFileRelations("Service.php", "Service.php", `<?php
namespace App;
use Vendor\Base;
class Service extends Base {}
`)
	if !valid.parsed {
		t.Fatal("expected valid PHP to use parser-backed relations")
	}
	if !containsString(valid.declared, `App\Service`) || !containsString(valid.imports, `Vendor\Base`) {
		t.Fatalf("unexpected parser-backed declarations/imports: %#v", valid)
	}
	if !containsString(referencedPHPClasses(valid), `Vendor\Base`) {
		t.Fatalf("expected canonical parser-backed inheritance reference: %#v", referencedPHPClasses(valid))
	}
	multiNamespace := parsePHPFileRelations("Multi.php", "Multi.php", `<?php
namespace One {
    use Vendor\First as Shared;
    require __DIR__ . '/one.php';
    class A extends Shared {}
}
namespace Two {
    use Vendor\Second as Shared;
    class B extends Shared {}
}
`)
	if !multiNamespace.parsed || !containsString(multiNamespace.imports, `Vendor\First`) || !containsString(multiNamespace.imports, `Vendor\Second`) {
		t.Fatalf("expected imports from both bracketed namespaces: %#v", multiNamespace)
	}
	for _, class := range []string{`Vendor\First`, `Vendor\Second`} {
		if !containsString(referencedPHPClasses(multiNamespace), class) {
			t.Fatalf("expected canonical reference %q across bracketed namespaces: %#v", class, referencedPHPClasses(multiNamespace))
		}
	}
	if !containsString(multiNamespace.includes, "one.php") {
		t.Fatalf("expected parser-backed __DIR__ include: %#v", multiNamespace.includes)
	}

	malformed := parsePHPFileRelations("Broken.php", "Broken.php", `<?php
namespace App;
use Vendor\Base;
class Broken extends Base {
`)
	if malformed.parsed {
		t.Fatal("expected malformed PHP to use deterministic scanner fallback")
	}
	if !containsString(malformed.declared, `App\Broken`) || malformed.uses["base"] != `Vendor\Base` {
		t.Fatalf("unexpected scanner fallback declarations/imports: %#v", malformed)
	}
	if !containsString(referencedPHPClasses(malformed), `Vendor\Base`) {
		t.Fatalf("expected fallback inheritance reference: %#v", referencedPHPClasses(malformed))
	}
}

func TestComposerResolverPSR4PSR0ClassmapAndPrecedence(t *testing.T) {
	root := t.TempDir()
	fixture := map[string]string{
		"composer.json": `{
  "autoload": {
    "psr-4": {
      "App\\": ["first/", "src/"],
      "App\\Special\\": "special/",
      "": "fallback/"
    },
    "psr-0": {
      "Legacy\\": ["old/", "legacy/"],
      "Pear_": "pear/",
      "": "psr0-fallback/"
    },
    "classmap": ["classmap/"]
  },
  "autoload-dev": {
    "psr-4": {"Tests\\": "tests/"},
    "psr-0": {"Dev_": "dev-legacy/"},
    "classmap": ["dev-classmap/"],
    "exclude-from-classmap": ["/dev-classmap/Hidden.php"]
  }
}`,
		"first/Service.php":             "<?php namespace App; class Service {}\n",
		"src/Service.php":               "<?php namespace App; class Service {}\n",
		"src/Winner.php":                "<?php namespace App; class Winner {}\n",
		"special/Thing.php":             "<?php namespace App\\Special; class Thing {}\n",
		"fallback/Other/Thing.php":      "<?php namespace Other; class Thing {}\n",
		"legacy/Legacy/Tool/Name.php":   "<?php namespace Legacy; class Tool_Name {}\n",
		"pear/Pear/Package/Class.php":   "<?php class Pear_Package_Class {}\n",
		"psr0-fallback/Loose/Class.php": "<?php class Loose_Class {}\n",
		"classmap/Winner.php":           "<?php namespace App; class Winner {}\n",
		"tests/Feature.php":             "<?php namespace Tests; class Feature {}\n",
		"dev-legacy/Dev/Legacy.php":     "<?php class Dev_Legacy {}\n",
		"dev-classmap/Mapped.php":       "<?php class DevMapped {}\n",
		"dev-classmap/Hidden.php":       "<?php class DevHidden {}\n",
		"wrong/App/Absent.php":          "<?php namespace App; class Absent {}\n",
	}
	files := writeComposerPHPFixture(t, root, fixture)
	resolver := buildComposerResolver(root, readComposerAutoload(root), files)

	for _, test := range []struct {
		class string
		want  string
	}{
		{`App\Service`, "first/Service.php"},
		{`App\Special\Thing`, "special/Thing.php"},
		{`Other\Thing`, "fallback/Other/Thing.php"},
		{`Legacy\Tool_Name`, "legacy/Legacy/Tool/Name.php"},
		{`Pear_Package_Class`, "pear/Pear/Package/Class.php"},
		{`Loose_Class`, "psr0-fallback/Loose/Class.php"},
		{`App\Winner`, "classmap/Winner.php"},
		{`Tests\Feature`, "tests/Feature.php"},
		{`Dev_Legacy`, "dev-legacy/Dev/Legacy.php"},
		{`DevMapped`, "dev-classmap/Mapped.php"},
	} {
		t.Run(strings.ReplaceAll(test.class, `\`, "_"), func(t *testing.T) {
			got, claimed := resolver.resolveClass(test.class)
			if got != test.want || !claimed {
				t.Fatalf("resolve %q = %q, claimed=%v; want %q, true", test.class, got, claimed, test.want)
			}
		})
	}

	classFiles := map[string]string{"app\\absent": "wrong/App/Absent.php"}
	if got := resolvePHPClassToRel(`App\Absent`, classFiles, resolver); got != "" {
		t.Fatalf("matched PSR-4 prefix must not fall back to wrong-path declaration: %q", got)
	}
	if got, _ := resolver.resolveClass(`app\Service`); got != "" {
		t.Fatalf("PSR lookup must remain case-sensitive, got %q", got)
	}
	if got, _ := resolver.resolveClass(`DevHidden`); got != "" {
		t.Fatalf("autoload-dev exclusion resolved to %q", got)
	}
	if _, excluded := resolver.excluded["dev-classmap/Hidden.php"]; !excluded {
		t.Fatal("autoload-dev exclude-from-classmap was not applied")
	}
}

func TestComposerClassmapGlobsExtensionsAndExclusions(t *testing.T) {
	root := t.TempDir()
	fixture := map[string]string{
		"composer.json": `{
  "autoload": {
	"psr-4": {"Excluded\\": "legacy/Tests/"},
    "classmap": ["legacy/", "addons/*/lib/", "single.inc"],
    "exclude-from-classmap": ["/legacy/Tests/", "/addons/*/lib/Internal/"]
  }
}`,
		"legacy/Allowed.php":                 "<?php class LegacyAllowed {}\n",
		"legacy/Tests/Hidden.php":            "<?php namespace Excluded; class Hidden {}\n",
		"addons/one/lib/Plugin.php":          "<?php class AddonPlugin {}\n",
		"addons/one/lib/Internal/Secret.php": "<?php class AddonSecret {}\n",
		"single.inc":                         "<?php class IncMapped {}\n",
	}
	files := writeComposerPHPFixture(t, root, fixture)
	resolver := buildComposerResolver(root, readComposerAutoload(root), files)

	for class, want := range map[string]string{
		"LegacyAllowed":   "legacy/Allowed.php",
		"AddonPlugin":     "addons/one/lib/Plugin.php",
		"IncMapped":       "single.inc",
		`Excluded\Hidden`: "legacy/Tests/Hidden.php",
	} {
		if got, claimed := resolver.resolveClass(class); got != want || !claimed {
			t.Fatalf("classmap %s = %q, claimed=%v; want %q", class, got, claimed, want)
		}
	}
	if _, classmapped := resolver.classmap[`Excluded\Hidden`]; classmapped {
		t.Fatal("exclude-from-classmap must omit the classmap entry even when PSR-4 can resolve the file")
	}
	for _, class := range []string{"AddonSecret"} {
		if got, claimed := resolver.resolveClass(class); got != "" || claimed {
			t.Fatalf("excluded class %s resolved to %q, claimed=%v", class, got, claimed)
		}
	}
	for _, rel := range []string{"legacy/Tests/Hidden.php", "addons/one/lib/Internal/Secret.php"} {
		if _, ok := resolver.excluded[rel]; !ok {
			t.Fatalf("expected excluded classmap file %q", rel)
		}
	}
}

func TestRepoRelatedFilesPHPComposerFilesAndExcludedFallback(t *testing.T) {
	root := t.TempDir()
	fixture := map[string]string{
		"composer.json": `{
  "autoload": {
    "classmap": ["legacy/"],
    "files": ["bootstrap/functions.php"],
    "exclude-from-classmap": ["/legacy/Tests/"]
  },
  "autoload-dev": {
    "files": ["tests/bootstrap.inc"]
  }
}`,
		"bootstrap/functions.php": "<?php function project_helper(): void {}\n",
		"tests/bootstrap.inc":     "<?php function test_helper(): void {}\n",
		"legacy/Allowed.php":      "<?php class AllowedLegacy {}\n",
		"legacy/Tests/Hidden.php": "<?php class HiddenLegacy {}\n",
		"src/Consumer.php": `<?php
use AllowedLegacy;
use HiddenLegacy;
class Consumer
{
    public function run(): AllowedLegacy { return new AllowedLegacy(); }
    public function hidden(): HiddenLegacy { return new HiddenLegacy(); }
}
`,
	}
	writeComposerPHPFixture(t, root, fixture)
	tool := newRepoRelatedFilesTool(root)
	call := func(path string) []relatedCandidate {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"path": path, "includeSameDir": false})
		got, err := tool.Handler(context.Background(), raw)
		if err != nil {
			t.Fatal(err)
		}
		return got.(map[string]any)["related"].([]relatedCandidate)
	}

	manifest := call("composer.json")
	if !hasRelatedReason(manifest, "bootstrap/functions.php", "autoloads") || !hasRelatedReason(manifest, "tests/bootstrap.inc", "autoloads") {
		t.Fatalf("expected Composer files forward edges: %#v", manifest)
	}
	if reverse := call("tests/bootstrap.inc"); !hasRelatedReason(reverse, "composer.json", "autoloaded_by") {
		t.Fatalf("expected Composer files reverse edge: %#v", reverse)
	}
	consumer := call("src/Consumer.php")
	if !hasRelatedReason(consumer, "legacy/Allowed.php", "imports") || !hasRelatedReason(consumer, "legacy/Allowed.php", "semantic_reference") {
		t.Fatalf("expected allowed classmap relationships: %#v", consumer)
	}
	if containsRelated(consumer, "legacy/Tests/Hidden.php") {
		t.Fatalf("excluded classmap file leaked through declaration fallback: %#v", consumer)
	}
}

func TestComposerPathsStayInsideRepository(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "Outside.php")
	if err := os.WriteFile(outside, []byte("<?php class Outside {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture := map[string]string{
		"composer.json": `{
  "autoload": {
    "psr-4": {"Escape\\": "../"},
    "classmap": ["../"],
    "files": ["../Outside.php"]
  }
}`,
		"src/Local.php": "<?php class Local {}\n",
	}
	files := writeComposerPHPFixture(t, root, fixture)
	config := readComposerAutoload(root)
	resolver := buildComposerResolver(root, config, files)
	if got, _ := resolver.resolveClass(`Escape\Outside`); got != "" {
		t.Fatalf("escaped PSR mapping resolved outside repository: %q", got)
	}
	if len(resolver.classmap) != 0 {
		t.Fatalf("escaped classmap populated entries: %#v", resolver.classmap)
	}
	if got := composerAutoloadFiles(root, config.files, loadGitIgnored(root)); len(got) != 0 {
		t.Fatalf("escaped files autoload entries survived: %#v", got)
	}
}

func TestComposerManifestMalformedValuesFailClosed(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "composer.json"), []byte(`{
  "autoload": {
    "psr-4": {
      "MissingSeparator": "src/",
      "WrongType\\": 42
    }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	config := readComposerAutoload(root)
	if len(config.psr4) != 0 || len(config.psr0) != 0 || len(config.classmap) != 0 || len(config.files) != 0 {
		t.Fatalf("malformed Composer mappings should fail closed: %#v", config)
	}

	if err := os.WriteFile(filepath.Join(root, "composer.json"), []byte(`{"autoload":`), 0o644); err != nil {
		t.Fatal(err)
	}
	config = readComposerAutoload(root)
	if len(config.psr4) != 0 || len(config.psr0) != 0 || len(config.classmap) != 0 || len(config.files) != 0 {
		t.Fatalf("invalid Composer JSON should fail closed: %#v", config)
	}
}

func TestPHPRelationDispatchAndNativeMIMEUseSharedPHPPaths(t *testing.T) {
	for _, path := range []string{"src/Service.php", "legacy/bootstrap.inc", "module/example.module", "theme/page.theme", "composer.json"} {
		if !isPHPRelationPath(path) {
			t.Errorf("expected PHP relationship invalidation for %q", path)
		}
	}
	if isPHPRelationPath("src/app.js") {
		t.Fatal("did not expect JavaScript to invalidate PHP relationships")
	}
	for _, path := range []string{"legacy/bootstrap.inc", "module/example.module", "theme/page.theme"} {
		if got := resourceMIMEType(path); got != "application/x-httpd-php" {
			t.Errorf("resourceMIMEType(%q) = %q", path, got)
		}
	}
}

func writeComposerPHPFixture(t *testing.T, root string, fixture map[string]string) map[string]phpFileRelations {
	t.Helper()
	files := map[string]phpFileRelations{}
	for rel, content := range fixture {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if isPHPRelationFile(rel) {
			files[rel] = parsePHPFileRelations(abs, rel, content)
		}
	}
	return files
}
