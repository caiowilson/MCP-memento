<?php
declare(strict_types=1);

namespace Holdout\Php74\Celadon;

/**
 * Immutable identifier carried on sealed parcels.
 *
 * @deprecated Use ParcelKey for newly issued parcels.
 */
final class VoucherSeal
{
    /** @var string */
    private $value;

    public function __construct(string $value)
    {
        $this->value = $value;
    }

    public function value(): string
    {
        return $this->value;
    }
}
