package indexing

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMeaningfulSearchTermsSplitIdentifiersAndDropStopWords(t *testing.T) {
	got := meaningfulSearchTerms("Where is JSONValue normalized by Report_Service?")
	want := []string{"json", "value", "normalized", "report", "service"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("meaningfulSearchTerms() = %#v; want %#v", got, want)
	}
	// Identifier splitting applies to indexed code/path text, not ordinary
	// query words that merely begin with a capital letter.
	if tokens := identifierSearchTokens("JsonValue ReportService"); !reflect.DeepEqual(tokens, []string{"json", "value", "report", "service"}) {
		t.Fatalf("identifierSearchTokens() = %#v", tokens)
	}
}

func TestMeaningfulSearchTermsForAuthoritativeDomainDefinitionDropsIntentVocabulary(t *testing.T) {
	query := "Which source fixes the authoritative domain's allowed membership freeze cause codes consumed by AccessTokenPresenter?"
	intent := classifyTermSearchIntent(query)
	got := meaningfulSearchTermsForIntent(query, intent)
	want := []string{"membership", "freeze", "cause"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("meaningfulSearchTermsForIntent() = %#v; want %#v", got, want)
	}
}

func TestMeaningfulSearchTermsForPermittedAuthoritativeDomainDropsRoleVocabulary(t *testing.T) {
	query := "Where is the authoritative cold-chain domain declaration that defines the permitted excursion dispositions and their stored string values?"
	intent := classifyTermSearchIntent(query)
	got := meaningfulSearchTermsForIntent(query, intent)
	want := []string{"cold", "chain", "excursion", "dispositions", "their"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("meaningfulSearchTermsForIntent() = %#v; want %#v", got, want)
	}
}

func TestMeaningfulSearchTermsKeepPermittedAndStoredOutsideAuthoritativeIntent(t *testing.T) {
	query := "Where are permitted stored values cached?"
	intent := classifyTermSearchIntent(query)
	if intent.authoritativeDomainDefinition {
		t.Fatalf("ordinary storage query activated authoritative-domain intent: %#v", intent)
	}
	got := meaningfulSearchTermsForIntent(query, intent)
	want := []string{"permitted", "stored", "values", "cached"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("meaningfulSearchTermsForIntent() = %#v; want %#v", got, want)
	}
}

func TestMeaningfulSearchTermsExcludeContrastClause(t *testing.T) {
	got := meaningfulSearchTerms("where does a method never return and throw instead of serializing a value")
	want := []string{"method", "never", "return", "throw"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("meaningfulSearchTerms() = %#v; want %#v", got, want)
	}
}

func TestTermSearchIntentClassifiesStructuralRoles(t *testing.T) {
	tests := []struct {
		name  string
		query string
		check func(termSearchIntent) bool
	}{
		{"attribute", "Where is the AuditTag attribute declared for methods?", func(intent termSearchIntent) bool { return intent.attribute && intent.definition }},
		{"callable", "Where is normalize converted into a first-class callable?", func(intent termSearchIntent) bool { return intent.callable }},
		{"deferred callable", "Where is formatLabel packaged up so it can be passed around and run later?", func(intent termSearchIntent) bool { return intent.callable }},
		{"config definition", "Which configuration entry defines the reporting endpoint?", func(intent termSearchIntent) bool { return intent.configDefinition && intent.definition }},
		{"relationship declaration", "Which entity mapping assigns its repository class?", func(intent termSearchIntent) bool { return intent.relationDeclaration && intent.definition }},
		{"collection relationship", "Where does the data model state that one parent record is connected to a collection of dependent records?", func(intent termSearchIntent) bool { return intent.relationDeclaration && intent.collectionRelation }},
		{"never termination", "Which method declares it never returns and terminates by throwing?", func(intent termSearchIntent) bool { return intent.neverTermination }},
		{"backed enum definition", "Where are the allowed phases and their persisted string values defined?", func(intent termSearchIntent) bool { return intent.backedEnumDefinition }},
		{"backed enum paraphrase", "Where are the canonical string codes for every parcel review outcome declared?", func(intent termSearchIntent) bool {
			return intent.backedEnumDefinition && intent.preferRelationshipTarget
		}},
		{"serialized domain labels", "Which source establishes the stable wire labels for every transit disposition?", func(intent termSearchIntent) bool {
			return intent.backedEnumDefinition && intent.preferRelationshipTarget
		}},
		{"serialized domain specification", "Which source specifies the canonical persisted values for every shipment disposition?", func(intent termSearchIntent) bool {
			return intent.backedEnumDefinition && intent.preferRelationshipTarget
		}},
		{"durable catalog spellings", "Which source fixes the durable catalog spellings for each museum object's pest-treatment stage?", func(intent termSearchIntent) bool {
			return intent.backedEnumDefinition && intent.catalogDomainDefinition && intent.preferRelationshipTarget
		}},
		{"serialized catalog spelling", "Which declaration fixes the durable serialized catalog spelling for every seed-lot viability category?", func(intent termSearchIntent) bool {
			return intent.backedEnumDefinition && intent.catalogDomainDefinition && intent.preferRelationshipTarget
		}},
		{"stored token domain", "Where is the authoritative closed set of stored tokens for every invoice delivery state established?", func(intent termSearchIntent) bool {
			return intent.backedEnumDefinition && intent.preferRelationshipTarget
		}},
		{"stored token domain with consumer context", "Which declaration establishes the authoritative closed set of stored tokens for every invoice delivery state consumed by InvoiceDeliverySerializer?", func(intent termSearchIntent) bool {
			return intent.backedEnumDefinition && intent.preferRelationshipTarget
		}},
		{"stored token domain with security-named consumer", "Which declaration establishes the authoritative closed set of stored tokens for every invoice delivery state consumed by AccessTokenPresenter?", func(intent termSearchIntent) bool {
			return intent.backedEnumDefinition && intent.preferRelationshipTarget
		}},
		{"stored token authentication state", "Where is the authoritative closed set of stored tokens for every authentication state established?", func(intent termSearchIntent) bool {
			return intent.backedEnumDefinition && intent.preferRelationshipTarget
		}},
		{"authoritative domain codes", "Where is the authoritative domain declaration that defines the allowed membership freeze cause codes?", func(intent termSearchIntent) bool {
			return intent.backedEnumDefinition && intent.preferRelationshipTarget
		}},
		{"authoritative domain codes with consumer context", "Which declaration defines the authoritative domain's allowed membership freeze cause codes consumed by AccessTokenPresenter?", func(intent termSearchIntent) bool {
			return intent.backedEnumDefinition && intent.preferRelationshipTarget
		}},
		{"authoritative domain codes with config-named consumer", "Which declaration defines the authoritative domain's allowed membership freeze cause codes consumed by ConfigurationPresenter?", func(intent termSearchIntent) bool {
			return intent.backedEnumDefinition && intent.preferRelationshipTarget
		}},
		{"permitted authoritative domain values", "Where is the authoritative cold-chain domain declaration that defines the permitted excursion dispositions and their stored string values?", func(intent termSearchIntent) bool {
			return intent.backedEnumDefinition && intent.authoritativeDomainDefinition && intent.preferRelationshipTarget
		}},
		{"shutdown registration", "Which implementation installs the callback after script termination, including an early exit?", func(intent termSearchIntent) bool { return intent.shutdownRegistration }},
		{"shutdown attachment paraphrase", "Where is the last-chance drain callback attached when the PHP process terminates?", func(intent termSearchIntent) bool {
			return intent.shutdownRegistration && intent.preferRelationshipTarget
		}},
		{"uninstall registration", "Which registration cleans up when the plugin is deleted?", func(intent termSearchIntent) bool { return intent.uninstallRegistration }},
		{"uninstall binding paraphrase", "Where does the plugin bind its permanent purge to when an administrator deletes the plugin?", func(intent termSearchIntent) bool {
			return intent.uninstallRegistration && intent.preferRelationshipSource
		}},
		{"config consumer", "Where is reporting configuration consumed by the exporter?", func(intent termSearchIntent) bool { return !intent.configDefinition }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if intent := classifyTermSearchIntent(test.query); !test.check(intent) {
				t.Fatalf("classifyTermSearchIntent(%q) = %#v", test.query, intent)
			}
		})
	}
}

