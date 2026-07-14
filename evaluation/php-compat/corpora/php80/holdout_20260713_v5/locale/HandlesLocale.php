<?php

declare(strict_types=1);

namespace RetrievalHoldout\Locale;

use Attribute;

#[Attribute(Attribute::TARGET_METHOD)]
final class HandlesLocale
{
    public function __construct(public string $code)
    {
    }
}
