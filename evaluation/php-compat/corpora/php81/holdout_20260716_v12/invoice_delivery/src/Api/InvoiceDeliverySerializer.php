<?php

declare(strict_types=1);

namespace NorthstarBilling\Api;

use NorthstarBilling\Domain\InvoiceDeliveryState;

final class InvoiceDeliverySerializer
{
    /** @return array{status: string, label: string} */
    public function serialize(InvoiceDeliveryState $state): array
    {
        return [
            'status' => $state->value,
            'label' => match ($state) {
                InvoiceDeliveryState::Queued => 'Queued for delivery',
                InvoiceDeliveryState::Dispatched => 'Sent to recipient',
                InvoiceDeliveryState::Delivered => 'Delivered',
                InvoiceDeliveryState::Bounced => 'Delivery bounced',
            },
        ];
    }
}