func TestTermSearchIntentDoesNotPromoteSerializedConsumers(t *testing.T) {
	queries := []string{
		"Which function serializes every transit disposition into a stable wire label?",
		"Where does DispositionSerializer render the wire label for one transit disposition?",
		"Which presenter maps every transit disposition to its serialized label?",
		"Which function serializes every status into a specific wire label?",
		"Which function fixes the durable catalog spelling for each status before rendering?",
		"Which source fixes one catalog spelling for a status?",
		"Which source fixes each durable catalog spelling for a status before rendering it?",
		"Which source fixes each durable catalog spelling after the status is serialized?",
		"Which serializer fixes every durable catalog spelling for a status?",
		"Which serializer establishes the closed set of stored tokens for every invoice delivery state?",
		"Which method establishes the authoritative stored tokens for every invoice delivery state?",
	}
	for _, query := range queries {
		if intent := classifyTermSearchIntent(query); intent.backedEnumDefinition || intent.preferRelationshipTarget {
			t.Fatalf("consumer query activated serialized-domain definition intent: query=%q intent=%#v", query, intent)
		}
	}
}

func TestTermSearchIntentDoesNotPromoteUnrelatedTokenRequests(t *testing.T) {
	queries := []string{
		"Where are authentication tokens stored for every API client?",
		"Which declaration establishes the authoritative closed set of stored authentication tokens for every API client?",
		"Which declaration establishes the authoritative closed set of stored authentication tokens for every stateful API client?",
		"Which declaration establishes the authoritative closed set of stored authentication tokens for every session state?",
		"Which tokenizer establishes every parser token?",
		"Where is each request token stored in the database?",
	}
	for _, query := range queries {
		if intent := classifyTermSearchIntent(query); intent.backedEnumDefinition || intent.preferRelationshipTarget {
			t.Fatalf("unrelated token query activated serialized-domain definition intent: query=%q intent=%#v", query, intent)
		}
	}
}

func TestTermSearchIntentDoesNotPromoteUnrelatedAuthoritativeDomainRequests(t *testing.T) {
	queries := []string{
		"Which method defines the authoritative domain's allowed membership freeze cause codes?",
		"Which consumer declaration defines the authoritative domain's allowed membership freeze cause codes?",
		"Which controller declaration defines the authoritative domain's allowed membership freeze cause codes?",
		"Which handler declaration defines the authoritative domain's allowed membership freeze cause codes?",
		"Which presenter defines the authoritative domain declaration for allowed membership freeze cause codes?",
		"Which configuration entry defines the authoritative domain's allowed codes?",
		"Which configuration declaration defines the authoritative domain's allowed membership freeze cause codes?",
		"Which declaration documents codes from an authoritative domain?",
		"Which authoritative domain declaration defines all allowed security token values?",
		"Which authoritative domain declaration defines all allowed refresh token values?",
		"Which method defines the authoritative domain's permitted excursion disposition string values?",
		"Which consumer declaration defines the authoritative domain's permitted excursion disposition string values?",
		"Which configuration declaration defines the authoritative domain's permitted excursion disposition string values?",
		"Which authoritative domain declaration defines all permitted security token string values?",
		"Which authoritative domain declaration defines all permitted refresh token string values?",
	}
	for _, query := range queries {
		if intent := classifyTermSearchIntent(query); intent.backedEnumDefinition {
			t.Fatalf("unrelated authoritative-domain query activated backed-enum intent: query=%q intent=%#v", query, intent)
		}
	}
}

