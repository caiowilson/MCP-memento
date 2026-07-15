<?php

declare(strict_types=1);

namespace App\Services;

final class ReportExporter
{
    public function endpoint(): string
    {
        return config('reporting.endpoint');
    }
}
