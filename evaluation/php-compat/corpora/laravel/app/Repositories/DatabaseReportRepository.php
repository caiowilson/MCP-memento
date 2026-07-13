<?php

declare(strict_types=1);

namespace App\Repositories;

use App\Contracts\ReportRepository;

final class DatabaseReportRepository implements ReportRepository
{
    public function recent(): array
    {
        return [];
    }
}