func TestTermSearchIntentDoesNotPromoteUnrelatedFixRequests(t *testing.T) {
	intent := classifyTermSearchIntent("Which source fixes the config parser bug?")
	if intent.definition || intent.configDefinition || intent.preferRelationshipTarget {
		t.Fatalf("unrelated fix request activated definition ranking: %#v", intent)
	}
}

func TestTermSearchRelationshipBonusesAreDirectionalAndCandidateBounded(t *testing.T) {
	edges := []RelationshipEdge{
		{FromPath: "consumer.php", ToPath: "provider.php"},
		{FromPath: "consumer.php", ToPath: "outside.php"},
		{FromPath: "provider.php", ToPath: "provider.php"},
	}
	candidates := []string{"consumer.php", "provider.php"}

	inbound := termSearchRelationshipBonuses(candidates, edges, termSearchIntent{preferRelationshipTarget: true})
	if inbound["provider.php"] != 20 || inbound["consumer.php"] != 0 || inbound["outside.php"] != 0 {
		t.Fatalf("unexpected inbound bonuses: %#v", inbound)
	}
	outbound := termSearchRelationshipBonuses(candidates, edges, termSearchIntent{preferRelationshipSource: true})
	if outbound["consumer.php"] != 20 || outbound["provider.php"] != 0 || outbound["outside.php"] != 0 {
		t.Fatalf("unexpected outbound bonuses: %#v", outbound)
	}
}

func TestMeaningfulSearchTermsForDefinitionDropsConsumerContext(t *testing.T) {
	query := "Which configuration entry defines the reporting endpoint consumed by ReportExporter?"
	intent := classifyTermSearchIntent(query)
	got := meaningfulSearchTermsForIntent(query, intent)
	want := []string{"configuration", "entry", "defines", "reporting", "endpoint"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("meaningfulSearchTermsForIntent() = %#v; want %#v", got, want)
	}
}

func TestMeaningfulSearchTermsForCatalogDefinitionDropsIntentVocabulary(t *testing.T) {
	query := "Which declaration fixes the durable serialized catalog spelling for every seed-lot viability category?"
	intent := classifyTermSearchIntent(query)
	got := meaningfulSearchTermsForIntent(query, intent)
	want := []string{"seed", "lot", "viability", "category"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("meaningfulSearchTermsForIntent() = %#v; want %#v", got, want)
	}
}

func TestTermsV4PreservesNeutralTermsV3Evidence(t *testing.T) {
	query := "where does ReportService load recent reports"
	intent := classifyTermSearchIntent(query)
	if intent != (termSearchIntent{}) {
		t.Fatalf("neutral query activated structural intent: %#v", intent)
	}
	terms := meaningfulSearchTerms(query)
	if got := meaningfulSearchTermsForIntent(query, intent); !reflect.DeepEqual(got, terms) {
		t.Fatalf("neutral terms changed: got=%v want=%v", got, terms)
	}
	chunks := []Chunk{
		{Path: "src/Service/ReportService.php", Language: "php", Content: "public function recentReports(): array { return []; }\n"},
		{Path: "src/Controller/ReportController.php", Language: "php", Content: "public function index(ReportService $reports): array { return $reports->recentReports(); }\n"},
	}
	for _, chunk := range chunks {
		before := termAwareChunkScore(chunk, terms)
		after := termAwareChunkScoreWithIntent(chunk, terms, intent)
		if after != before {
			t.Errorf("neutral score changed for %s: before=%d after=%d", chunk.Path, before, after)
		}
	}
}

func TestContrastClauseCannotActivateStructuralIntent(t *testing.T) {
	query := "where does ReportExporter read the endpoint rather than which configuration entry defines it"
	if intent := classifyTermSearchIntent(query); intent != (termSearchIntent{}) {
		t.Fatalf("contrast clause activated structural intent: %#v", intent)
	}
}

func TestSearchTermMatchQualityHandlesConservativeInflections(t *testing.T) {
	for _, pair := range [][2]string{
		{"normalized", "normalize"},
		{"handled", "handler"},
		{"strategies", "strategy"},
		{"typed", "type"},
		{"trimmed", "trim"},
		{"bound", "bind"},
		{"constant", "const"},
		{"iterator", "iterable"},
		{"located", "location"},
		{"itself", "this"},
	} {
		if quality := searchTermMatchQuality(pair[0], pair[1]); quality == 0 {
			t.Errorf("expected %q and %q to match", pair[0], pair[1])
		}
	}
	for _, pair := range [][2]string{{"route", "repository"}, {"thing", "think"}, {"class", "classic"}} {
		if quality := searchTermMatchQuality(pair[0], pair[1]); quality != 0 {
			t.Errorf("unexpected match for %q and %q: %d", pair[0], pair[1], quality)
		}
	}
}

