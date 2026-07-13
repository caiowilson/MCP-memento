<?php

declare(strict_types=1);

namespace App\Contracts;

interface ReportRepository
{
    public function recent(): array;
}
