<?php

declare(strict_types=1);

namespace Fixture\PHP83;

final class ReportFormatter
{
    public function prefix(string $report): string
    {
        return $report;
    }
}
