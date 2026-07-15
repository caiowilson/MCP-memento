<?php

declare(strict_types=1);

namespace RetrievalHoldout\Units;

require_once __DIR__ . '/base_unit.php';

final class SegmentSizer
{
    public function doubled(): int
    {
        return BASE_UNIT * 2;
    }
}
