<?php

declare(strict_types=1);

namespace FieldOps\ColdChain\Domain;

enum ExcursionDisposition: string
{
    case Quarantine = 'quarantine';
    case Release = 'release';
    case Destroy = 'destroy';
    case ReturnToSupplier = 'return_to_supplier';

    public function requiresSupervisorApproval(): bool
    {
        return match ($this) {
            self::Release,
            self::Destroy => true,
            self::Quarantine,
            self::ReturnToSupplier => false,
        };
    }
}