func TestTermAwareChunkScoreUsesPositiveClauseAndSelfReference(t *testing.T) {
	terms := meaningfulSearchTerms("where does a producer return itself rather than only declare an interface")
	target := Chunk{Path: "LanguageFeatures.php", Language: "php", Content: "public function produce(): LanguageFeatures\n{\n    return $this;\n}\n"}
	distractor := Chunk{Path: "LanguageFeatures.php", Language: "php", Content: "interface Producer\n{\n    public function produce(): object;\n}\n"}
	if got, want := termAwareChunkScore(target, terms), termAwareChunkScore(distractor, terms); got <= want {
		t.Fatalf("self-returning declaration score %d must exceed interface score %d", got, want)
	}
}

func TestTermAwareChunkScorePrefersContentEvidenceOverSharedPath(t *testing.T) {
	terms := meaningfulSearchTerms("where are consumer dependencies consumed")
	header := Chunk{Path: "app/Consumer.php", Content: "<?php\nnamespace Fixture\\Composer\\App;\n"}
	declaration := Chunk{Path: "app/Consumer.php", Content: "final class Consumer { public function dependencies(): array {} }\n"}
	if got, want := termAwareChunkScore(declaration, terms), termAwareChunkScore(header, terms); got <= want {
		t.Fatalf("declaration score %d must exceed path-only header score %d", got, want)
	}
}

func TestTermAwareChunkScoreDownranksPHPBoilerplate(t *testing.T) {
	terms := meaningfulSearchTerms("oddly located mapped thing")
	header := Chunk{Path: "classmap/MappedThing.php", Language: "php", Content: "<?php\nnamespace Odd\\Location;\n"}
	declaration := Chunk{Path: "classmap/MappedThing.php", Language: "php", Content: "final class MappedThing {}\n"}
	if !isPHPHeaderOnlyChunk(header) || isPHPHeaderOnlyChunk(declaration) {
		t.Fatal("unexpected PHP header classification")
	}
	if got, want := termAwareChunkScore(declaration, terms), termAwareChunkScore(header, terms); got <= want {
		t.Fatalf("declaration score %d must exceed boilerplate score %d", got, want)
	}
}

func TestTermAwareChunkScorePrefersMatchingDeclarationOverReference(t *testing.T) {
	terms := meaningfulSearchTerms("where is the oddly located mapped thing")
	declaration := Chunk{Path: "classmap/MappedThing.php", Language: "php", Content: "final class MappedThing\n{\n}\n"}
	reference := Chunk{Path: "app/Consumer.php", Language: "php", Content: "public function dependencies(): array\n{\n    return [new MappedThing()];\n}\n"}
	if got, want := termAwareChunkScore(declaration, terms), termAwareChunkScore(reference, terms); got <= want {
		t.Fatalf("matching declaration score %d must exceed reference score %d", got, want)
	}
}

func TestPHPDeclarationEvidenceExcludesBodiesAndNonPHP(t *testing.T) {
	php := Chunk{Path: "Consumer.php", Language: "php", Content: "public function dependencies(): array { return [new MappedThing()]; }\n"}
	if quality := bestSearchTermQuality("mapped", phpDeclarationHeaderTokens(php)); quality != 0 {
		t.Fatalf("one-line PHP body leaked into declaration evidence: %d", quality)
	}
	nonPHP := Chunk{Path: "consumer.go", Language: "go", Content: "func dependencies() { _ = MappedThing{} }\n"}
	if tokens := phpDeclarationHeaderTokens(nonPHP); len(tokens) != 0 {
		t.Fatalf("non-PHP declaration evidence = %v, want none", tokens)
	}
}

func TestPHPDeclarationEvidenceIncludesAttributes(t *testing.T) {
	chunk := Chunk{Path: "src/Entity/AuditLog.php", Language: "php", Content: "#[ORM\\Entity(repositoryClass: AuditLogRepository::class)]\nfinal class AuditLog\n{\n}\n"}
	tokens := phpDeclarationHeaderTokens(chunk)
	for _, term := range []string{"entity", "repository", "class", "audit", "log"} {
		if quality := bestSearchTermQuality(term, tokens); quality == 0 {
			t.Errorf("attribute declaration tokens %v do not contain %q", tokens, term)
		}
	}
}

