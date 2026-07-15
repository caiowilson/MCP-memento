<?php
declare(strict_types=1);

namespace VirelliumK9\EmberLedger;

register_deactivation_hook(
    dirname(__DIR__) . '/virellium-k9-ember-ledger.php',
    __NAMESPACE__ . '\\pauseEmberLedger'
);

function pauseEmberLedger(): void
{
    wp_clear_scheduled_hook('virellium_k9_ember_tick');
}
