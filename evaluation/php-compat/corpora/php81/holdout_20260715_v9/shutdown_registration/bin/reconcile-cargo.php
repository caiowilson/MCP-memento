<?php

declare(strict_types=1);

use HarborTelemetry\Runtime\CargoTelemetry;
use HarborTelemetry\Runtime\ExitFlush;

require dirname(__DIR__) . '/vendor/autoload.php';

$telemetry = new CargoTelemetry();
ExitFlush::connect($telemetry);

if (($argv[1] ?? '') === '--cancel') {
    $telemetry->queue('cargo-reconciliation-cancelled');
    exit(3);
}
