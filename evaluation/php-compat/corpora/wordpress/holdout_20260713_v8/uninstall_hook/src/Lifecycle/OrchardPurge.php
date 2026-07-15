<?php

declare(strict_types=1);

namespace MicaOrchard\Lifecycle;

final class OrchardPurge
{
    public static function eraseAll(): void
    {
        delete_option('mica_orchard_schema');
        delete_option('mica_orchard_cursor');
        wp_clear_scheduled_hook('mica_orchard_daily_prune');
    }
}
