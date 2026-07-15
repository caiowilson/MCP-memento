<?php

declare(strict_types=1);

namespace NorthstarTransit\Domain;

enum TransitDisposition: string
{
    case AwaitingTransfer = 'awaiting_transfer';
    case ClearedForRoute = 'cleared_for_route';
    case ManualInspection = 'manual_inspection';
}
