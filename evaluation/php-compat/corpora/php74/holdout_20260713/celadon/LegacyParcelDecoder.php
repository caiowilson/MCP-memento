<?php
declare(strict_types=1);

namespace Holdout\Php74\Celadon;

final class LegacyParcelDecoder
{
    /** @param array<string, mixed> $row */
    public function decode(array $row): VoucherSeal
    {
        return new VoucherSeal((string) $row['seal']);
    }
}
