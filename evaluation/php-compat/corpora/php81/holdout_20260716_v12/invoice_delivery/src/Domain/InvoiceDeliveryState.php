<?php

declare(strict_types=1);

namespace NorthstarBilling\Domain;

enum InvoiceDeliveryState: string
{
    case Queued = 'queued';
    case Dispatched = 'dispatched';
    case Delivered = 'delivered';
    case Bounced = 'bounced';
}
