<?php

declare(strict_types=1);

namespace OpalHarbor\Maintenance;

final class HarborEraser
{
    public static function erasePermanentData(): void
    {
        delete_option('opal_harbor_registry_version');
        delete_option('opal_harbor_last_manifest');
        wp_clear_scheduled_hook('opal_harbor_manifest_refresh');
    }
}
