<?php

declare(strict_types=1);

namespace App\Events;

use App\Models\Report;

final class ReportCreated
{
    public function __construct(public Report $report)
    {
    }
}
