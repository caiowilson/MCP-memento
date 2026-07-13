package mcp

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestSymfonyAndTwigTemplateReferences(t *testing.T) {
	root := t.TempDir()
	writePHPFrameworkFixture(t, root, "templates/base.html.twig", "base")
	writePHPFrameworkFixture(t, root, "templates/report/index.html.twig", "index")
	writePHPFrameworkFixture(t, root, "templates/report/_summary.html.twig", "summary")

	php := []byte(`<?php
// $this->render('ignored/comment.html.twig');
$example = "$this->render('ignored/string.html.twig')";
return $this
    ->render('report/index.html.twig', ['reports' => $reports]);
$this->render('../outside.html.twig');
`)
	if got, want := symfonyPHPTemplateReferences(root, "src/Controller/ReportController.php", php), []string{"templates/report/index.html.twig"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Symfony references = %#v, want %#v", got, want)
	}

	twig := []byte(`{% extends 'base.html.twig' %}
{# {% include 'ignored/comment.html.twig' %} #}
{% include "report/_summary.html.twig" %}
{% include '@Vendor/package.html.twig' %}
`)
	if got, want := twigTemplateReferences(root, "templates/report/index.html.twig", twig), []string{"templates/base.html.twig", "templates/report/_summary.html.twig"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Twig references = %#v, want %#v", got, want)
	}
}

