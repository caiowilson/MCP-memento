<?php

declare(strict_types=1);

namespace App\Service;

use App\Repository\ReportRepository;

final class ReportService
{
    public function __construct(private ReportRepository $reports)
    {
    }

    public function recent(): array
    {
        return $this->reports->findRecent();
    }
}
