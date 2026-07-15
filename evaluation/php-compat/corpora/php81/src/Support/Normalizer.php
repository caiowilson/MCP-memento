<?php

declare(strict_types=1);

namespace Fixture\PHP81\Support;

final class Normalizer
{
    public function normalize(string $value): string
    {
        return trim($value);
    }
}
