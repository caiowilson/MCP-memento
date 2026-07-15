<?php

declare(strict_types=1);

namespace NorthstarTransit\Api;

use NorthstarTransit\Domain\TransitDisposition;

final class DispositionSerializer
{
    public function caption(TransitDisposition $disposition): string
    {
        return match ($disposition) {
            TransitDisposition::AwaitingTransfer => 'Awaiting transfer',
            TransitDisposition::ClearedForRoute => 'Cleared for route',
            TransitDisposition::ManualInspection => 'Manual inspection',
        };
    }
}
