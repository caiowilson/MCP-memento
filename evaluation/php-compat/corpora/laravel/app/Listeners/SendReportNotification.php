<?php

declare(strict_types=1);

namespace App\Listeners;

use App\Events\ReportCreated;

final class SendReportNotification
{
    public function handle(ReportCreated $event): void
    {
    }
}
