<?php

declare(strict_types=1);

/**
 * Plugin Name: Opal Harbor Registry
 */

use OpalHarbor\Maintenance\HarborEraser;

register_uninstall_hook(
    __FILE__,
    [HarborEraser::class, 'erasePermanentData'],
);
