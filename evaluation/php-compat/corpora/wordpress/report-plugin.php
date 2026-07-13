<?php
/**
 * Plugin Name: Memento Report Fixture
 */

declare(strict_types=1);

require_once __DIR__ . '/includes/class-report-repository.php';
require_once __DIR__ . '/includes/class-report-plugin.php';
require_once __DIR__ . '/includes/functions.php';

add_action('plugins_loaded', [Report_Plugin::class, 'boot']);