func TestTermAwareChunkScoreUsesStructuralIntent(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		target     Chunk
		distractor Chunk
	}{
		{
			name:       "attribute declaration",
			query:      "Where is the AuditTag attribute declared and restricted to methods?",
			target:     Chunk{Path: "src/Attribute/AuditTag.php", Language: "php", Content: "#[Attribute(Attribute::TARGET_METHOD)]\nfinal class AuditTag {}\n"},
			distractor: Chunk{Path: "src/Handler/AuditedHandler.php", Language: "php", Content: "#[AuditTag]\npublic function handle(): void {}\n"},
		},
		{
			name:       "first class callable",
			query:      "Where is normalize converted into a first-class callable?",
			target:     Chunk{Path: "src/Factory/NormalizerFactory.php", Language: "php", Content: "public function create(Normalizer $normalizer): callable\n{\n    return $normalizer->normalize(...);\n}\n"},
			distractor: Chunk{Path: "src/Support/Normalizer.php", Language: "php", Content: "public function normalize(string $value): string\n{\n    return trim($value);\n}\n"},
		},
		{
			name:       "deferred callable paraphrase",
			query:      "Where is formatLabel packaged up so it can be passed around and run later?",
			target:     Chunk{Path: "src/DeferredLabel.php", Language: "php", Content: "public function callback(): Closure\n{\n    return LabelFormatter::formatLabel(...);\n}\n"},
			distractor: Chunk{Path: "src/LabelFormatter.php", Language: "php", Content: "public static function formatLabel(string $value): string\n{\n    return trim($value);\n}\n"},
		},
		{
			name:       "config definition",
			query:      "Which configuration entry defines the reporting endpoint consumed by ReportExporter?",
			target:     Chunk{Path: "config/reporting.php", Language: "php", Content: "return ['endpoint' => env('REPORTING_ENDPOINT')];\n"},
			distractor: Chunk{Path: "app/Services/ReportExporter.php", Language: "php", Content: "final class ReportExporter { public function endpoint(): string { return config('reporting.endpoint'); } }\n"},
		},
		{
			name:       "entity repository mapping",
			query:      "Which entity mapping assigns the AuditLog repository class?",
			target:     Chunk{Path: "src/Entity/AuditLog.php", Language: "php", Content: "#[ORM\\Entity(repositoryClass: AuditLogRepository::class)]\nfinal class AuditLog {}\n"},
			distractor: Chunk{Path: "src/Service/AuditReader.php", Language: "php", Content: "final class AuditReader { public function __construct(private AuditLogRepository $repository) {} }\n"},
		},
		{
			name:       "parent collection relationship",
			query:      "Where does the data model state that one parent record is connected to a collection of dependent records?",
			target:     Chunk{Path: "app/Models/VaultRecord.php", Language: "php", Content: "public function fragments(): HasMany\n{\n    return $this->hasMany(FragmentRecord::class);\n}\n"},
			distractor: Chunk{Path: "app/Http/Controllers/VaultPresenter.php", Language: "php", Content: "public function present(VaultRecord $vault): array\n{\n    return $vault->fragments()->all();\n}\n"},
		},
		{
			name:       "never termination",
			query:      "Which method declares it never returns and terminates by throwing?",
			target:     Chunk{Path: "src/Command/AbortCommand.php", Language: "php", Content: "public function abort(): never\n{\n    throw new RuntimeException('aborted');\n}\n"},
			distractor: Chunk{Path: "src/Registry/MethodRegistry.php", Language: "php", Content: "public function method(string $name): ?string\n{\n    return null;\n}\n"},
		},
		{
			name:       "backed enum definition",
			query:      "Where are the allowed processing phases and their persisted string values defined?",
			target:     Chunk{Path: "src/Domain/GlintPhase.php", Language: "php", Content: "enum GlintPhase: string\n{\n    case Seeded = 'seeded';\n}\n"},
			distractor: Chunk{Path: "src/Application/GlintPhasePresenter.php", Language: "php", Content: "return match ($phase) { GlintPhase::Seeded => 'Queued' };\n"},
		},
		{
			name:       "backed enum canonical-code paraphrase",
			query:      "Where are the canonical string codes for every parcel review outcome declared?",
			target:     Chunk{Path: "src/Domain/ParcelVerdict.php", Language: "php", Content: "enum ParcelVerdict: string\n{\n    case Cleared = 'cleared';\n}\n"},
			distractor: Chunk{Path: "src/Presentation/VerdictPresenter.php", Language: "php", Content: "return match ($verdict) { ParcelVerdict::Cleared => 'Cleared' };\n"},
		},
		{
			name:       "serialized domain label paraphrase",
			query:      "Which source establishes the stable wire labels for every transit disposition?",
			target:     Chunk{Path: "src/Domain/TransitDisposition.php", Language: "php", Content: "enum TransitDisposition: string\n{\n    case AwaitingTransfer = 'awaiting_transfer';\n}\n"},
			distractor: Chunk{Path: "src/Api/DispositionSerializer.php", Language: "php", Content: "return match ($disposition) { TransitDisposition::AwaitingTransfer => 'Awaiting transfer' };\n"},
		},
		{
			name:       "durable catalog spelling paraphrase",
			query:      "Which source fixes the durable catalog spellings for each museum object's pest-treatment stage?",
			target:     Chunk{Path: "src/Domain/FumigationStage.php", Language: "php", Content: "enum FumigationStage: string\n{\n"},
			distractor: Chunk{Path: "src/Presentation/FumigationBrief.php", Language: "php", Content: "public function caption(FumigationStage $stage): string\n{\n    return match ($stage) {\n        FumigationStage::SealedForTreatment => 'Treatment chamber sealed',\n        FumigationStage::ExposureUnderway => 'Pest exposure underway',\n        FumigationStage::ClearedForStorage => 'Cleared for collection storage',\n    };\n}\n"},
		},
		{
			name:       "serialized catalog spelling every-domain paraphrase",
			query:      "Which declaration fixes the durable serialized catalog spelling for every seed-lot viability category?",
			target:     Chunk{Path: "src/Domain/SeedLotViability.php", Language: "php", Content: "enum SeedLotViability: string\n{\n"},
			distractor: Chunk{Path: "src/Api/ViabilityCatalogSerializer.php", Language: "php", Content: "final class ViabilityCatalogSerializer\n{\n    /** @return array{category: string, catalog_spelling: string} */\n    public function serialize(SeedLotViability $category): array\n    {\n        return ['category' => strtolower($category->name), 'catalog_spelling' => $category->value];\n    }\n}\n"},
		},
		{
			name:       "stored token domain paraphrase",
			query:      "Where is the authoritative closed set of stored tokens for every invoice delivery state established?",
			target:     Chunk{Path: "src/Domain/InvoiceDeliveryState.php", Language: "php", Content: "enum InvoiceDeliveryState: string\n{\n    case Queued = 'queued';\n}\n"},
			distractor: Chunk{Path: "src/Api/InvoiceDeliverySerializer.php", Language: "php", Content: "final class InvoiceDeliverySerializer\n{\n    public function serialize(InvoiceDeliveryState $state): array\n    {\n        return ['status' => $state->value, 'label' => match ($state) {\n            InvoiceDeliveryState::Queued => 'Queued for delivery',\n        }];\n    }\n}\n"},
		},
		{
			name:       "authoritative domain codes paraphrase",
			query:      "Where is the authoritative domain declaration that defines the allowed membership freeze cause codes?",
			target:     Chunk{Path: "src/Domain/MembershipFreezeCause.php", Language: "php", Content: "enum MembershipFreezeCause: string\n{\n    case MemberRequest = 'member_request';\n}\n"},
			distractor: Chunk{Path: "src/Presentation/MembershipFreezeCausePresenter.php", Language: "php", Content: "final class MembershipFreezeCausePresenter\n{\n    public function present(MembershipFreezeCause $cause): array\n    {\n        return ['code' => $cause->value, 'label' => match ($cause) {\n            MembershipFreezeCause::MemberRequest => 'Member request',\n        }];\n    }\n}\n"},
		},
		{
			name:       "shutdown callback registration",
			query:      "Which implementation installs the callback that appends the final marker after script termination, including an early exit?",
			target:     Chunk{Path: "src/TerminalPulse.php", Language: "php", Content: "register_shutdown_function(static function (): void { appendFinalMarker(); });\n"},
			distractor: Chunk{Path: "bin/worker.php", Language: "php", Content: "appendFinalPulse($path);\nif ($halt) { exit(17); }\n"},
		},
		{
			name:       "shutdown attachment paraphrase",
			query:      "Where is the last-chance drain callback attached so records flush when the PHP process terminates?",
			target:     Chunk{Path: "src/FinalizationHooks.php", Language: "php", Content: "register_shutdown_function(static function (): void { drainPending(); });\n"},
			distractor: Chunk{Path: "bin/export.php", Language: "php", Content: "FinalizationHooks::install($drainer);\nif ($abort) { exit(2); }\n"},
		},
		{
			name:       "WordPress uninstall registration",
			query:      "Which registration makes cleanup occur when the plugin is deleted rather than merely switched off?",
			target:     Chunk{Path: "plugin.php", Language: "php", Content: "register_uninstall_hook(__FILE__, 'purgePlugin');\n"},
			distractor: Chunk{Path: "src/Deactivation.php", Language: "php", Content: "register_deactivation_hook(__FILE__, 'pausePlugin');\n"},
		},
		{
			name:       "WordPress uninstall binding paraphrase",
			query:      "Where does the plugin bind its permanent data purge to the moment an administrator deletes the plugin?",
			target:     Chunk{Path: "plugin.php", Language: "php", Content: "register_uninstall_hook(__FILE__, [OrchardPurge::class, 'eraseAll']);\n"},
			distractor: Chunk{Path: "src/OrchardPurge.php", Language: "php", Content: "public static function eraseAll(): void { delete_option('orchard'); }\n"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			intent := classifyTermSearchIntent(test.query)
			terms := meaningfulSearchTermsForIntent(test.query, intent)
			got := termAwareChunkScoreWithIntent(test.target, terms, intent)
			want := termAwareChunkScoreWithIntent(test.distractor, terms, intent)
			if got <= want {
				t.Fatalf("target score %d must exceed distractor score %d", got, want)
			}
		})
	}
}

