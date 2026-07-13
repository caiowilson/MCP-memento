<?php

declare(strict_types=1);

namespace App\MessageHandler;

use App\Message\GenerateReport;
use App\Service\ReportService;
use Symfony\Component\Messenger\Attribute\AsMessageHandler;

#[AsMessageHandler]
final class GenerateReportHandler
{
    public function __construct(private ReportService $reports)
    {
    }

    public function __invoke(GenerateReport $message): void
    {
    }
}
