<?php

declare(strict_types=1);

use GlassKite\Beacon\ConsoleKernel;

require dirname(__DIR__) . '/vendor/autoload.php';

$kernel = new ConsoleKernel();
$kernel->run();