func TestTermsV14PromotesPostTermsV13Evidence(t *testing.T) {
	query := "Where is the authoritative domain declaration that defines the allowed membership freeze cause codes?"
	intent := classifyTermSearchIntent(query)
	if !intent.backedEnumDefinition || !intent.preferRelationshipTarget {
		t.Fatalf("promoted terms-v13 evidence did not activate backed-enum intent: %#v", intent)
	}
	terms := meaningfulSearchTermsForIntent(query, intent)
	enum := Chunk{
		Path:     "holdout_20260724_v13/Membership/MembershipFreezeCause.php",
		Language: "php",
		Content:  "enum MembershipFreezeCause: string\n{\n    case MemberRequest = 'member_request';\n}\n",
	}
	presenter := Chunk{
		Path:     "holdout_20260724_v13/Membership/MembershipFreezeCausePresenter.php",
		Language: "php",
		Content:  "final class MembershipFreezeCausePresenter\n{\n    public function present(MembershipFreezeCause $cause): array\n    {\n        return ['code' => $cause->value];\n    }\n}\n",
	}
	enumScore := termAwareChunkScoreWithIntent(enum, terms, intent)
	presenterScore := termAwareChunkScoreWithIntent(presenter, terms, intent)
	if enumScore <= presenterScore {
		t.Fatalf("promoted enum score %d must exceed presenter score %d", enumScore, presenterScore)
	}
}

