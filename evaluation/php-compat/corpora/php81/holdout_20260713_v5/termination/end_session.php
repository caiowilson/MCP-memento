<?php

declare(strict_types=1);

namespace RetrievalHoldout\Termination;

function endSession(string $reason): never
{
    fwrite(STDERR, $reason . PHP_EOL);
    exit(1);
}
