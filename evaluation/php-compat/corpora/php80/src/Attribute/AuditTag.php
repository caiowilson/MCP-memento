<?php

declare(strict_types=1);

namespace Fixture\PHP80\Attribute;

use Attribute;

#[Attribute(Attribute::TARGET_METHOD)]
final class AuditTag
{
    public function __construct(public string $category)
    {
    }
}