func TestTermsV15PromotesPostTermsV14Evidence(t *testing.T) {
	compatRoot := filepath.Join("..", "..", "evaluation", "php-compat")
	manifestData, err := os.ReadFile(filepath.Join(compatRoot, "suite.v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Corpora []struct {
			ID        string `json:"id"`
			Root      string `json:"root"`
			Retrieval []struct {
				ID       string `json:"id"`
				Query    string `json:"query"`
				Relevant []struct {
					Path string `json:"path"`
				} `json:"relevant"`
				HardNegatives []struct {
					Path string `json:"path"`
				} `json:"hardNegatives"`
			} `json:"retrieval"`
		} `json:"corpora"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	var corpusRoot, query, enumPath, presenterPath string
	for _, corpus := range manifest.Corpora {
		for _, retrieval := range corpus.Retrieval {
			if retrieval.ID != "postv14-php81-cold-chain-disposition-authority" {
				continue
			}
			if len(retrieval.Relevant) != 1 || len(retrieval.HardNegatives) != 1 {
				t.Fatalf("post-freeze judgment has relevant=%d hardNegatives=%d; want 1 each", len(retrieval.Relevant), len(retrieval.HardNegatives))
			}
			corpusRoot = corpus.Root
			query = retrieval.Query
			enumPath = retrieval.Relevant[0].Path
			presenterPath = retrieval.HardNegatives[0].Path
		}
	}
	if corpusRoot == "" || query == "" || enumPath == "" || presenterPath == "" {
		t.Fatal("post-freeze judgment is missing from suite.v2.json")
	}
	intent := classifyTermSearchIntent(query)
	if !intent.backedEnumDefinition || !intent.authoritativeDomainDefinition || !intent.preferRelationshipTarget {
		t.Fatalf("promoted terms-v14 evidence did not activate authoritative-domain intent: %#v", intent)
	}
	root := t.TempDir()
	for _, path := range []string{enumPath, presenterPath} {
		content, err := os.ReadFile(filepath.Join(compatRoot, filepath.FromSlash(corpusRoot), filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		destination := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := New(Config{RootAbs: root, StoreDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.indexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	chunks, err := idx.SearchTermsByPathContext(context.Background(), query, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 || chunks[0].Path != enumPath || chunks[1].Path != presenterPath {
		t.Fatalf("promoted ranking = %#v; want enum first and presenter second", chunks)
	}
}

func TestTermsV15PostFreezeHoldoutRemainsAdvisory(t *testing.T) {
	compatRoot := filepath.Join("..", "..", "evaluation", "php-compat")
	manifestData, err := os.ReadFile(filepath.Join(compatRoot, "suite.v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Corpora []struct {
			Root      string `json:"root"`
			Retrieval []struct {
				ID       string `json:"id"`
				Query    string `json:"query"`
				Relevant []struct {
					Path string `json:"path"`
				} `json:"relevant"`
				HardNegatives []struct {
					Path string `json:"path"`
				} `json:"hardNegatives"`
			} `json:"retrieval"`
		} `json:"corpora"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	var corpusRoot, query, enumPath, runbookPath string
	for _, corpus := range manifest.Corpora {
		for _, retrieval := range corpus.Retrieval {
			if retrieval.ID != "postv15-php81-orchard-frost-response-authority" {
				continue
			}
			if len(retrieval.Relevant) != 1 || len(retrieval.HardNegatives) != 1 {
				t.Fatalf("post-freeze judgment has relevant=%d hardNegatives=%d; want 1 each", len(retrieval.Relevant), len(retrieval.HardNegatives))
			}
			corpusRoot = corpus.Root
			query = retrieval.Query
			enumPath = retrieval.Relevant[0].Path
			runbookPath = retrieval.HardNegatives[0].Path
		}
	}
	if corpusRoot == "" || query == "" || enumPath == "" || runbookPath == "" {
		t.Fatal("post-freeze judgment is missing from suite.v2.json")
	}
	intent := classifyTermSearchIntent(query)
	if intent.backedEnumDefinition || intent.preferRelationshipTarget {
		t.Fatalf("post-freeze holdout unexpectedly activated backed-enum intent: %#v", intent)
	}
	root, err := filepath.Abs(filepath.Join(compatRoot, filepath.FromSlash(corpusRoot)))
	if err != nil {
		t.Fatal(err)
	}
	idx, err := New(Config{RootAbs: root, StoreDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.indexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	chunks, err := idx.SearchTermsByPathContext(context.Background(), query, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		runbookPath,
		"holdout_20260713/orchard/OrchardEndpoint.php",
		enumPath,
	}
	if len(chunks) < len(want) {
		t.Fatalf("post-freeze ranking returned %d chunks; want at least %d: %#v", len(chunks), len(want), chunks)
	}
	for rank, path := range want {
		if chunks[rank].Path != path {
			t.Fatalf("post-freeze rank %d = %q; want %q until a new scorer fingerprint", rank+1, chunks[rank].Path, path)
		}
	}
}

func TestSearchTermsRanksIndependentStructuralFixtures(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"src/Attribute/AuditTag.php":        "<?php\nnamespace App\\Attribute;\nuse Attribute;\n#[Attribute(Attribute::TARGET_METHOD)]\nfinal class AuditTag {}\n",
		"src/Handler/AuditedHandler.php":    "<?php\nnamespace App\\Handler;\nuse App\\Attribute\\AuditTag;\n#[AuditTag]\nfinal class AuditedHandler { public function handle(): void {} }\n",
		"src/Factory/NormalizerFactory.php": "<?php\nfinal class NormalizerFactory { public function create(Normalizer $normalizer): callable { return $normalizer->normalize(...); } }\n",
		"src/Support/Normalizer.php":        "<?php\nfinal class Normalizer { public function normalize(string $value): string { return trim($value); } }\n",
		"config/reporting.php":              "<?php\nreturn ['endpoint' => env('REPORTING_ENDPOINT')];\n",
		"app/Services/ReportExporter.php":   "<?php\nfinal class ReportExporter { public function endpoint(): string { return config('reporting.endpoint'); } }\n",
		"src/Entity/AuditLog.php":           "<?php\nuse App\\Repository\\AuditLogRepository;\n#[ORM\\Entity(repositoryClass: AuditLogRepository::class)]\nfinal class AuditLog {}\n",
		"src/Service/AuditReader.php":       "<?php\nfinal class AuditReader { public function __construct(private AuditLogRepository $repository) {} }\n",
	}
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := New(Config{RootAbs: root, StoreDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.indexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	queries := map[string]string{
		"Where is the AuditTag attribute declared and restricted to methods?":                  "src/Attribute/AuditTag.php",
		"Where is an object's normalize method converted into a first-class callable?":         "src/Factory/NormalizerFactory.php",
		"Which configuration entry defines the reporting endpoint consumed by ReportExporter?": "config/reporting.php",
		"Which entity mapping assigns the AuditLog repository class?":                          "src/Entity/AuditLog.php",
	}
	for query, want := range queries {
		chunks, err := idx.SearchTermsByPathContext(context.Background(), query, 5, nil)
		if err != nil {
			t.Fatalf("%q: %v", query, err)
		}
		if len(chunks) == 0 || chunks[0].Path != want {
			t.Errorf("%q: rank one = %#v; want %s", query, chunks, want)
		}
	}
}

func TestTermAwareChunkScoreKeepsTargetedPHPImportsDiscoverable(t *testing.T) {
	terms := meaningfulSearchTerms("where is MappedThing imported with use")
	declaration := Chunk{Path: "app/Consumer.php", Language: "php", Content: "use Odd\\Location\\MappedThing;\n"}
	reference := Chunk{Path: "app/Factory.php", Language: "php", Content: "public function create(): object\n{\n    return new MappedThing();\n}\n"}
	if got, want := termAwareChunkScore(declaration, terms), termAwareChunkScore(reference, terms); got <= want {
		t.Fatalf("targeted import score %d must exceed reference score %d", got, want)
	}
}

func TestSearchTermsFindsNaturalLanguageWithoutChangingExactSearch(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"src/Controller/ReportController.php": "<?php final class ReportController { public function summary(): array { return ['#theme' => 'report_summary']; } }\n",
		"src/Service/ReportRepository.php":    "<?php final class ReportRepository { public function recent(): array { return []; } }\n",
		"README.md":                           "A report repository stores report records.\n",
	}
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := New(Config{RootAbs: root, StoreDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.indexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	query := "where is the report summary handled"
	exact, err := idx.SearchContext(context.Background(), query, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(exact) != 0 {
		t.Fatalf("exact search contract changed: %#v", exact)
	}
	terms, err := idx.SearchTermsContext(context.Background(), query, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(terms) == 0 || terms[0].Path != "src/Controller/ReportController.php" {
		t.Fatalf("unexpected term-aware ranking: %#v", terms)
	}
}

func TestSearchTermsPrefersMatchingPHPDeclarationChunk(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"classmap/MappedThing.php": "<?php\n\ndeclare(strict_types=1);\n\nnamespace Odd\\Location;\n\nfinal class MappedThing\n{\n}\n",
		"app/Consumer.php":         "<?php\n\nnamespace Fixture\\App;\n\nuse Odd\\Location\\MappedThing;\n\nfinal class Consumer\n{\n    public function dependencies(): array\n    {\n        return [new MappedThing()];\n    }\n}\n",
	}
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := New(Config{RootAbs: root, StoreDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.indexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	chunks, err := idx.SearchTermsByPathContext(context.Background(), "where is the oddly located mapped thing", 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 || chunks[0].Path != "classmap/MappedThing.php" || chunks[0].StartLine != 7 || chunks[0].EndLine != 9 {
		t.Fatalf("expected the mapped declaration at rank one, got %#v", chunks)
	}
}

func TestSearchTermsRejectsStopWordsOnly(t *testing.T) {
	idx, err := New(Config{RootAbs: t.TempDir(), StoreDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := idx.SearchTermsContext(context.Background(), "where is the", 5, nil); err == nil {
		t.Fatal("expected stop-word-only query to fail closed")
	}
}

func TestSearchTermsPrefilterKeepsCanonicalIrregularMatch(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Bindings.php"), []byte("<?php // The repository is bound here.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := New(Config{RootAbs: root, StoreDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.indexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	chunks, err := idx.SearchTermsContext(context.Background(), "where is the repository binding", 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 || chunks[0].Path != "Bindings.php" {
		t.Fatalf("canonical match was lost by the trigram prefilter: %#v", chunks)
	}
}

func TestSearchTermsByPathIsStableAndDistinct(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"b/ReportHandler.php", "a/ReportHandler.php"} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		content := "<?php\nfinal class ReportHandlerOne { public function handle(): void {} }\nfinal class ReportHandlerTwo { public function handle(): void {} }\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := New(Config{RootAbs: root, StoreDir: t.TempDir(), MaxChunkLines: 1, MaxChunkBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.indexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	allChunks, err := idx.SearchTermsContext(context.Background(), "where is a report handled", 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(allChunks) < 4 || allChunks[0].Path != allChunks[1].Path {
		t.Fatalf("fixture did not produce multiple ranked chunks per path: %#v", allChunks)
	}

	var want []string
	for run := 0; run < 2; run++ {
		chunks, err := idx.SearchTermsByPathContext(context.Background(), "where is a report handled", 5, nil)
		if err != nil {
			t.Fatal(err)
		}
		got := make([]string, 0, len(chunks))
		seen := map[string]bool{}
		for _, chunk := range chunks {
			if seen[chunk.Path] {
				t.Fatalf("duplicate path in distinct results: %#v", chunks)
			}
			seen[chunk.Path] = true
			got = append(got, chunk.Path)
		}
		if run == 0 {
			want = got
		} else if !reflect.DeepEqual(got, want) {
			t.Fatalf("unstable result order: first=%v second=%v", want, got)
		}
	}
	if !reflect.DeepEqual(want, []string{"a/ReportHandler.php", "b/ReportHandler.php"}) {
		t.Fatalf("unexpected tie order: %v", want)
	}
}
