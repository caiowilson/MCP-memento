<?php

declare(strict_types=1);

namespace CopperLedger\Runtime;

final class FinalizationHooks
{
    public static function install(SpoolDrainer $drainer): void
    {
        register_shutdown_function(
            static function () use ($drainer): void {
                $drainer->drainPending();
            },
        );
    }
}
