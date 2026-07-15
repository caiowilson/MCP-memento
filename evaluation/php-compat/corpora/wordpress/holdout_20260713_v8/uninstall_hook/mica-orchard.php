<?php

declare(strict_types=1);

/**
 * Plugin Name: Mica Orchard Ledger
 */

use MicaOrchard\Lifecycle\OrchardPurge;

register_uninstall_hook(
    __FILE__,
    [OrchardPurge::class, 'eraseAll'],
);
