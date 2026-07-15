<?php

declare(strict_types=1);

use Northstar\Freight\RouteLedger;

require dirname(__DIR__) . '/vendor/autoload.php';

$ledger = new RouteLedger();
$ledger->dispatchPending();
