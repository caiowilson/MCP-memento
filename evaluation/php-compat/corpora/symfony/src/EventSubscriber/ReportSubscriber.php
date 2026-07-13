<?php

declare(strict_types=1);

namespace App\EventSubscriber;

use App\Message\GenerateReport;
use Symfony\Component\EventDispatcher\EventSubscriberInterface;

final class ReportSubscriber implements EventSubscriberInterface
{
    public static function getSubscribedEvents(): array
    {
        return [GenerateReport::class => 'onReport'];
    }

    public function onReport(GenerateReport $event): void
    {
    }
}
