<?php

declare(strict_types=1);

namespace HarborTelemetry\Runtime;

final class ExitFlush
{
    public static function connect(CargoTelemetry $telemetry): void
    {
        register_shutdown_function(
            static function () use ($telemetry): void {
                $telemetry->commitQueued();
            },
        );
    }
}
