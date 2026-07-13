<?php

declare(strict_types=1);

namespace Fixture\PHP83;

class BaseFormatter
{
    public function format(string $value): string
    {
        return $value;
    }
}

final class LanguageFeatures extends BaseFormatter
{
    public const string PREFIX = 'report';
    private const int RETRIES = 3;

    #[\Override]
    public function format(string $value): string
    {
        return self::PREFIX . ':' . $value;
    }

    public static function constant(string $name): mixed
    {
        return self::{$name};
    }
}
