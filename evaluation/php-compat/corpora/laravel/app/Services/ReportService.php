<?php

declare(strict_types=1);

namespace App\Services;

use App\Contracts\ReportRepository;

final class ReportService
{
    public function __construct(private ReportRepository $reports)
    {
    }

    public function recent(): array
    {
        return $this->reports->recent();
    }
}