func TestYAMLPHPClassReferencesUsesBoundedResolver(t *testing.T) {
	root := t.TempDir()
	writePHPFrameworkFixture(t, root, "src/Controller/ReportController.php", "<?php")
	writePHPFrameworkFixture(t, root, "src/Repository/ReportRepository.php", "<?php")
	writePHPFrameworkFixture(t, root, "src/Service/ReportService.php", "<?php")

	outside := filepath.Join(t.TempDir(), "Outside.php")
	if err := os.WriteFile(outside, []byte("<?php"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escaped.php")); err != nil {
		t.Fatal(err)
	}

	classes := map[string]string{
		"App\\Controller\\ReportController": "src/Controller/ReportController.php",
		"App\\Repository\\ReportRepository": "src/Repository/ReportRepository.php",
		"App\\Service\\ReportService":       "src/Service/ReportService.php",
		"App\\Escaped":                      "escaped.php",
	}
	seen := []string{}
	resolve := func(class string) string {
		seen = append(seen, class)
		return classes[class]
	}
	source := []byte(`services:
  App\Service\ReportService:
    arguments: ['@App\Repository\ReportRepository']
route:
  controller: '\App\Controller\ReportController::index'
  escaped: App\Escaped
  # controller: App\Ignored\CommentedController::index
`)
	got := yamlPHPClassReferences(root, source, resolve)
	want := []string{
		"src/Controller/ReportController.php",
		"src/Repository/ReportRepository.php",
		"src/Service/ReportService.php",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("YAML references = %#v, want %#v", got, want)
	}
	sort.Strings(seen)
	if containsPHPFrameworkString(seen, "App\\Ignored\\CommentedController") {
		t.Fatalf("commented class was sent to resolver: %#v", seen)
	}
}

func TestDrupalTemplateReferences(t *testing.T) {
	root := t.TempDir()
	writePHPFrameworkFixture(t, root, "web/modules/custom/report/templates/report-summary.html.twig", "summary")
	writePHPFrameworkFixture(t, root, "web/modules/custom/report/templates/comment-fake.html.twig", "fake")

	controller := []byte(`<?php
// return ['#theme' => 'comment_fake'];
$example = "'#theme' => 'comment_fake'";
return ['#theme' => 'report_summary'];
`)
	from := "web/modules/custom/report/src/Controller/ReportController.php"
	if got, want := drupalPHPTemplateReferences(root, from, controller), []string{"web/modules/custom/report/templates/report-summary.html.twig"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Drupal render-array references = %#v, want %#v", got, want)
	}

	hook := []byte(`<?php
return [
    'report_summary' => ['template' => 'report-summary'],
];
`)
	if got, want := drupalPHPTemplateReferences(root, "web/modules/custom/report/report.theme", hook), []string{"web/modules/custom/report/templates/report-summary.html.twig"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Drupal hook references = %#v, want %#v", got, want)
	}
}

func TestLaravelBladeTemplateReferences(t *testing.T) {
	root := t.TempDir()
	prefix := "apps/report/resources/views/"
	writePHPFrameworkFixture(t, root, prefix+"layouts/app.blade.php", "layout")
	writePHPFrameworkFixture(t, root, prefix+"dashboard/partials/stats.blade.php", "stats")
	writePHPFrameworkFixture(t, root, prefix+"components/alert.blade.php", "alert")
	writePHPFrameworkFixture(t, root, prefix+"components/forms/input.blade.php", "input")
	writePHPFrameworkFixture(t, root, prefix+"components/comment-fake.blade.php", "fake")

	source := []byte(`@extends('layouts.app')
@include("dashboard.partials.stats")
<x-alert />
<x-forms.input></x-forms.input>
{{-- <x-comment-fake /> --}}
<!-- <x-comment-fake /> -->
<x-vendor::button />
<x-dynamic-component :component="$name" />
`)
	got := laravelBladeTemplateReferences(root, prefix+"dashboard/index.blade.php", source)
	want := []string{
		prefix + "components/alert.blade.php",
		prefix + "components/forms/input.blade.php",
		prefix + "dashboard/partials/stats.blade.php",
		prefix + "layouts/app.blade.php",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Blade references = %#v, want %#v", got, want)
	}
}

func TestWordPressTemplatePartsAndHookCallbacks(t *testing.T) {
	root := t.TempDir()
	writePHPFrameworkFixture(t, root, "site/wp-content/themes/report/template-parts/content-report.php", "specialized")
	writePHPFrameworkFixture(t, root, "site/wp-content/themes/report/template-parts/content.php", "fallback")
	writePHPFrameworkFixture(t, root, "site/wp-content/themes/report/template-parts/comment-fake.php", "fake")

	templateSource := []byte(`<?php
// get_template_part('template-parts/comment-fake');
$example = "get_template_part('template-parts/comment-fake')";
get_template_part('template-parts/content', 'report');
get_template_part('../outside');
`)
	from := "site/wp-content/themes/report/single-report.php"
	wantParts := []string{
		"site/wp-content/themes/report/template-parts/content-report.php",
		"site/wp-content/themes/report/template-parts/content.php",
	}
	if got := wordpressTemplatePartReferences(root, from, templateSource); !reflect.DeepEqual(got, wantParts) {
		t.Fatalf("WordPress template parts = %#v, want %#v", got, wantParts)
	}

	hookSource := []byte(`<?php
// add_action('ignored', 'ignored_callback');
$example = "add_action('ignored_string', 'ignored_callback')";
add_action('plugins_loaded', [Report_Plugin::class, 'boot']);
add_action('init', 'memento_register_report_type');
add_filter('the_title', array('Report_Filter', 'title'));
add_shortcode('memento_reports', 'memento_render_reports');
add_action('dynamic', $callback);
`)
	wantCallbacks := []wordpressHookCallback{
		{Registration: "add_action", Hook: "init", Callback: "memento_register_report_type"},
		{Registration: "add_action", Hook: "plugins_loaded", Receiver: "Report_Plugin", Callback: "boot"},
		{Registration: "add_filter", Hook: "the_title", Receiver: "Report_Filter", Callback: "title"},
		{Registration: "add_shortcode", Hook: "memento_reports", Callback: "memento_render_reports"},
	}
	if got := wordpressHookCallbacks(hookSource); !reflect.DeepEqual(got, wantCallbacks) {
		t.Fatalf("WordPress callbacks = %#v, want %#v", got, wantCallbacks)
	}
}

func writePHPFrameworkFixture(t *testing.T, root, rel, contents string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func containsPHPFrameworkString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
