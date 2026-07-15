<?php
/**
 * Plugin Name: Virellium K9 Ember Ledger
 */
declare(strict_types=1);

namespace VirelliumK9\EmberLedger;

register_uninstall_hook(__FILE__, __NAMESPACE__ . '\\purgeEmberLedger');

function purgeEmberLedger(): void
{
    delete_option('virellium_k9_ember_ledger');
}
