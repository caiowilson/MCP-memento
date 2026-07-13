package parsing

import (
	"strings"
	"testing"
)

func TestAnalyzePHPUsesGrammarForModernDeclarations(t *testing.T) {
	source := []byte(`<?php
namespace App\Domain;

use Framework\Route;
use Vendor\Package\{One, Two as Dos};
require_once './bootstrap.php';

#[Route('/service')]
readonly class Service extends BaseService implements Contract, Auditable
{
    use LogsActions, TracksChanges;

    public const string NAME = 'service';

    public function __construct(private readonly One $one) {}

    public function run(One|Dos|null $value): Result
    {
        return new Result();
    }

    public string $displayName {
        get => $this->displayName;
        set => trim($value);
    }
}

enum Status: string { case Ready = 'ready'; }

function helper(): void {}
`)
	analysis, err := Analyze("fixture.php", source)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Language != "php" || analysis.PackageName != `App\Domain` {
		t.Fatalf("unexpected metadata: %#v", analysis)
	}
	for _, want := range []string{`Framework\Route`, `Vendor\Package\One`, `Vendor\Package\Two`, "./bootstrap.php"} {
		if !containsStringValue(analysis.Imports, want) {
			t.Fatalf("imports=%#v, missing %q", analysis.Imports, want)
		}
	}

	assertPHPSymbol(t, analysis.Symbols, "Service", "class", "", "readonly class Service", "")
	assertPHPSymbol(t, analysis.Symbols, "one", "property", "Service", "private readonly One $one", "")
	assertPHPSymbol(t, analysis.Symbols, "NAME", "const", "Service", "public const string NAME", "service")
	assertPHPSymbol(t, analysis.Symbols, "run", "method", "Service", "One|Dos|null $value", "return new Result")
	display := assertPHPSymbol(t, analysis.Symbols, "displayName", "property", "Service", "public string $displayName", "get =>")
	if display.ExtentEndLine <= display.StartLine {
		t.Fatalf("property hook body missing from anchor extent: %#v", display)
	}
	assertPHPSymbol(t, analysis.Symbols, "Status", "enum", "", "enum Status", "")
	assertPHPSymbol(t, analysis.Symbols, "Ready", "case", "Status", "case Ready", "")
	assertPHPSymbol(t, analysis.Symbols, "helper", "function", "", "function helper", "")

	if !containsLine(analysis.DeclarationStarts, 8) || !containsLine(analysis.DeclarationStarts, 28) || !containsLine(analysis.DeclarationStarts, 30) {
		t.Fatalf("unexpected declaration starts: %#v", analysis.DeclarationStarts)
	}
}

func TestAnalyzePHPRelationsResolveEachNamespaceScope(t *testing.T) {
	source := []byte(`<?php
namespace First {
    use Vendor\Model as Entity;
    use Vendor\{SupportTrait, Marker};
    use function Vendor\helper;
    use const Vendor\FLAG;
	require_once __DIR__ . '/bootstrap.php';
	include dirname(__DIR__) . '/shared.php';
	function first_helper(): void {}

    class Service extends \Framework\Base implements Contract
    {
        use SupportTrait;

        #[Marker]
        public function run(Entity $entity): Result
        {
            return $entity instanceof Entity ? Entity::make() : new Result();
        }
    }
}

namespace Second {
    use Other\Model as Entity;
	function second_helper(): void {}
    class Service { public function run(Entity $entity): void {} }
}
`)
	analysis, err := Analyze("fixture.php", source)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []Relation{
		{Kind: RelationDeclaration, Name: `First\Service`},
		{Kind: RelationDeclaration, Name: `Second\Service`},
		{Kind: RelationFunction, Name: `First\first_helper`},
		{Kind: RelationFunction, Name: `Second\second_helper`},
		{Kind: RelationImport, Name: `Vendor\Model`, Alias: "Entity"},
		{Kind: RelationImport, Name: `Other\Model`, Alias: "Entity"},
		{Kind: RelationTraitUse, Name: `Vendor\SupportTrait`},
		{Kind: RelationReference, Name: `Framework\Base`},
		{Kind: RelationReference, Name: `First\Contract`},
		{Kind: RelationReference, Name: `Vendor\Marker`},
		{Kind: RelationReference, Name: `Vendor\Model`},
		{Kind: RelationReference, Name: `First\Result`},
		{Kind: RelationReference, Name: `Other\Model`},
		{Kind: RelationInclude, Name: `bootstrap.php`},
		{Kind: RelationInclude, Name: `../shared.php`},
	} {
		if !containsRelation(analysis.Relations, want) {
			t.Fatalf("relations=%#v, missing %#v", analysis.Relations, want)
		}
	}
	for _, include := range []string{"bootstrap.php", "../shared.php"} {
		if !containsStringValue(analysis.Imports, include) {
			t.Fatalf("imports=%#v, missing static include %q", analysis.Imports, include)
		}
	}
	for _, forbidden := range []string{`Vendor\helper`, `Vendor\FLAG`, `First\Entity`, `Second\Entity`} {
		for _, relation := range analysis.Relations {
			if relation.Name == forbidden {
				t.Fatalf("unexpected relation %#v in %#v", relation, analysis.Relations)
			}
		}
	}
}

func TestPHPBearingExtensionsUsePHPGrammar(t *testing.T) {
	for _, path := range []string{"index.php", "legacy.php5", "template.phtml", "autoload.inc", "site.module", "site.install", "site.theme", "site.profile", "template.engine"} {
		if !IsPHPPath(path) || !Supported(path) {
			t.Fatalf("expected %s to use PHP grammar", path)
		}
	}
	if !IsPHPPath("view.blade.php") || Supported("view.blade.php") {
		t.Fatal("Blade templates must stay in PHP indexing/relationships but use the scanner fallback")
	}
}

func assertPHPSymbol(t *testing.T, symbols []Symbol, name, kind, container, signatureContains, signatureOmits string) Symbol {
	t.Helper()
	symbol, ok := symbolNamed(symbols, name)
	if !ok {
		t.Fatalf("missing %s in %#v", name, symbols)
	}
	if symbol.Kind != kind || symbol.Container != container || !strings.Contains(symbol.Signature, signatureContains) || signatureOmits != "" && strings.Contains(symbol.Signature, signatureOmits) {
		t.Fatalf("unexpected symbol %#v", symbol)
	}
	return symbol
}

func containsStringValue(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsRelation(relations []Relation, want Relation) bool {
	for _, relation := range relations {
		if relation == want {
			return true
		}
	}
	return false
}
