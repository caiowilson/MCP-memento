<?php

declare(strict_types=1);

final class Report_Plugin
{
    public function __construct(private Report_Repository $reports)
    {
    }

    public static function boot(): void
    {
        add_action('init', 'memento_register_report_type');
        add_shortcode('memento_reports', 'memento_render_reports');
    }
}
