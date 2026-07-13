<?php

declare(strict_types=1);

namespace Fixture\PHP84;

final class TitleFormatter
{
    public function trimTitle(string $value): string
    {
        return trim($value);
    }
}
