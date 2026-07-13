<?php

declare(strict_types=1);

namespace Fixture\PHP74;

use Closure;

interface Producer
{
    public function produce(): object;
}

trait NormalizesNames
{
    private function normalizeName(string $value): string
    {
        return trim($value);
    }
}

final class LanguageFeatures implements Producer
{
    use NormalizesNames;

    private string $name;
    private ?Closure $afterNormalize = null;

    public function __construct(string $name)
    {
        $this->name = $name;
    }

    public function produce(): LanguageFeatures
    {
        return $this;
    }

    public function label(?string $override): string
    {
        $override ??= $this->name;
        $normalize = fn (string $value): string => $this->normalizeName($value);

        return $normalize($override);
    }
}

function php74_amount(): float
{
    return 1_234.50;
}
