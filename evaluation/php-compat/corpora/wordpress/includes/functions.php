<?php

declare(strict_types=1);

function memento_register_report_type(): void
{
    register_post_type('report', ['public' => true]);
}

function memento_render_reports(): string
{
    return '<div class="reports"></div>';
}
