<?php

declare(strict_types=1);

require_once dirname(__DIR__) . '/includes/functions.php';

add_action('after_setup_theme', 'memento_register_report_type');
