<?php

declare(strict_types=1);

namespace App\Message;

final readonly class GenerateReport
{
    public function __construct(public int $reportId)
    {
    }
}
