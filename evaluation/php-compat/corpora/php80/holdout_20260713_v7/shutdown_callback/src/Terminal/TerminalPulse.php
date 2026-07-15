<?php
declare(strict_types=1);

namespace VirelliumK9\Terminal;

function appendFinalPulse(string $path): void
{
    register_shutdown_function(
        static function () use ($path): void {
            file_put_contents($path, "final\n", FILE_APPEND);
        }
    );
}
