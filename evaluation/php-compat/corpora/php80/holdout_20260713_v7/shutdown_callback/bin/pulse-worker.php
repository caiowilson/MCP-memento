<?php
declare(strict_types=1);

use function VirelliumK9\Terminal\appendFinalPulse;

require __DIR__ . '/../src/Terminal/TerminalPulse.php';

appendFinalPulse(__DIR__ . '/../var/pulse.log');

if (($argv[1] ?? '') === 'halt') {
    exit(17);
}

echo "running\n";
