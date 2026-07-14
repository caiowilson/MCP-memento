<?php

declare(strict_types=1);

use CopperLedger\Runtime\FinalizationHooks;
use CopperLedger\Runtime\SpoolDrainer;

require dirname(__DIR__) . '/vendor/autoload.php';

$drainer = new SpoolDrainer();
FinalizationHooks::install($drainer);

if (($argv[1] ?? '') === '--abort') {
    $drainer->queue('export-aborted');
    exit(2);
}
