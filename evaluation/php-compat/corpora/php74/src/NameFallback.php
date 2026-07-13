<?php

declare(strict_types=1);

namespace Fixture\PHP74;

final class NameFallback
{
    public function fallbackName(string $name): string
    {
        return $name;
    }
}
