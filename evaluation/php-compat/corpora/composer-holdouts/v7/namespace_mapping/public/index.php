<?php
declare(strict_types=1);

use VirelliumK9\Aurora\Beacon;

require dirname(__DIR__) . '/vendor/autoload.php';

echo (new Beacon())->shine();
